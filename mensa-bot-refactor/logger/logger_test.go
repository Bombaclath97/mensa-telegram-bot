package logger

import (
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// captureEntry swaps the default logger for one writing to a temp file, runs fn, and returns
// the single log entry it produced.
//
// It sets defaultLogger directly rather than going through Init, which is guarded by a
// sync.Once and would therefore only ever work for the first test in the package.
func captureEntry(t *testing.T, fn func()) LogEntry {
	t.Helper()

	f, err := os.CreateTemp(t.TempDir(), "log-*.jsonl")
	if err != nil {
		t.Fatalf("creating temp log file: %v", err)
	}
	defer f.Close()

	prev := defaultLogger
	defaultLogger = &Logger{level: LevelDebug, output: f}
	t.Cleanup(func() { defaultLogger = prev })

	fn()

	raw, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("reading temp log file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 1 || lines[0] == "" {
		t.Fatalf("expected exactly one log line, got %d: %q", len(lines), raw)
	}

	var entry LogEntry
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("unmarshalling log line %q: %v", lines[0], err)
	}
	return entry
}

// TestCallerPointsAtTheCallSite is the regression test for the runtime.Caller(3) bug.
//
// With skip 3 the logger reported the caller OF the function containing the log statement,
// so every line in production named the wrong file — and where that frame left application
// code entirely it reported Go runtime internals (asm_amd64.s, proc.go, net/http/server.go).
// This asserts the reported site is the log statement itself.
func TestCallerPointsAtTheCallSite(t *testing.T) {
	var wantLine int

	entry := captureEntry(t, func() {
		// These two statements MUST stay adjacent: the assertion below relies on the Info
		// call sitting exactly one line after the runtime.Caller probe.
		_, _, line, _ := runtime.Caller(0)
		Info("hello")
		wantLine = line + 1
	})

	assertCaller(t, entry, wantLine)
}

// TestCallerIsCorrectForEveryEntryPoint locks in the invariant that callerSkip depends on:
// every exported entry point sits exactly ONE frame above (*Logger).log. The formatted
// variants used to delegate to Info/Warn/Error, putting them two frames up and silently
// mis-attributing their call sites.
func TestCallerIsCorrectForEveryEntryPoint(t *testing.T) {
	cases := []struct {
		name string
		emit func(string)
	}{
		{"Debug", func(m string) { Debug(m) }},
		{"Info", func(m string) { Info(m) }},
		{"Warn", func(m string) { Warn(m) }},
		{"Error", func(m string) { Error(m) }},
		{"Debugf", func(m string) { Debugf("%s", m) }},
		{"Infof", func(m string) { Infof("%s", m) }},
		{"Warnf", func(m string) { Warnf("%s", m) }},
		{"Errorf", func(m string) { Errorf("%s", m) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry := captureEntry(t, func() { tc.emit("hello") })

			// The reported site is the `func(m string) { Info(m) }` literal in the table
			// above — still this file. That is the property that matters: with the old skip
			// count these landed in a foreign frame (runtime or stdlib), never here.
			if !strings.HasPrefix(entry.Caller, "logger_test.go:") {
				t.Errorf("%s: caller = %q, want a logger_test.go site (got a foreign frame)",
					tc.name, entry.Caller)
			}
		})
	}
}

// TestCallerInsideGoroutine covers the case that produced "asm_amd64.s:1771" in production:
// a log statement that is the top-level call of a goroutine. The frame above it is
// runtime.goexit, so an off-by-one skip escapes into the runtime.
func TestCallerInsideGoroutine(t *testing.T) {
	var (
		wantLine int
		wg       sync.WaitGroup
	)

	entry := captureEntry(t, func() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Adjacent by contract, as above.
			_, _, line, _ := runtime.Caller(0)
			Info("from a goroutine")
			wantLine = line + 1
		}()
		wg.Wait()
	})

	assertCaller(t, entry, wantLine)
}

// TestFormattedMessageIsRendered checks that flattening the *f variants onto log did not
// break the formatting itself.
//
// Fatalf is deliberately untested: it calls os.Exit and would terminate the test binary. It
// was flattened the same way and for the same reason as the variants covered above.
func TestFormattedMessageIsRendered(t *testing.T) {
	entry := captureEntry(t, func() {
		Infof("member %d joined %s", 42, "group")
	})

	if entry.Message != "member 42 joined group" {
		t.Errorf("message = %q, want %q", entry.Message, "member 42 joined group")
	}
}

// TestLevelFilteringStillApplies makes sure moving the caller lookup did not disturb the
// level gate.
func TestLevelFilteringStillApplies(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "log-*.jsonl")
	if err != nil {
		t.Fatalf("creating temp log file: %v", err)
	}
	defer f.Close()

	prev := defaultLogger
	defaultLogger = &Logger{level: LevelWarn, output: f}
	t.Cleanup(func() { defaultLogger = prev })

	Info("should be dropped")
	Debug("should be dropped")

	raw, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("reading temp log file: %v", err)
	}
	if len(strings.TrimSpace(string(raw))) != 0 {
		t.Errorf("expected no output below the level threshold, got %q", raw)
	}
}

func assertCaller(t *testing.T, entry LogEntry, wantLine int) {
	t.Helper()

	if entry.Caller == "" {
		t.Fatal("caller is empty")
	}

	file, _, found := strings.Cut(entry.Caller, ":")
	if !found {
		t.Fatalf("caller %q is not in file:line form", entry.Caller)
	}
	if file != "logger_test.go" {
		t.Errorf("caller file = %q, want logger_test.go — the skip count escaped this file", file)
	}

	want := "logger_test.go:" + itoa(wantLine)
	if entry.Caller != want {
		t.Errorf("caller = %q, want %q", entry.Caller, want)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
