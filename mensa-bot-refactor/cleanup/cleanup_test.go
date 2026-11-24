package cleanup

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Mensa-Italia/mensa-telegram-bot/database"
	"github.com/Mensa-Italia/mensa-telegram-bot/internal/testdb"
	"github.com/Mensa-Italia/mensa-telegram-bot/internal/testtg"
	"github.com/Mensa-Italia/mensa-telegram-bot/membership"
)

// fakeTG is the fake Telegram Bot API every test in this package talks to. Tests that touch
// group associations assert against it; the rest never reach it.
var fakeTG *testtg.Server

// TestMain initialises a real, throwaway PostgreSQL container (the sweep loads `users` and
// deletes rows, and relies on the association cascade). Skips when Docker is unavailable.
func TestMain(m *testing.M) {
	ctx := context.Background()

	dsn, terminate, err := testdb.Start(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: no postgres container (is Docker running?): %v\n", err)
		os.Exit(0)
	}
	os.Setenv("DATABASE_URL", dsn)

	if err := database.Init(); err != nil {
		terminate()
		panic("database.Init failed: " + err.Error())
	}

	fakeTG, err = testtg.Install()
	if err != nil {
		terminate()
		panic("testtg.Install failed: " + err.Error())
	}

	code := m.Run()

	fakeTG.Close()
	_ = database.Close()
	terminate()
	os.Exit(code)
}

// ---------------------------------------------------------------------------
// Test doubles & helpers
// ---------------------------------------------------------------------------

// mockAPI is a stand-in for the Mensa /valid endpoint. It records every member_id it was
// asked about (so a test can prove the MEMBER NUMBER, not the Telegram ID, reached the API)
// and answers per-member from `active`.
type mockAPI struct {
	mu      sync.Mutex
	seen    []string
	active  map[string]bool // member_id -> is_membership_active
	status  int             // response status (0 => 200)
	rawBody string          // if set, returned verbatim regardless of member_id
	server  *httptest.Server
}

func newMockAPI(t *testing.T, active map[string]bool) *mockAPI {
	t.Helper()
	m := &mockAPI{active: active, status: http.StatusOK}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		memberID := r.PostFormValue("member_id")

		m.mu.Lock()
		m.seen = append(m.seen, memberID)
		status := m.status
		raw := m.rawBody
		isActive := m.active[memberID]
		m.mu.Unlock()

		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		if raw != "" {
			_, _ = w.Write([]byte(raw))
			return
		}
		// A well-formed payload with a non-empty name so the sweep's "suspicious empty
		// payload" guard does not fire — this is a CONCLUSIVE answer.
		fmt.Fprintf(w, `{"username":"member%s","name":"Member %s","is_membership_active":%t}`,
			memberID, memberID, isActive)
	}))
	t.Cleanup(m.server.Close)
	return m
}

func (m *mockAPI) sawMember(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.seen {
		if s == id {
			return true
		}
	}
	return false
}

// arm points the cleanup package's api client at the mock and disables Telegram throttling.
func (m *mockAPI) arm(t *testing.T) {
	t.Helper()
	t.Setenv("API_ENDPOINT", m.server.URL)
	t.Setenv("API_BEARER_TOKEN", "test-token")
	t.Setenv("CLEANUP_TELEGRAM_DELAY_MS", "0")
}

func clearDB(t *testing.T) {
	t.Helper()
	for _, table := range []string{
		"groups_associations", "pending_join_requests", "user_preferences", "users", "groups",
	} {
		if err := database.Get().Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatalf("clear %s: %v", table, err)
		}
	}
	database.SeedBotAdmins(nil)
}

type userOpt func(*database.User)

func international() userOpt { return func(u *database.User) { u.IsInternational = true } }
func botAdmin() userOpt      { return func(u *database.User) { u.IsBotAdmin = true } }

// neverGroupSynced leaves groups_synced_at NULL, i.e. the bot has NEVER probed which groups
// this member is in. Combined with an empty association list this means "we do not know where
// they are", NOT "they are in no groups" — and the sweep must refuse to delete them.
func neverGroupSynced() userOpt { return func(u *database.User) { u.GroupsSyncedAt = nil } }

