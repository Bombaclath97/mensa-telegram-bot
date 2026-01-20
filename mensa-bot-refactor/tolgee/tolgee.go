package tolgee

import (
	"encoding/json"
	"strings"

	"github.com/go-resty/resty/v2"
	"github.com/tidwall/gjson"
)

// Language represents a language configuration from Tolgee
type Language struct {
	Id           int               `json:"id"`
	Name         string            `json:"name"`
	Tag          string            `json:"tag"`
	OriginalName string            `json:"originalName"`
	FlagEmoji    string            `json:"flagEmoji"`
	Base         bool              `json:"base"`
	Translations map[string]string `json:"translations"`
}

var (
	apiKey       string
	translations map[string]Language
	baseLanguage = "en"
)

// Load initializes the Tolgee client and fetches all translations
func Load(ak string) error {
	apiKey = ak
	return fetchLanguages()
}

// fetchLanguages fetches all languages and their translations from Tolgee
func fetchLanguages() error {
	languagesData, err := resty.New().R().SetQueryParams(
		map[string]string{
			"ak":   apiKey,
			"size": "1000",
		},
	).Get("https://app.tolgee.io/v2/projects/languages")
	if err != nil {
		return err
	}

	arrayOfLanguages := gjson.ParseBytes(languagesData.Body()).Get("_embedded.languages").Array()
	languageList := make([]Language, 0, len(arrayOfLanguages))

	for _, language := range arrayOfLanguages {
		buildLang := Language{
			Id:           int(language.Get("id").Int()),
			Name:         language.Get("name").String(),
			Tag:          language.Get("tag").String(),
			OriginalName: language.Get("originalName").String(),
			FlagEmoji:    language.Get("flagEmoji").String(),
			Base:         language.Get("base").Bool(),
		}

		translationData, err := resty.New().R().SetQueryParams(
			map[string]string{
				"ak":                 apiKey,
				"size":               "1000",
				"languages":          buildLang.Tag,
				"format":             "JSON",
				"zip":                "false",
				"structureDelimiter": "",
			},
		).Get("https://app.tolgee.io/v2/projects/export")
		if err == nil {
			_ = json.Unmarshal(translationData.Body(), &buildLang.Translations)
		}

		if buildLang.Base {
			baseLanguage = buildLang.Tag
		}
		languageList = append(languageList, buildLang)
	}

	translations = make(map[string]Language)
	for _, lang := range languageList {
		translations[lang.Tag] = lang
	}

	return nil
}

// T is a shorthand for GetTranslation
func T(key string, lang string, namedArgs ...map[string]string) string {
	return GetTranslation(key, lang, namedArgs...)
}

// GetTranslation retrieves a translation for the given key and language
// Falls back to base language if the requested language is not found
// Supports named parameter substitution via {paramName} placeholders
func GetTranslation(key string, lang string, namedArgs ...map[string]string) string {
	existingLanguage := baseLanguage

	// Check if the language exists
	if len(strings.TrimSpace(lang)) > 1 {
		if _, ok := translations[lang]; ok {
			existingLanguage = lang
		} else {
			// Try without region code (e.g., "it_IT" -> "it")
			if parts := strings.Split(lang, "_"); len(parts) > 1 {
				if _, ok := translations[parts[0]]; ok {
					existingLanguage = parts[0]
				}
			}
		}
	}

	// Get translation
	if translation, ok := translations[existingLanguage].Translations[key]; ok {
		result := translation
		// Replace named parameters
		if len(namedArgs) > 0 {
			for k, v := range namedArgs[0] {
				result = strings.ReplaceAll(result, "{"+k+"}", v)
			}
		}
		return result
	}

	// Return key if translation not found
	return key
}
