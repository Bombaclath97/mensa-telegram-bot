// Package tolgee loads the bot's translations and resolves them at runtime.
//
// Translations are loaded from the public Tolgee CDN and cached on disk so that
// a Tolgee outage can never degrade the bot to raw dotted keys:
//
//  1. Tolgee CDN     (https://cdn.tolg.ee/<project>/<lang>.json, no auth)
//  2. On-disk cache  (TOLGEE_CACHE_PATH — the last successful load)
//
// The authenticated export API is intentionally NOT used: it returns whatever
// project the API key belongs to, and a key pointing at a different project
// silently serves keys that don't match the bot's, rendering every string raw.
// Whatever is loaded from the CDN is persisted back to the disk cache, so a cold
// start with the CDN unreachable degrades to "slightly stale strings".
package tolgee

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/Mensa-Italia/mensa-telegram-bot/logger"
)

// Language represents a language configuration from Tolgee.
type Language struct {
	Id           int               `json:"id"`
	Name         string            `json:"name"`
	Tag          string            `json:"tag"`
	OriginalName string            `json:"originalName"`
	FlagEmoji    string            `json:"flagEmoji"`
	Base         bool              `json:"base"`
	Translations map[string]string `json:"translations"`
}

const (
	// DefaultBaseLanguage must stay in sync with database.DefaultLanguage ("it").
	// It is duplicated rather than imported to keep this package free of any
	// dependency on the database layer.
	DefaultBaseLanguage = "it"

	// DefaultCDNProjectID is the public Tolgee CDN project of this bot (see the
	// legacy mensa-bot/tolgee/init.go, commit 5f0d3ad).
	DefaultCDNProjectID = "6b648d1efa05ed4f9ee10ba36ac4e6d6"

	defaultCacheFileName = "tolgee-cache.json"
	httpTimeout          = 15 * time.Second
)

// defaultLanguageTags is the language list asked of the CDN. The CDN has no
// index endpoint, so the tags are built in; keep this in sync with the Tolgee
// project (see TOLGEE_KEYS.md). The disk cache widens it on a warm start if the
// project gains a language before this list is updated.
var defaultLanguageTags = []string{"it", "en", "es", "de", "fr", "pt", "ja", "zh", "hi", "bn"}

var (
	mu           sync.RWMutex
	translations = map[string]Language{}
	baseLanguage = DefaultBaseLanguage
)

// Load initializes the Tolgee client and fetches all translations.
//
// The API key may be empty: in that case the export tier is skipped and
// translations are loaded from the CDN, then from the disk cache. Load only
// returns an error when NO translation whatsoever could be obtained from any
// tier — i.e. when the bot would actually render raw keys.
func Load() error {
	return Refresh()
}

// Refresh re-runs the whole load chain. Safe to call concurrently with T.
func Refresh() error {
	cached, cachedBase := loadDiskCache()

	// The CDN has no language index, so the tags come from the disk cache (warm
	// start) or the built-in default list (cold start).
	langs := fallbackLanguageList(cached, cachedBase)

	loaded := make(map[string]Language, len(langs))
	sources := make(map[string]string, len(langs))
	base := DefaultBaseLanguage
	if cachedBase != "" {
		base = cachedBase
	}
	fromNetwork := 0

	for _, lang := range langs {
		if lang.Tag == "" {
			continue
		}
		if lang.Base {
			base = lang.Tag
		}

		tr, source := fetchTranslationsFor(lang.Tag)
		if len(tr) == 0 {
			// Tier 3: the last successful load, persisted on disk.
			if c, ok := cached[lang.Tag]; ok && len(c.Translations) > 0 {
				tr = c.Translations
				source = "disk-cache"
				logger.Warn("Tolgee: serving stale translations from the disk cache", map[string]interface{}{
					"language": lang.Tag,
					"keys":     len(tr),
				})
			}
		} else {
			fromNetwork++
		}

		if len(tr) == 0 {
			logger.Error("Tolgee: no translations available for language from any source (Tolgee, CDN, disk cache)", map[string]interface{}{
				"language": lang.Tag,
			})
			continue
		}

		lang.Translations = tr
		loaded[lang.Tag] = lang
		sources[lang.Tag] = source
	}

	if len(loaded) == 0 {
		logger.Error("Tolgee: translation load FAILED on every tier, the bot will render raw keys", nil)
		return fmt.Errorf("tolgee: no translations could be loaded from Tolgee, the CDN or the disk cache")
	}

	// Never point the base language at a language we failed to load.
	if _, ok := loaded[base]; !ok {
		if _, ok := loaded[DefaultBaseLanguage]; ok {
			base = DefaultBaseLanguage
		} else {
			base = anyTag(loaded)
		}
		logger.Warn("Tolgee: base language unavailable, using a fallback base language", map[string]interface{}{
			"base_language": base,
		})
	}

	mu.Lock()
	translations = loaded
	baseLanguage = base
	mu.Unlock()

	logger.Info("Tolgee: translations loaded", map[string]interface{}{
		"base_language": base,
		"languages":     len(loaded),
		"sources":       sources,
	})

	// Only rewrite the cache when at least one language came off the network,
	// otherwise we would just be rewriting the cache with itself.
	if fromNetwork > 0 {
		if err := saveDiskCache(loaded, base); err != nil {
			logger.Warn("Tolgee: could not persist the translation cache", map[string]interface{}{
				"path":  cachePath(),
				"error": err.Error(),
			})
		}
	}

	return nil
}