// insertUser creates a member row directly (bypassing repo side effects).
//
// groups_synced_at defaults to SET, because that is the steady state of a real registered
// member: registration probes every known group and stamps them. A test that passes no
// associations is therefore asserting "probed, and genuinely in no groups" — which is a
// deletable member, and keeps these tests free of Telegram calls. Use neverGroupSynced() for
// the other case.
func insertUser(t *testing.T, telegramID, memberNumber int64, opts ...userOpt) {
	t.Helper()
	syncedAt := time.Now()
	u := &database.User{
		TelegramID:     telegramID,
		MensaEmail:     fmt.Sprintf("user%d@mensa.it", telegramID),
		FirstName:      "F",
		LastName:       "L",
		MemberNumber:   memberNumber,
		Language:       "it",
		GroupsSyncedAt: &syncedAt,
	}
	for _, o := range opts {
		o(u)
	}
	if err := database.Get().Create(u).Error; err != nil {
		t.Fatalf("insert user %d: %v", telegramID, err)
	}
}

func userCount(t *testing.T) int {
	t.Helper()
	var n int64
	if err := database.Get().Model(&database.User{}).Count(&n).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	return int(n)
}

func userExists(t *testing.T, telegramID int64) bool {
	t.Helper()
	var n int64
	database.Get().Model(&database.User{}).Where("telegram_id = ?", telegramID).Count(&n)
	return n > 0
}

// ---------------------------------------------------------------------------
// The member number, not the Telegram ID, reaches the API (priority 1)
// ---------------------------------------------------------------------------

// TestSweepQueriesMemberNumberNotTelegramID is the central regression guard: the legacy bug
// passed the Telegram ID to the /valid endpoint, so every member looked expired. Assert the
// OUTGOING request body carries the member number.
func TestSweepQueriesMemberNumberNotTelegramID(t *testing.T) {
	clearDB(t)
	const telegramID = int64(999999)
	const memberNumber = int64(42)
	insertUser(t, telegramID, memberNumber)

	api := newMockAPI(t, map[string]bool{"42": true}) // active member
	api.arm(t)
	t.Setenv("CLEANUP_DRY_RUN", "false")

	report, err := RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !api.sawMember("42") {
		t.Fatalf("the API was never asked about member number 42; seen=%v", api.seen)
	}
	if api.sawMember("999999") {
		t.Fatal("the API was queried with the TELEGRAM ID — this is the exact legacy bug that kicked everyone")
	}
	if report.Checked != 1 {
		t.Fatalf("checked = %d, want 1", report.Checked)
	}
	// Active member: nothing removed.
	if report.Deleted != 0 || report.Expired != 0 {
		t.Fatalf("an active member must not be touched: %+v", report)
	}
}

// ---------------------------------------------------------------------------
// A genuinely expired member IS removed (priority 1)
// ---------------------------------------------------------------------------

func TestExpiredMemberIsRemoved(t *testing.T) {
	clearDB(t)
	insertUser(t, 500001, 77) // no group associations => no Telegram calls needed to remove

	api := newMockAPI(t, map[string]bool{"77": false}) // expired
	api.arm(t)
	t.Setenv("CLEANUP_DRY_RUN", "false")

	report, err := RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.Expired != 1 {
		t.Fatalf("expired = %d, want 1: %+v", report.Expired, report)
	}
	if report.Deleted != 1 {
		t.Fatalf("deleted = %d, want 1: %+v", report.Deleted, report)
	}
	if userExists(t, 500001) {
		t.Fatal("an expired member should have been deleted")
	}
}

