package api

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/luthfi9251/ku-dump/internal/cryptx"
	"github.com/luthfi9251/ku-dump/internal/engine"
	"github.com/luthfi9251/ku-dump/internal/meta"
	"github.com/luthfi9251/ku-dump/internal/runner"
	"github.com/luthfi9251/ku-dump/internal/storage"
)

type Deps struct {
	Store    *meta.Store
	Crypt    *cryptx.Cryptx
	Runner   *runner.Runner
	Engines  map[string]engine.Engine
	NewStore func(kind string) (storage.Store, error)
	Sessions *Sessions
	Limiter  *RateLimiter
}

type Server struct {
	Deps
	mux *http.ServeMux
}

func NewServer(d Deps) http.Handler {
	if d.Sessions == nil || d.Limiter == nil {
		panic("api: Sessions and Limiter are required")
	}
	s := &Server{Deps: d, mux: http.NewServeMux()}
	s.routes()
	return s.withLogging(s.mux)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/auth/status", s.handleAuthStatus)
	s.mux.HandleFunc("POST /api/auth/setup", s.handleSetup)
	s.mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	s.mux.HandleFunc("GET /api/me", s.auth(s.handleMe))
	s.mux.HandleFunc("/", s.handleNotFound)
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	fail(w, http.StatusNotFound, "NOT_FOUND", "route not found")
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	uid := userID(r)
	u, err := s.Store.GetUserByID(r.Context(), uid)
	if err != nil {
		fail(w, http.StatusUnauthorized, "UNAUTHORIZED", "user not found")
		return
	}
	jsonOut(w, http.StatusOK, map[string]any{"id": encID(u.ID), "username": u.Username})
}

type ctxKey int

const userIDKey ctxKey = 1

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			fail(w, http.StatusUnauthorized, "UNAUTHORIZED", "login required")
			return
		}
		uid, ok := s.Sessions.Parse(c.Value)
		if !ok {
			fail(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid session")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userIDKey, uid)))
	}
}

func userID(r *http.Request) int64 {
	uid, _ := r.Context().Value(userIDKey).(int64)
	return uid
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			if p := recover(); p != nil {
				log.Printf("panic %s %s: %v", r.Method, r.URL.Path, p)
				fail(rec, http.StatusInternalServerError, "INTERNAL", "internal error")
			}
		}()
		next.ServeHTTP(rec, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rec.status, time.Since(start))
	})
}
