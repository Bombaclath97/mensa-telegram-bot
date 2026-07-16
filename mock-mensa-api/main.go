// Command mock-mensa-api stands in for the Mensa Italia membership API so the bot's
// registration flow can be exercised end to end without touching production.
//
// It plays BOTH halves of the real interaction:
//
//  1. the bot asks it   — POST /v1/request  (email, member_id, callme_url)
//     POST /v1/valid    (member_id)
//  2. it calls the bot back, impersonating the official Mensa app —
//     POST {callme_url} {"accepted": true|false}
//
// That callback is the whole point: it is the only thing that proves a member's identity,
// and without a server that performs it the registration can never complete.
//
// Everything is stdlib. See README.md for the control API.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ApproveMode decides what the fake Mensa app does when the bot asks it to verify someone.
type ApproveMode string

const (
	// ModeAuto approves every pending request after CallbackDelay — the happy path.
	ModeAuto ApproveMode = "auto"
	// ModeReject rejects every pending request (the user tapping "no" in the app).
	ModeReject ApproveMode = "reject"
	// ModeManual fires no callback at all until you POST /_control/approve/{token}.
	// Use it to test the approval timeout, or to drive the flow by hand.
	ModeManual ApproveMode = "manual"
)

// Member is a fake Mensa member.
type Member struct {
	Email        string `json:"email"`
	MemberNumber int64  `json:"member_number"`
	Name         string `json:"name"`
	Active       bool   `json:"active"` // drives /valid, i.e. the expiry cleanup sweep
}

// Pending is a verification request the bot has made and the "app" has not answered yet.
type Pending struct {
	Token        string    `json:"token"`
	Email        string    `json:"email"`
	MemberNumber int64     `json:"member_number"`
	CallmeURL    string    `json:"callme_url"`
	CreatedAt    time.Time `json:"created_at"`
}

type server struct {
	mu        sync.Mutex
	members   []Member
	pending   map[string]Pending // token -> request
	mode      ApproveMode
	delay     time.Duration
	acceptAny bool

	client *http.Client
}

func main() {
	s := &server{
		pending: map[string]Pending{},
		mode:    ApproveMode(env("MOCK_APPROVE_MODE", string(ModeAuto))),
		delay:   envDuration("MOCK_CALLBACK_DELAY", 2*time.Second),
		// When true, ANY (email, member number) pair is treated as a real member. Off by
		// default so the "you are not a member" path is reachable and testable.
		acceptAny: env("MOCK_ACCEPT_ANY", "false") == "true",
		client:    &http.Client{Timeout: 10 * time.Second},
	}
	s.members = parseMembers(env("MOCK_MEMBERS", defaultMembers))

	mux := http.NewServeMux()

	// --- the API the bot talks to (mirrors the real Mensa API) ---
	mux.HandleFunc("POST /v1/request", s.handleRequest)
	mux.HandleFunc("POST /v1/valid", s.handleValid)

	// --- control plane, for driving tests ---
	mux.HandleFunc("GET /_control/state", s.handleState)
	mux.HandleFunc("POST /_control/mode/{mode}", s.handleSetMode)
	mux.HandleFunc("POST /_control/approve/{token}", s.handleManual(true))
	mux.HandleFunc("POST /_control/reject/{token}", s.handleManual(false))
	mux.HandleFunc("POST /_control/members", s.handleSetMembers)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	addr := env("LISTEN_ADDR", ":8081")
	log.Printf("mock-mensa-api listening on %s | mode=%s delay=%s accept_any=%v", addr, s.mode, s.delay, s.acceptAny)
	for _, m := range s.members {
		log.Printf("  member: %s / %d  active=%v", m.Email, m.MemberNumber, m.Active)
	}
	log.Fatal(http.ListenAndServe(addr, logging(mux)))
}

// handleRequest is POST /v1/request: the bot asking us to push an approval to the member's
// app. HTTP 200 == "this pair is a real member and the push was sent".
func (s *server) handleRequest(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	memberID, _ := strconv.ParseInt(r.FormValue("member_id"), 10, 64)
	callmeURL := r.FormValue("callme_url")

	if callmeURL == "" {
		http.Error(w, "callme_url is required", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	member, ok := s.find(email, memberID)
	acceptAny, mode, delay := s.acceptAny, s.mode, s.delay
	s.mu.Unlock()

	if !ok && !acceptAny {
		// A 4xx is how the real API says "no such member". The bot turns this into
		// ErrMemberNotFound and tells the user their details do not match.
		log.Printf("REQUEST  ✗ no member for %s / %d -> 404", email, memberID)
		http.Error(w, "no member matches that email and membership number", http.StatusNotFound)
		return
	}
	if !ok {
		member = Member{Email: email, MemberNumber: memberID, Name: "Accept-Any Member", Active: true}
	}

	// The token is the last path segment of the callme URL; the bot uses it to authenticate
	// the callback, so we must echo back exactly the URL we were given.
	token := callmeURL[strings.LastIndex(callmeURL, "/")+1:]

	s.mu.Lock()
	s.pending[token] = Pending{
		Token: token, Email: member.Email, MemberNumber: member.MemberNumber,
		CallmeURL: callmeURL, CreatedAt: time.Now(),
	}
	s.mu.Unlock()

	log.Printf("REQUEST  ✓ %s / %d -> 200 (token=%s, mode=%s)", member.Email, member.MemberNumber, token, mode)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "sent": true})

	// Now play the Mensa app: after a beat, call the bot back.
	switch mode {
	case ModeAuto:
		go s.fireCallback(token, true, delay)
	case ModeReject:
		go s.fireCallback(token, false, delay)
	case ModeManual:
		log.Printf("         manual mode: POST /_control/approve/%s to complete it", token)
	}
}