// T is a shorthand for GetTranslation.
func T(key string, lang string, namedArgs ...map[string]string) string {
	return GetTranslation(key, lang, namedArgs...)
}

// GetTranslation retrieves a translation for the given key and language.
//
// Resolution order: exact language -> language without its region suffix
// ("it_IT" -> "it") -> base language -> the key itself. Named parameters are
// substituted for their {placeholder} occurrences.
func GetTranslation(key string, lang string, namedArgs ...map[string]string) string {
	mu.RLock()
	defer mu.RUnlock()

	for _, tag := range candidateTags(lang) {
		l, ok := translations[tag]
		if !ok {
			continue
		}
		if translation, ok := l.Translations[key]; ok && translation != "" {
			return applyNamedArgs(translation, namedArgs...)
		}
	}

	logger.Debug("Tolgee: missing translation key, returning the key itself", map[string]interface{}{
		"key":      key,
		"language": lang,
	})
	// Even the fallback renders its placeholders, so a missing key never leaks a
	// raw "{name}" to the user on top of the raw key.
	return applyNamedArgs(key, namedArgs...)
}

// BaseLanguage returns the language every lookup falls back to.
func BaseLanguage() string {
	mu.RLock()
	defer mu.RUnlock()
	return baseLanguage
}

// Languages returns the tags of every loaded language, sorted.
func Languages() []string {
	mu.RLock()
	defer mu.RUnlock()
	tags := make([]string, 0, len(translations))
	for tag := range translations {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags
}

// ---------------------------------------------------------------------------
// resolution helpers
// ---------------------------------------------------------------------------

// candidateTags builds the ordered lookup chain for a requested language.
// Caller must hold at least the read lock (it reads baseLanguage).
func candidateTags(lang string) []string {
	tags := make([]string, 0, 3)
	add := func(t string) {
		if t == "" {
			return
		}
		for _, existing := range tags {
			if existing == t {
				return
			}
		}
		tags = append(tags, t)
	}

	lang = strings.TrimSpace(lang)
	if lang != "" {
		add(lang)
		// "it_IT" / "it-IT" -> "it"
		if i := strings.IndexAny(lang, "_-"); i > 0 {
			add(lang[:i])
		}
	}
	add(baseLanguage)
	add(DefaultBaseLanguage)
	return tags
}

// applyNamedArgs substitutes {placeholder} occurrences.
//
// Values are HTML-ESCAPED, because the bot sends messages with ParseMode=HTML and every
// value substituted here is untrusted: Telegram display names, group titles, and text the
// user typed. Without escaping, a member named "Anna <b>" or a group called "A & B" would
// make Telegram reject the entire message ("can't parse entities") and the user would
// silently receive nothing — and a hostile display name could inject markup into a message
// the bot puts in a group.
//
// This is safe to do unconditionally: the only keys that take parameters are message
// bodies. Button labels and command-menu descriptions take none, so they are never escaped
// (Telegram renders those as plain text, where "&amp;" would show literally).
func applyNamedArgs(s string, namedArgs ...map[string]string) string {
	if len(namedArgs) == 0 {
		return s
	}
	for _, args := range namedArgs {
		for k, v := range args {
			s = strings.ReplaceAll(s, "{"+k+"}", EscapeHTML(v))
		}
	}
	return s
}

// EscapeHTML makes an untrusted value safe to interpolate into a ParseMode=HTML message.
// Telegram's HTML mode only requires these three to be escaped.
func EscapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func anyTag(m map[string]Language) string {
	tags := make([]string, 0, len(m))
	for tag := range m {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	if len(tags) == 0 {
		return DefaultBaseLanguage
	}
	return tags[0]
}

func httpClient() *resty.Client {
	return resty.New().SetTimeout(httpTimeout)
}

// fallbackLanguageList decides WHICH languages to ask the CDN for: the disk
// cache on a warm start, the built-in default list on a cold start.
func fallbackLanguageList(cached map[string]Language, cachedBase string) []Language {
	if len(cached) > 0 {
		langs := make([]Language, 0, len(cached))
		for _, tag := range sortedKeys(cached) {
			l := cached[tag]
			l.Translations = nil // refetched below; cache is consulted per-language
			l.Base = tag == cachedBase
			langs = append(langs, l)
		}
		return langs
	}

	langs := make([]Language, 0, len(defaultLanguageTags))
	for _, tag := range defaultLanguageTags {
		langs = append(langs, Language{Tag: tag, Base: tag == DefaultBaseLanguage})
	}
	return langs
}

// fetchTranslationsFor loads one language from the CDN. It returns an empty map
// on failure, logged with the language and the cause; the caller then falls
// back to the disk cache.
func fetchTranslationsFor(tag string) (map[string]string, string) {
	tr, err := fetchFromCDN(tag)
	switch {
	case err != nil:
		logger.Warn("Tolgee: CDN fetch failed", map[string]interface{}{
			"language": tag,
			"url":      cdnURL(tag),
			"error":    err.Error(),
		})
	case len(tr) == 0:
		logger.Warn("Tolgee: CDN returned no translations", map[string]interface{}{
			"language": tag,
			"url":      cdnURL(tag),
		})
	default:
		return tr, "tolgee-cdn"
	}

	return nil, ""
}

// ---------------------------------------------------------------------------
// tier 1: the Tolgee CDN
// ---------------------------------------------------------------------------

// cdnURL builds https://cdn.tolg.ee/<project>/<lang>.json.
// TOLGEE_CDN_URL overrides the whole base, TOLGEE_CDN_PROJECT_ID just the project.
func cdnURL(tag string) string {
	base := strings.TrimSpace(os.Getenv("TOLGEE_CDN_URL"))
	if base == "" {
		project := strings.TrimSpace(os.Getenv("TOLGEE_CDN_PROJECT_ID"))
		if project == "" {
			project = DefaultCDNProjectID
		}
		base = "https://cdn.tolg.ee/" + project
	}
	return strings.TrimRight(base, "/") + "/" + tag + ".json"
}

func fetchFromCDN(tag string) (map[string]string, error) {
	resp, err := httpClient().R().Get(cdnURL(tag))
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, fmt.Errorf("CDN returned HTTP %d", resp.StatusCode())
	}
	return decodeTranslations(resp.Body())
}

// decodeTranslations accepts both the flat map the export produces and a nested
// object (should the project ever be exported with a structure delimiter), which
// it flattens back to dotted keys.
func decodeTranslations(body []byte) (map[string]string, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("invalid translation JSON: %w", err)
	}

	out := make(map[string]string, len(raw))
	flatten("", raw, out)
	return out, nil
}