// TestNeverGroupSyncedMemberIsNotDeleted pins THE orphaning bug.
//
// An expired member with no associations and groups_synced_at NULL must be DEFERRED, never
// deleted. The two states are indistinguishable from the association table alone:
//
//	never probed          -> we have no idea which groups they are in
//	probed, truly empty   -> they are in none
//
// Reading the first as the second walks through the kick loop with nothing to kick, marks the
// sweep resolved, and drops the row — leaving an expired member inside the group forever with
// no record they ever existed. cleanupRemoveMember's error branch was written to prevent
// exactly that ("that would orphan them inside the groups forever"), but an empty list is not
// an error, so it sailed straight past.
//
// This is not hypothetical: before registration probed groups, EVERY member already in a
// group when the bot arrived had exactly this shape.
func TestNeverGroupSyncedMemberIsNotDeleted(t *testing.T) {
	clearDB(t)
	insertUser(t, 500042, 88, neverGroupSynced())

	api := newMockAPI(t, map[string]bool{"88": false}) // expired
	api.arm(t)
	t.Setenv("CLEANUP_DRY_RUN", "false")

	report, err := RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if report.Expired != 1 {
		t.Fatalf("expired = %d, want 1: %+v", report.Expired, report)
	}
	if report.Deleted != 0 {
		t.Fatalf("deleted = %d, want 0: a never-probed member must never be deleted: %+v",
			report.Deleted, report)
	}
	if report.Failed != 1 {
		t.Fatalf("failed = %d, want 1: the deferral must be reported, not silently skipped: %+v",
			report.Failed, report)
	}
	if !userExists(t, 500042) {
		t.Fatal("a member whose groups were never probed must survive the sweep: deleting them " +
			"orphans them inside their groups forever")
	}
}

// TestGroupSyncedButGrouplessMemberIsDeleted is the other half of the contract.
//
// Without this, "never delete on an empty list" would look like a safe blanket rule — and it
// would leak rows forever for anyone legitimately in no groups. The timestamp is what makes
// the empty list trustworthy.
func TestGroupSyncedButGrouplessMemberIsDeleted(t *testing.T) {
	clearDB(t)
	insertUser(t, 500043, 89) // groups_synced_at defaults to SET

	api := newMockAPI(t, map[string]bool{"89": false}) // expired
	api.arm(t)
	t.Setenv("CLEANUP_DRY_RUN", "false")

	report, err := RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.Deleted != 1 {
		t.Fatalf("deleted = %d, want 1: a probed member with genuinely no groups is deletable: %+v",
			report.Deleted, report)
	}
	if userExists(t, 500043) {
		t.Fatal("a probed, genuinely groupless expired member should have been deleted")
	}
}

// ---------------------------------------------------------------------------
// Fail-closed: any inconclusive API answer means NO deletion (priority 1)
// ---------------------------------------------------------------------------