// fireCallback impersonates the Mensa app calling the bot's webhook.
func (s *server) fireCallback(token string, accepted bool, delay time.Duration) {
	time.Sleep(delay)

	s.mu.Lock()
	p, ok := s.pending[token]
	if ok {
		delete(s.pending, token)
	}
	s.mu.Unlock()
	if !ok {
		log.Printf("CALLBACK ? unknown token %s (already answered?)", token)
		return
	}

	body, _ := json.Marshal(map[string]bool{"accepted": accepted})
	resp, err := s.client.Post(p.CallmeURL, "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("CALLBACK ✗ POST %s failed: %v", p.CallmeURL, err)
		return
	}
	defer resp.Body.Close()
	log.Printf("CALLBACK %s POST %s {\"accepted\":%v} -> %d",
		map[bool]string{true: "✓", false: "✓(reject)"}[accepted], p.CallmeURL, accepted, resp.StatusCode)
}

// handleValid is POST /v1/valid: the membership-expiry check the cleanup sweep uses.
// NOTE the snake_case field — a camelCase tag on the bot's side decodes to false for every
// member, which would make the sweep kick and delete the entire membership.
func (s *server) handleValid(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	memberID, _ := strconv.ParseInt(r.FormValue("member_id"), 10, 64)

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.members {
		if m.MemberNumber == memberID {
			log.Printf("VALID    %d -> active=%v", memberID, m.Active)
			writeJSON(w, http.StatusOK, map[string]any{
				"username":             m.Email,
				"name":                 m.Name,
				"is_membership_active": m.Active,
			})
			return
		}
	}
	if s.acceptAny {
		log.Printf("VALID    %d -> active=true (accept_any)", memberID)
		writeJSON(w, http.StatusOK, map[string]any{
			"username": "unknown", "name": "Accept-Any Member", "is_membership_active": true,
		})
		return
	}
	log.Printf("VALID    %d -> 404 unknown member", memberID)
	http.Error(w, "unknown member", http.StatusNotFound)
}

// --- control plane ---

func (s *server) handleState(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pend := make([]Pending, 0, len(s.pending))
	for _, p := range s.pending {
		pend = append(pend, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"mode": s.mode, "callback_delay": s.delay.String(), "accept_any": s.acceptAny,
		"members": s.members, "pending": pend,
	})
}

func (s *server) handleSetMode(w http.ResponseWriter, r *http.Request) {
	m := ApproveMode(r.PathValue("mode"))
	switch m {
	case ModeAuto, ModeReject, ModeManual:
	default:
		http.Error(w, "mode must be auto | reject | manual", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.mode = m
	s.mu.Unlock()
	log.Printf("CONTROL  mode=%s", m)
	writeJSON(w, http.StatusOK, map[string]any{"mode": m})
}

// handleManual fires the callback for a token that is waiting in manual mode.
func (s *server) handleManual(accepted bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.PathValue("token")
		s.mu.Lock()
		_, ok := s.pending[token]
		s.mu.Unlock()
		if !ok {
			http.Error(w, "no pending request for that token", http.StatusNotFound)
			return
		}
		go s.fireCallback(token, accepted, 0)
		writeJSON(w, http.StatusOK, map[string]any{"token": token, "accepted": accepted})
	}
}

func (s *server) handleSetMembers(w http.ResponseWriter, r *http.Request) {
	var in []Member
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "expected a JSON array of members", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.members = in
	s.mu.Unlock()
	log.Printf("CONTROL  members replaced (%d)", len(in))
	writeJSON(w, http.StatusOK, map[string]any{"members": in})
}

// --- helpers ---

func (s *server) find(email string, memberNumber int64) (Member, bool) {
	for _, m := range s.members {
		if strings.EqualFold(m.Email, email) && m.MemberNumber == memberNumber {
			return m, true
		}
	}
	return Member{}, false
}

// defaultMembers seeds one active member and one expired one, so both the happy path and
// the cleanup sweep are testable out of the box.
const defaultMembers = "mario.rossi@mensa.it:12345:Mario Rossi:active,anna.bianchi@mensa.it:67890:Anna Bianchi:expired"

// parseMembers reads "email:number:name:active|expired" entries, comma separated.
func parseMembers(s string) []Member {
	var out []Member
	for _, entry := range strings.Split(s, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.Split(entry, ":")
		if len(parts) < 2 {
			log.Printf("skipping malformed MOCK_MEMBERS entry %q", entry)
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil {
			log.Printf("skipping MOCK_MEMBERS entry %q: bad member number", entry)
			continue
		}
		m := Member{Email: strings.TrimSpace(parts[0]), MemberNumber: n, Active: true}
		if len(parts) > 2 {
			m.Name = strings.TrimSpace(parts[2])
		}
		if len(parts) > 3 {
			m.Active = strings.TrimSpace(parts[3]) != "expired"
		}
		out = append(out, m)
	}
	return out
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			log.Printf("--> %s %s", r.Method, r.URL.Path)
		}
		next.ServeHTTP(w, r)
	})
}

func env(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func envDuration(k string, def time.Duration) time.Duration {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		log.Printf("%s=%q is not a duration (e.g. 2s); using %s", k, v, def)
	}
	return def
}
