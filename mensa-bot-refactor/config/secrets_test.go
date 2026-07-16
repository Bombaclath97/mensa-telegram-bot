package config

import (
	"os"
	"path/filepath"
	"testing"
)

// writeSecret creates a secret file the way Docker would mount one.
func writeSecret(t *testing.T, name, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing secret fixture: %v", err)
	}
	return path
}

// clearEnv unsets a variable for the duration of one test and restores it afterwards.
//
// t.Setenv is not enough here: it only restores what the TEST set, and the whole job of
// ExpandFileSecrets is to os.Setenv the TARGET variable — which nothing then cleans up. That
// leaks into the next test, where a BOT_TOKEN left behind by an earlier case trips the
// "both set" guard and fails an unrelated assertion.
func clearEnv(t *testing.T, key string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	os.Unsetenv(key)
	t.Cleanup(func() {
		if had {
			os.Setenv(key, old)
			return
		}
		os.Unsetenv(key)
	})
}

func TestExpandFileSecretsResolvesIntoPlainEnv(t *testing.T) {
	clearEnv(t, "BOT_TOKEN")
	path := writeSecret(t, "bot_token", "123:ABC")
	t.Setenv("BOT_TOKEN_FILE", path)

	if err := ExpandFileSecrets(); err != nil {
		t.Fatalf("ExpandFileSecrets: %v", err)
	}
	if got := os.Getenv("BOT_TOKEN"); got != "123:ABC" {
		t.Fatalf("BOT_TOKEN = %q, want %q", got, "123:ABC")
	}
}

// TestExpandFileSecretsTrimsTrailingNewline pins the classic Docker-secrets footgun.
//
// `echo "token" | docker secret create bot_token -` leaves a trailing newline in the file. A
// BOT_TOKEN carrying "\n" produces a 404 from Telegram that looks exactly like a wrong token,
// and people burn hours on it. Secrets are single-line values; there is nothing legitimate to
// preserve.
func TestExpandFileSecretsTrimsTrailingNewline(t *testing.T) {
	clearEnv(t, "BOT_TOKEN")
	path := writeSecret(t, "bot_token", "123:ABC\n")
	t.Setenv("BOT_TOKEN_FILE", path)

	if err := ExpandFileSecrets(); err != nil {
		t.Fatalf("ExpandFileSecrets: %v", err)
	}
	if got := os.Getenv("BOT_TOKEN"); got != "123:ABC" {
		t.Fatalf("BOT_TOKEN = %q, want the newline trimmed", got)
	}
}

// TestExpandFileSecretsRejectsBothSet: guessing a winner is how a stale token silently
// overrides a rotated one.
func TestExpandFileSecretsRejectsBothSet(t *testing.T) {
	clearEnv(t, "BOT_TOKEN")
	path := writeSecret(t, "bot_token", "from-file")
	t.Setenv("BOT_TOKEN_FILE", path)
	t.Setenv("BOT_TOKEN", "from-env")

	err := ExpandFileSecrets()
	if err == nil {
		t.Fatal("setting both BOT_TOKEN and BOT_TOKEN_FILE must be an error, not a silent pick")
	}
	if got := os.Getenv("BOT_TOKEN"); got != "from-env" {
		t.Fatalf("a rejected expansion must not mutate the environment, got %q", got)
	}
}

func TestExpandFileSecretsErrorsOnMissingFile(t *testing.T) {
	clearEnv(t, "API_BEARER_TOKEN")
	t.Setenv("API_BEARER_TOKEN_FILE", "/definitely/not/here")

	if err := ExpandFileSecrets(); err == nil {
		t.Fatal("an unreadable secret must be an error: continuing would fall back to an " +
			"empty token and fail later with a worse message")
	}
}

func TestExpandFileSecretsErrorsOnEmptySecret(t *testing.T) {
	clearEnv(t, "BOT_TOKEN")
	path := writeSecret(t, "bot_token", "   \n")
	t.Setenv("BOT_TOKEN_FILE", path)

	if err := ExpandFileSecrets(); err == nil {
		t.Fatal("an empty secret file must be an error")
	}
}

// TestExpandFileSecretsIgnoresBlankFileVar: compose interpolating an unset variable yields
// X_FILE="", which means "not configured", not "read the file at ”".
func TestExpandFileSecretsIgnoresBlankFileVar(t *testing.T) {
	clearEnv(t, "BOT_TOKEN")
	t.Setenv("BOT_TOKEN_FILE", "")

	if err := ExpandFileSecrets(); err != nil {
		t.Fatalf("a blank X_FILE must be ignored, got: %v", err)
	}
	if _, set := os.LookupEnv("BOT_TOKEN"); set {
		t.Fatal("a blank X_FILE must not define BOT_TOKEN")
	}
}

// TestExpandFileSecretsLeavesOtherVarsAlone guards the suffix matching: a variable that merely
// CONTAINS _FILE, or is exactly "_FILE", must not be treated as a secret pointer.
func TestExpandFileSecretsLeavesOtherVarsAlone(t *testing.T) {
	t.Setenv("TOLGEE_CACHE_PATH", "/data/tolgee-cache.json")
	t.Setenv("SOME_FILE_PATH", "/etc/thing")

	if err := ExpandFileSecrets(); err != nil {
		t.Fatalf("ExpandFileSecrets: %v", err)
	}
	if got := os.Getenv("TOLGEE_CACHE_PATH"); got != "/data/tolgee-cache.json" {
		t.Fatalf("TOLGEE_CACHE_PATH was mangled: %q", got)
	}
	if _, set := os.LookupEnv("SOME_FILE_PATH_"); set {
		t.Fatal("SOME_FILE_PATH is not a secret pointer: only the _FILE SUFFIX counts")
	}
}