func TestFailClosedNeverDeletes(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T) // arms env + API for a single expired-looking member (id 88)
	}{
		{
			name: "non-200 status",
			setup: func(t *testing.T) {
				api := newMockAPI(t, nil)
				api.status = http.StatusInternalServerError
				api.arm(t)
			},
		},
		{
			name: "empty/suspicious payload",
			setup: func(t *testing.T) {
				api := newMockAPI(t, nil)
				// zero-valued payload: this is what a regressed json tag looks like from inside
				// the sweep and MUST be treated as active, never as expired.
				api.rawBody = `{"username":"","name":"","is_membership_active":false}`
				api.arm(t)
			},
		},
		{
			name: "connection error / API unreachable",
			setup: func(t *testing.T) {
				// A server we immediately close: dialling it is a hard transport error, which
				// stands in for a timeout/outage. Must fail closed, not delete.
				srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
				url := srv.URL
				srv.Close()
				t.Setenv("API_ENDPOINT", url)
				t.Setenv("API_BEARER_TOKEN", "t")
				t.Setenv("CLEANUP_TELEGRAM_DELAY_MS", "0")
			},
		},
		{
			name: "malformed JSON body",
			setup: func(t *testing.T) {
				api := newMockAPI(t, nil)
				api.rawBody = `{ this is not json`
				api.arm(t)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearDB(t)
			insertUser(t, 600001, 88)
			tc.setup(t)
			t.Setenv("CLEANUP_DRY_RUN", "false") // armed for real deletion...

			report, err := RunOnce(context.Background())
			if err != nil {
				t.Fatalf("RunOnce: %v", err)
			}
			// ...but an inconclusive check must delete NOBODY.
			if report.Expired != 0 {
				t.Fatalf("%s: expired = %d, want 0 (fail-closed)", tc.name, report.Expired)
			}
			if report.Deleted != 0 {
				t.Fatalf("%s: deleted = %d, want 0 (fail-closed)", tc.name, report.Deleted)
			}
			if report.Inconclusive != 1 {
				t.Fatalf("%s: inconclusive = %d, want 1", tc.name, report.Inconclusive)
			}
			if !userExists(t, 600001) {
				t.Fatalf("%s: the member must survive an inconclusive check", tc.name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Circuit breaker aborts the whole sweep (priority 1)
// ---------------------------------------------------------------------------

// TestCircuitBreakerAbsoluteCap: more expired than CLEANUP_MAX_DELETIONS aborts the sweep
// and deletes NOBODY.
func TestCircuitBreakerAbsoluteCap(t *testing.T) {
	clearDB(t)
	active := map[string]bool{}
	for i := int64(1); i <= 12; i++ {
		insertUser(t, 700000+i, i)
		active[fmt.Sprintf("%d", i)] = false // all expired
	}

	api := newMockAPI(t, active)
	api.arm(t)
	t.Setenv("CLEANUP_DRY_RUN", "false")
	t.Setenv("CLEANUP_MAX_DELETIONS", "10")
	t.Setenv("CLEANUP_FRACTION_MIN_MEMBERS", "1000") // keep the fractional rule dormant

	report, err := RunOnce(context.Background())
	if err != ErrCircuitBreaker {
		t.Fatalf("expected ErrCircuitBreaker, got %v", err)
	}
	if !report.BreakerTripped {
		t.Fatal("report.BreakerTripped should be true")
	}
	if report.Deleted != 0 {
		t.Fatalf("the breaker must delete nobody, deleted = %d", report.Deleted)
	}
	if userCount(t) != 12 {
		t.Fatalf("all 12 members must survive a tripped breaker, %d remain", userCount(t))
	}
}

// TestCircuitBreakerFraction: too LARGE a fraction of the checked members aborts the sweep.
func TestCircuitBreakerFraction(t *testing.T) {
	clearDB(t)
	active := map[string]bool{}
	// 20 members, 6 expired (30% > 25% cap), 14 active.
	for i := int64(1); i <= 20; i++ {
		insertUser(t, 710000+i, i)
		active[fmt.Sprintf("%d", i)] = i > 6 // ids 1..6 expired
	}

	api := newMockAPI(t, active)
	api.arm(t)
	t.Setenv("CLEANUP_DRY_RUN", "false")
	t.Setenv("CLEANUP_MAX_DELETIONS", "1000") // absolute rule dormant
	t.Setenv("CLEANUP_MAX_FRACTION", "0.25")
	t.Setenv("CLEANUP_FRACTION_MIN_MEMBERS", "20")

	report, err := RunOnce(context.Background())
	if err != ErrCircuitBreaker {
		t.Fatalf("expected ErrCircuitBreaker, got %v (report=%+v)", err, report)
	}
	if !report.BreakerTripped || report.Deleted != 0 {
		t.Fatalf("fractional breaker must trip and delete nobody: %+v", report)
	}
	if userCount(t) != 20 {
		t.Fatalf("all 20 members must survive, %d remain", userCount(t))
	}
}

// TestBreakerDoesNotTripOnPlausibleExpiry: a small, plausible number of expiries proceeds.
func TestBreakerDoesNotTripOnPlausibleExpiry(t *testing.T) {
	clearDB(t)
	active := map[string]bool{}
	for i := int64(1); i <= 20; i++ {
		insertUser(t, 720000+i, i)
		active[fmt.Sprintf("%d", i)] = i > 2 // only ids 1,2 expired (10%)
	}

	api := newMockAPI(t, active)
	api.arm(t)
	t.Setenv("CLEANUP_DRY_RUN", "false")
	t.Setenv("CLEANUP_MAX_DELETIONS", "10")
	t.Setenv("CLEANUP_MAX_FRACTION", "0.25")
	t.Setenv("CLEANUP_FRACTION_MIN_MEMBERS", "20")

	report, err := RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.BreakerTripped {
		t.Fatalf("the breaker must NOT trip on a plausible expiry: %+v", report)
	}
	if report.Expired != 2 || report.Deleted != 2 {
		t.Fatalf("expected 2 expired and 2 deleted, got %+v", report)
	}
	if userCount(t) != 18 {
		t.Fatalf("18 members should remain, %d do", userCount(t))
	}
}

// ---------------------------------------------------------------------------
// Dry run changes nothing (priority 1)
// ---------------------------------------------------------------------------

func TestDryRunDeletesNothing(t *testing.T) {
	clearDB(t)
	insertUser(t, 800001, 55) // expired

	api := newMockAPI(t, map[string]bool{"55": false})
	api.arm(t)
	t.Setenv("CLEANUP_DRY_RUN", "true")

	report, err := RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !report.DryRun {
		t.Fatal("report.DryRun should be true")
	}
	if report.Expired != 1 {
		t.Fatalf("dry run must still DETECT the expiry, expired = %d", report.Expired)
	}
	if report.Deleted != 0 {
		t.Fatalf("dry run must delete nobody, deleted = %d", report.Deleted)
	}
	if !userExists(t, 800001) {
		t.Fatal("dry run must not remove the member")
	}
}

// TestDryRunIsTheDefault: with CLEANUP_DRY_RUN unset, the sweep is safe by default.
func TestDryRunIsTheDefault(t *testing.T) {
	clearDB(t)
	insertUser(t, 810001, 56)
	api := newMockAPI(t, map[string]bool{"56": false})
	api.arm(t)
	os.Unsetenv("CLEANUP_DRY_RUN") // ensure it is not set from a prior t.Setenv

	report, err := RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !report.DryRun {
		t.Fatal("dry-run must be the DEFAULT: an unconfigured sweep must not delete")
	}
	if report.Deleted != 0 || !userExists(t, 810001) {
		t.Fatal("the default sweep must delete nobody")
	}
}

// ---------------------------------------------------------------------------
// Members that must never be swept (priority 1 safety rail)
// ---------------------------------------------------------------------------

// TestInternationalAndBotAdminsAreSkipped: international members (unverifiable) and bot
// admins are skipped, never checked, never deleted — even when they look expired.
func TestInternationalAndBotAdminsAreSkipped(t *testing.T) {
	clearDB(t)
	insertUser(t, 900001, 61, international())
	insertUser(t, 900002, 62, botAdmin())
	insertUser(t, 900003, 63) // ordinary, expired -> should be removed

	api := newMockAPI(t, map[string]bool{"61": false, "62": false, "63": false})
	api.arm(t)
	t.Setenv("CLEANUP_DRY_RUN", "false")
	t.Setenv("CLEANUP_SKIP_BOT_ADMINS", "true")
	t.Setenv("CLEANUP_MAX_DELETIONS", "10")

	report, err := RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.Skipped != 2 {
		t.Fatalf("skipped = %d, want 2 (international + bot admin)", report.Skipped)
	}
	// The skipped members must never have been asked about.
	if api.sawMember("61") {
		t.Fatal("international member 61 must NOT be queried against the Italian API")
	}
	if api.sawMember("62") {
		t.Fatal("bot admin 62 must NOT be swept")
	}
	if !userExists(t, 900001) || !userExists(t, 900002) {
		t.Fatal("skipped members must survive")
	}
	// The ordinary expired member is removed.
	if userExists(t, 900003) {
		t.Fatal("the ordinary expired member should have been removed")
	}
	if report.Deleted != 1 {
		t.Fatalf("deleted = %d, want 1", report.Deleted)
	}
}

// TestMemberNumberZeroIsSkipped: a member with no membership number is never queried with
// the nonsense id 0 (the exact shape of the legacy massacre).
func TestMemberNumberZeroIsSkipped(t *testing.T) {
	clearDB(t)
	insertUser(t, 910001, 0)
	api := newMockAPI(t, nil)
	api.arm(t)
	t.Setenv("CLEANUP_DRY_RUN", "false")

	report, err := RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.Skipped != 1 || report.Checked != 0 {
		t.Fatalf("a member with number 0 must be skipped, not checked: %+v", report)
	}
	if api.sawMember("0") {
		t.Fatal("the API must never be queried with member id 0")
	}
	if !userExists(t, 910001) {
		t.Fatal("the member must survive")
	}
}

// ---------------------------------------------------------------------------
// Guardrails around the sweep entry point
// ---------------------------------------------------------------------------

// TestSweepAbortsWithoutEndpoint: no API_ENDPOINT means an aborted sweep, never a massacre.
func TestSweepAbortsWithoutEndpoint(t *testing.T) {
	clearDB(t)
	insertUser(t, 920001, 71)
	t.Setenv("API_ENDPOINT", "")
	t.Setenv("CLEANUP_DRY_RUN", "false")

	_, err := RunOnce(context.Background())
	if err == nil {
		t.Fatal("a sweep with no API_ENDPOINT must return an error and touch nobody")
	}
	if !userExists(t, 920001) {
		t.Fatal("nobody may be deleted when the endpoint is unset")
	}
}

// TestEmptyMembershipIsNoop.
func TestEmptyMembershipIsNoop(t *testing.T) {
	clearDB(t)
	api := newMockAPI(t, nil)
	api.arm(t)
	t.Setenv("CLEANUP_DRY_RUN", "false")

	report, err := RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.TotalMembers != 0 || report.Deleted != 0 {
		t.Fatalf("empty membership should be a no-op: %+v", report)
	}
}

// TestConcurrentRunOnceIsSerialised: a second overlapping RunOnce returns ErrSweepInProgress
// rather than double-kicking.
func TestConcurrentRunOnceIsSerialised(t *testing.T) {
	// Grab the sweep lock manually to simulate an in-flight sweep.
	if !running.CompareAndSwap(false, true) {
		t.Fatal("running flag should have been false at the start of the test")
	}
	defer running.Store(false)

	_, err := RunOnce(context.Background())
	if err != ErrSweepInProgress {
		t.Fatalf("expected ErrSweepInProgress, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Unit tests for the pure decision helpers
// ---------------------------------------------------------------------------

func TestBreakerTrippedHelper(t *testing.T) {
	cfg := cleanupConfig{maxDeletions: 10, maxFraction: 0.25, fractionMinMembers: 20}

	cases := []struct {
		name        string
		expired     int
		checked     int
		wantTripped bool
	}{
		{"under absolute cap", 5, 8, false},
		{"at absolute cap", 10, 12, false},
		{"over absolute cap", 11, 12, true},
		{"fraction dormant on small population", 3, 5, false}, // 60% but checked < 20
		{"fraction over on large population", 6, 20, true},    // 30% of 20
		{"fraction under on large population", 4, 20, false},  // 20% of 20
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, reason := cleanupBreakerTripped(c.expired, c.checked, cfg)
			if got != c.wantTripped {
				t.Fatalf("cleanupBreakerTripped(%d,%d) = %v (%q), want %v",
					c.expired, c.checked, got, reason, c.wantTripped)
			}
		})
	}
}

func TestIsSweepableHelper(t *testing.T) {
	cfg := cleanupConfig{skipBotAdmins: true}

	if _, ok := cleanupIsSweepable(&database.User{MemberNumber: 5}, cfg); !ok {
		t.Fatal("an ordinary member with a number must be sweepable")
	}
	if _, ok := cleanupIsSweepable(&database.User{MemberNumber: 5, IsInternational: true}, cfg); ok {
		t.Fatal("international members must not be sweepable")
	}
	if _, ok := cleanupIsSweepable(&database.User{MemberNumber: 0}, cfg); ok {
		t.Fatal("a member with number 0 must not be sweepable")
	}
	if _, ok := cleanupIsSweepable(&database.User{MemberNumber: 5, IsBotAdmin: true}, cfg); ok {
		t.Fatal("bot admins must not be sweepable when skipBotAdmins is on")
	}
	// With skipBotAdmins off, a bot admin becomes sweepable.
	if _, ok := cleanupIsSweepable(&database.User{MemberNumber: 5, IsBotAdmin: true}, cleanupConfig{skipBotAdmins: false}); !ok {
		t.Fatal("with skipBotAdmins off, a bot admin should be sweepable")
	}
}

// TestDTOSelfCheckPasses: the runtime DTO assertion must succeed against the correct DTO.
func TestDTOSelfCheckPasses(t *testing.T) {
	if err := cleanupAssertDTODecodes(); err != nil {
		t.Fatalf("the DTO self-check must pass with the correct json tags: %v", err)
	}
}

func TestIsGroupGoneHelper(t *testing.T) {
	gone := []string{"chat not found", "Bot is not a member of the group chat", "bot was kicked from the group"}
	for _, msg := range gone {
		if !membership.IsGroupGone(errStr(msg)) {
			t.Errorf("%q should be classified as group-gone", msg)
		}
	}
	if membership.IsGroupGone(errStr("temporary network blip")) {
		t.Error("a transient error must NOT be classified as group-gone")
	}
	if membership.IsGroupGone(nil) {
		t.Error("nil must not be group-gone")
	}
}

type errStr string

func (e errStr) Error() string { return string(e) }
