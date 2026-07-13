// Package server hosts the inbound HTTP surface of the bot.
//
// It serves PLAIN HTTP: TLS is terminated by a reverse proxy in front of the
// service, so there is no certificate handling here (the legacy bot called
// gin's RunTLS with CERT_PATH/KEY_PATH; that is deliberately gone).
//
// Routes:
//
//	GET  /health            liveness probe for compose/k8s healthchecks
//	POST /callme_api/{token} the Mensa app identity-verification callback (see callme_handler.go)
package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/Bombaclath97/mensa-telegram-bot/logger"
)

// DefaultAddr is the bind address used when HTTP_ADDR is unset.
const DefaultAddr = ":8080"

const (
	// shutdownTimeout bounds how long in-flight requests get to finish on shutdown.
	shutdownTimeout = 10 * time.Second

	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 60 * time.Second

	// maxBodyBytes caps the request body. The only body we accept is {"accepted": bool}.
	maxBodyBytes = 4 << 10 // 4 KiB
)

// Server wraps the stdlib http.Server that hosts the callme webhook.
type Server struct {
	addr string
	http *http.Server
}

// New builds the HTTP server, binding to HTTP_ADDR (default ":8080").
// It does not listen until Run is called.
func New() *Server {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = DefaultAddr
	}
	return NewWithAddr(addr)
}

// NewWithAddr builds the HTTP server on an explicit bind address.
func NewWithAddr(addr string) *Server {
	s := &Server{addr: addr}

	mux := http.NewServeMux()
	// Go 1.22+ method+wildcard patterns: no router dependency needed.
	mux.HandleFunc("GET /health", healthHandler)
	mux.HandleFunc("POST /callme_api/{token}", CallmeHandler)

	s.http = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
	return s
}

// Addr returns the bind address the server was configured with.
func (s *Server) Addr() string {
	return s.addr
}

// Run starts the server and blocks until ctx is cancelled or the listener fails.
//
// On cancellation it drains in-flight requests with a bounded graceful shutdown and
// returns nil. It returns a non-nil error only when the server could not serve (e.g.
// the port is already taken) or the graceful shutdown timed out.
//
// Intended usage from main:
//
//	srv := server.New()
//	go func() {
//		if err := srv.Run(ctx); err != nil {
//			logger.Error("http server stopped", map[string]interface{}{"error": err.Error()})
//		}
//	}()
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		logger.Info("Starting HTTP server", map[string]interface{}{"addr": s.addr})
		if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		if err != nil {
			logger.Error("HTTP server failed", map[string]interface{}{
				"addr":  s.addr,
				"error": err.Error(),
			})
		}
		return err
	case <-ctx.Done():
	}

	logger.Info("Shutting down HTTP server", map[string]interface{}{"addr": s.addr})

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := s.http.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server graceful shutdown failed", map[string]interface{}{
			"error": err.Error(),
		})
		return err
	}

	// Drain the serve goroutine so it cannot outlive Run.
	<-errCh

	logger.Info("HTTP server stopped", nil)
	return nil
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// writeJSON writes a JSON body with the given status code.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		logger.Warn("Failed to write HTTP response body", map[string]interface{}{
			"error": err.Error(),
		})
	}
}
