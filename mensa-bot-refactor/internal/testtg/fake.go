// Package testtg is a fake Telegram Bot API server for tests.
//
// The destructive paths (the cleanup sweep's kick/demote/owner branches and
// handlers.DeleteMemberEverywhere) talk to Telegram through the telegram_bot singleton. This
// package stands up an httptest server speaking just enough of the Bot API for those paths,
// points a real go-telegram/bot client at it, and records every call so a test can assert
// exactly who was kicked, demoted or messaged — and, just as importantly, who was not.
//
// The telegram_bot singleton can only be initialised once per test binary, so Install is
// meant to be called from TestMain, and Reset between tests.
package testtg

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"

	"github.com/go-telegram/bot"

	tb "github.com/Bombaclath97/mensa-telegram-bot/telegram_bot"
)

// Call is one recorded Bot API request.
type Call struct {
	Method string
	ChatID int64
	UserID int64
	Text   string
}

// APIError makes the fake answer a method with {"ok":false}.
type APIError struct {
	Code        int
	Description string
	// RetryAfter is sent as parameters.retry_after, which Telegram sets on a 429.
	RetryAfter int
}

// ErrTooManyRequests is Telegram's rate-limit answer.
var ErrTooManyRequests = &APIError{Code: 429, Description: "Too Many Requests: retry after 1", RetryAfter: 1}

// ErrChatNotFound is what Telegram answers for a chat the bot can no longer see.
var ErrChatNotFound = &APIError{Code: 400, Description: "Bad Request: chat not found"}

// ErrTransient is a generic failure that is NOT a "group is gone" signal.
var ErrTransient = &APIError{Code: 500, Description: "Internal Server Error"}

type key struct {
	method string
	chatID int64
	userID int64
}

// Server is the fake. All exported methods are safe for concurrent use.
type Server struct {
	srv *httptest.Server

	mu       sync.Mutex
	calls    []Call
	statuses map[[2]int64]string // (chat, user) -> chat member status
	titles   map[int64]string    // chat -> title, for getChat
	failures map[key]*failure    // (method, chat, user) -> error; user 0 matches any user
}

type failure struct {
	err       *APIError
	remaining int // calls left to fail; -1 fails forever
}

// Install starts the fake and wires the telegram_bot singleton to it. Call it once, from
// TestMain; the returned Server lives for the whole test binary.
func Install() (*Server, error) {
	s := &Server{}
	s.Reset()
	s.srv = httptest.NewServer(http.HandlerFunc(s.handle))

	b, err := bot.New("123456:TEST", bot.WithServerURL(s.srv.URL), bot.WithSkipGetMe())
	if err != nil {
		s.srv.Close()
		return nil, err
	}
	tb.Init(b)
	return s, nil
}

// Close stops the server.
func (s *Server) Close() { s.srv.Close() }

// Reset forgets recorded calls and every configured status/failure.
func (s *Server) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = nil
	s.statuses = map[[2]int64]string{}
	s.titles = map[int64]string{}
	s.failures = map[key]*failure{}
}

// SetStatus sets what getChatMember reports for user in chat: "creator", "administrator",
// "member", "restricted", "left" or "kicked". Unset pairs answer "member".
func (s *Server) SetStatus(chatID, userID int64, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statuses[[2]int64{chatID, userID}] = status
}

// SetTitle sets what getChat reports as the chat's title.
func (s *Server) SetTitle(chatID int64, title string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.titles[chatID] = title
}

// Fail makes method fail for (chat, user). userID 0 matches any user in that chat.
func (s *Server) Fail(method string, chatID, userID int64, e *APIError) {
	s.FailTimes(method, chatID, userID, e, -1)
}

// FailTimes is Fail for only the next n matching calls; after that the method succeeds.
// n < 0 fails forever.
func (s *Server) FailTimes(method string, chatID, userID int64, e *APIError, n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures[key{method, chatID, userID}] = &failure{err: e, remaining: n}
}

// Calls returns every recorded call, optionally filtered to one method.
func (s *Server) Calls(method string) []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Call
	for _, c := range s.calls {
		if method == "" || c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

// Called reports whether method was called for (chat, user). userID 0 matches any user.
func (s *Server) Called(method string, chatID, userID int64) bool {
	for _, c := range s.Calls(method) {
		if c.ChatID == chatID && (userID == 0 || c.UserID == userID) {
			return true
		}
	}
	return false
}

// Kicked reports whether user was kicked from chat (a ban followed by an unban).
func (s *Server) Kicked(chatID, userID int64) bool {
	return s.Called("banChatMember", chatID, userID) && s.Called("unbanChatMember", chatID, userID)
}

// Demoted reports whether user had their admin rights stripped in chat.
func (s *Server) Demoted(chatID, userID int64) bool {
	return s.Called("promoteChatMember", chatID, userID)
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	// Path is /bot<token>/<method>.
	method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	_ = r.ParseMultipartForm(1 << 20)

	chatID, _ := strconv.ParseInt(r.FormValue("chat_id"), 10, 64)
	userID, _ := strconv.ParseInt(r.FormValue("user_id"), 10, 64)

	s.mu.Lock()
	s.calls = append(s.calls, Call{Method: method, ChatID: chatID, UserID: userID, Text: r.FormValue("text")})
	f := s.failures[key{method, chatID, userID}]
	if f == nil || f.remaining == 0 {
		f = s.failures[key{method, chatID, 0}]
	}
	var apiErr *APIError
	if f != nil && f.remaining != 0 {
		apiErr = f.err
		if f.remaining > 0 {
			f.remaining--
		}
	}
	status := s.statuses[[2]int64{chatID, userID}]
	title := s.titles[chatID]
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if apiErr != nil {
		body := map[string]any{"ok": false, "error_code": apiErr.Code, "description": apiErr.Description}
		if apiErr.RetryAfter > 0 {
			body["parameters"] = map[string]any{"retry_after": apiErr.RetryAfter}
		}
		_ = json.NewEncoder(w).Encode(body)
		return
	}

	var result any
	switch method {
	case "getChatMember":
		if status == "" {
			status = "member"
		}
		m := map[string]any{"status": status, "user": map[string]any{"id": userID, "is_bot": false, "first_name": "U"}}
		if status == "administrator" {
			m["can_be_edited"] = true
		}
		result = m
	case "sendMessage":
		result = map[string]any{
			"message_id": 1,
			"date":       0,
			"chat":       map[string]any{"id": chatID, "type": "private"},
			"text":       r.FormValue("text"),
		}
	case "getChat":
		result = map[string]any{"id": chatID, "type": "supergroup", "title": title, "accent_color_id": 0, "max_reaction_count": 0}
	case "getChatAdministrators":
		result = []any{}
	case "banChatMember", "unbanChatMember", "promoteChatMember", "deleteMessage", "answerCallbackQuery":
		result = true
	default:
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": false, "error_code": 404, "description": fmt.Sprintf("Not Found: fake has no %s", method),
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
}
