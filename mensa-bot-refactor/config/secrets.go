// Package config resolves configuration that arrives indirectly, before anything reads it.
package config

import (
	"fmt"
	"os"
	"strings"
)

// fileSuffix is the conventional Docker marker: BOT_TOKEN_FILE points at a file whose
// contents are the real BOT_TOKEN. The postgres, mysql and redis official images all use it,
// so Swarm and Portainer users expect it to work.
const fileSuffix = "_FILE"

// ExpandFileSecrets turns every "X_FILE=/path" environment variable into "X=<file contents>".
//
// # Why this shape
//
// Docker Swarm delivers secrets as FILES under /run/secrets/, never as environment variables
// — that is the entire point, since env vars leak through `docker service inspect`, the
// Portainer UI, and any child process. But this codebase reads all seventeen of its settings
// with os.Getenv, scattered across main, database, api, server, cleanup, tolgee and model.
//
// Rather than thread a file-aware config layer through every one of those call sites, this
// resolves the indirection ONCE, at startup, before any of them run. BOT_TOKEN_FILE becomes
// BOT_TOKEN and every existing os.Getenv keeps working untouched. The alternative — a
// config.Get() wrapper — would be a large diff across the whole tree for identical behaviour.
//
// Call it FIRST in main, before logger.Init and before anything reads its configuration.
//
// # Failure policy
//
// Every error here is fatal to the caller, deliberately. A secret that was configured but
// cannot be read is not a degraded state to limp along in: proceeding would fall back to an
// empty BOT_TOKEN or API_BEARER_TOKEN and fail later, further away, with a worse message.
func ExpandFileSecrets() error {
	// os.Environ returns a snapshot, so mutating the environment while ranging is safe.
	for _, entry := range os.Environ() {
		key, path, found := strings.Cut(entry, "=")
		if !found {
			continue
		}

		target, isFileVar := strings.CutSuffix(key, fileSuffix)
		if !isFileVar || target == "" {
			continue
		}

		// An empty X_FILE is almost always a compose file interpolating an unset variable.
		// Treat it as "not configured" rather than reading "".
		if strings.TrimSpace(path) == "" {
			continue
		}

		// Both X and X_FILE set is ambiguous, and guessing which one wins is how a stale
		// token silently overrides a rotated one. Refuse.
		if existing, ok := os.LookupEnv(target); ok && existing != "" {
			return fmt.Errorf("both %s and %s are set: remove one, they cannot both define %s",
				target, key, target)
		}

		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s from %q: %w", target, path, err)
		}

		// Trim surrounding whitespace. This is not cosmetic: `echo "token" | docker secret
		// create` leaves a trailing newline, and a BOT_TOKEN with "\n" on the end produces a
		// 404 from Telegram that looks exactly like a wrong token. Secrets are single-line
		// values, so there is nothing legitimate to preserve here.
		value := strings.TrimSpace(string(raw))
		if value == "" {
			return fmt.Errorf("%s is empty (from %q)", target, path)
		}

		if err := os.Setenv(target, value); err != nil {
			return fmt.Errorf("setting %s: %w", target, err)
		}
	}

	return nil
}
