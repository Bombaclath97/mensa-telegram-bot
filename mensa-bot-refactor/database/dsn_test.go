package database

import (
	"net/url"
	"strings"
	"testing"
)

// clearDSNEnv wipes every variable resolveDSN reads, so a test starts from nothing regardless
// of what TestMain or the developer's shell set.
func clearDSNEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"DATABASE_URL", "DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME", "DB_SSLMODE",
	} {
		t.Setenv(k, "")
	}
}

func TestResolveDSNPrefersDatabaseURL(t *testing.T) {
	clearDSNEnv(t)
	t.Setenv("DATABASE_URL", "postgres://u:p@example:5432/db?sslmode=require")
	t.Setenv("DB_HOST", "ignored")

	got, err := resolveDSN()
	if err != nil {
		t.Fatalf("resolveDSN: %v", err)
	}
	if got != "postgres://u:p@example:5432/db?sslmode=require" {
		t.Fatalf("DATABASE_URL must win verbatim, got %q", got)
	}
}

func TestResolveDSNAssemblesFromParts(t *testing.T) {
	clearDSNEnv(t)
	t.Setenv("DB_HOST", "postgres")
	t.Setenv("DB_USER", "mensa")
	t.Setenv("DB_PASSWORD", "hunter2")
	t.Setenv("DB_NAME", "mensa")

	got, err := resolveDSN()
	if err != nil {
		t.Fatalf("resolveDSN: %v", err)
	}

	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("assembled DSN does not parse: %v", err)
	}
	if u.Host != "postgres:5432" {
		t.Errorf("host = %q, want postgres:5432 (DB_PORT should default)", u.Host)
	}
	if u.Path != "/mensa" {
		t.Errorf("path = %q, want /mensa", u.Path)
	}
	if pw, _ := u.User.Password(); pw != "hunter2" {
		t.Errorf("password = %q, want hunter2", pw)
	}
	if u.Query().Get("sslmode") != "disable" {
		t.Errorf("sslmode = %q, want disable by default", u.Query().Get("sslmode"))
	}
}

// TestResolveDSNEscapesNastyPassword is the reason this is built with net/url rather than
// fmt.Sprintf.
//
// Docker secrets are frequently generated with `openssl rand -base64`, which emits '+' and
// '/'. A password containing '@' or '/' pasted straight into a DSN silently re-parses: the
// '@' becomes the host separator and the connection goes somewhere else entirely, or fails
// with an error that points nowhere near the real cause.
func TestResolveDSNEscapesNastyPassword(t *testing.T) {
	clearDSNEnv(t)
	const nasty = "p@ss/w:rd?#&=+ x"
	t.Setenv("DB_HOST", "postgres")
	t.Setenv("DB_USER", "mensa")
	t.Setenv("DB_PASSWORD", nasty)
	t.Setenv("DB_NAME", "mensa")

	got, err := resolveDSN()
	if err != nil {
		t.Fatalf("resolveDSN: %v", err)
	}

	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("a password with URL metacharacters broke the DSN: %v (dsn=%q)", err, got)
	}
	if u.Host != "postgres:5432" {
		t.Fatalf("host = %q, want postgres:5432 — the password's '@' leaked into the host", u.Host)
	}
	pw, _ := u.User.Password()
	if pw != nasty {
		t.Fatalf("password round-trip = %q, want %q", pw, nasty)
	}
}

func TestResolveDSNErrorsWhenUnconfigured(t *testing.T) {
	clearDSNEnv(t)

	if _, err := resolveDSN(); err == nil {
		t.Fatal("no DATABASE_URL and no DB_HOST must be an error")
	}
}

func TestResolveDSNErrorsOnPartialConfig(t *testing.T) {
	clearDSNEnv(t)
	t.Setenv("DB_HOST", "postgres") // but no user/name

	if _, err := resolveDSN(); err == nil {
		t.Fatal("DB_HOST without DB_USER/DB_NAME must be an error, not a half-built DSN")
	}
}

// TestRedactDSNHidesPassword: the DSN is logged on every boot, and it carries the Postgres
// password. Whatever ships the logs must never see it.
func TestRedactDSNHidesPassword(t *testing.T) {
	got := redactDSN("postgres://mensa:sup3rs3cret@postgres:5432/mensa?sslmode=disable")

	if strings.Contains(got, "sup3rs3cret") {
		t.Fatalf("redactDSN leaked the password: %q", got)
	}
	if !strings.Contains(got, "postgres:5432") {
		t.Fatalf("redactDSN should keep the host for diagnostics, got %q", got)
	}
	if !strings.Contains(got, "mensa") {
		t.Fatalf("redactDSN should keep the user/db for diagnostics, got %q", got)
	}
}

func TestRedactDSNSurvivesGarbage(t *testing.T) {
	// Must never panic on a malformed DSN: this runs on the error path at boot.
	if got := redactDSN("://not a url at all"); got == "" {
		t.Fatal("redactDSN must always return something printable")
	}
}