func flatten(prefix string, in map[string]interface{}, out map[string]string) {
	for k, v := range in {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch val := v.(type) {
		case string:
			out[key] = val
		case map[string]interface{}:
			flatten(key, val, out)
		case nil:
			// skip empty translations
		default:
			out[key] = fmt.Sprint(val)
		}
	}
}

// ---------------------------------------------------------------------------
// tier 3: the on-disk cache
// ---------------------------------------------------------------------------

type diskCache struct {
	BaseLanguage string              `json:"base_language"`
	FetchedAt    time.Time           `json:"fetched_at"`
	Languages    map[string]Language `json:"languages"`
}

// cachePath resolves TOLGEE_CACHE_PATH, defaulting to a file in the working directory.
//
// It used to default to DB_PATH's directory, so the cache would land on the same volume as
// the SQLite file. Both the variable and the volume are gone: the database is Postgres now
// and the bot keeps no state on disk, so there is no "persistent volume" to sit next to. The
// Docker image points this at /tmp, which is writable and expendable — losing the cache only
// costs a re-fetch from the CDN.
func cachePath() string {
	if p := strings.TrimSpace(os.Getenv("TOLGEE_CACHE_PATH")); p != "" {
		return p
	}
	return defaultCacheFileName
}

func loadDiskCache() (map[string]Language, string) {
	path := cachePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Warn("Tolgee: could not read the translation cache", map[string]interface{}{
				"path":  path,
				"error": err.Error(),
			})
		}
		return map[string]Language{}, ""
	}

	var c diskCache
	if err := json.Unmarshal(data, &c); err != nil {
		logger.Warn("Tolgee: the translation cache is corrupt and will be ignored", map[string]interface{}{
			"path":  path,
			"error": err.Error(),
		})
		return map[string]Language{}, ""
	}

	if c.Languages == nil {
		c.Languages = map[string]Language{}
	}
	logger.Debug("Tolgee: translation cache loaded", map[string]interface{}{
		"path":       path,
		"languages":  len(c.Languages),
		"fetched_at": c.FetchedAt,
	})
	return c.Languages, c.BaseLanguage
}

func saveDiskCache(langs map[string]Language, base string) error {
	path := cachePath()
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	data, err := json.Marshal(diskCache{
		BaseLanguage: base,
		FetchedAt:    time.Now().UTC(),
		Languages:    langs,
	})
	if err != nil {
		return err
	}

	// Write atomically so a crash mid-write cannot leave a truncated cache.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tolgee-cache-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}

	logger.Debug("Tolgee: translation cache persisted", map[string]interface{}{
		"path":      path,
		"languages": len(langs),
	})
	return nil
}

func sortedKeys(m map[string]Language) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
