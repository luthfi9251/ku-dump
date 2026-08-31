package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const sessionCookie = "ku_dump_session"

type Sessions struct {
	secret []byte
	ttl    time.Duration
}

func NewSessions(secret []byte) *Sessions {
	return &Sessions{secret: secret, ttl: 24 * time.Hour}
}

func (s *Sessions) sign(payload string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Sessions) NewToken(userID int64) string {
	payload := fmt.Sprintf("%d|%d", userID, time.Now().Add(s.ttl).Unix())
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + s.sign(payload)
}

func (s *Sessions) Parse(token string) (int64, bool) {
	dot := strings.LastIndexByte(token, '.')
	if dot <= 0 {
		return 0, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(token[:dot])
	if err != nil {
		return 0, false
	}
	payload := string(raw)
	if subtle.ConstantTimeCompare([]byte(s.sign(payload)), []byte(token[dot+1:])) != 1 {
		return 0, false
	}
	parts := strings.Split(payload, "|")
	if len(parts) != 2 {
		return 0, false
	}
	uid, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, false
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return 0, false
	}
	return uid, true
}

type limiterEntry struct {
	count int
	reset time.Time
}

type RateLimiter struct {
	mu      sync.Mutex
	max     int
	window  time.Duration
	entries map[string]*limiterEntry
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{max: 5, window: 15 * time.Minute, entries: map[string]*limiterEntry{}}
}

func (r *RateLimiter) Allowed(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	e, ok := r.entries[key]
	if !ok || now.After(e.reset) {
		r.entries[key] = &limiterEntry{count: 1, reset: now.Add(r.window)}
		return true
	}
	e.count++
	return e.count <= r.max
}

func (r *RateLimiter) Reset(key string) {
	r.mu.Lock()
	delete(r.entries, key)
	r.mu.Unlock()
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type authRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	n, err := s.Store.CountUsers(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	jsonOut(w, http.StatusOK, map[string]bool{"needsSetup": n == 0})
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	n, err := s.Store.CountUsers(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	if n > 0 {
		fail(w, http.StatusConflict, "ALREADY_SETUP", "admin user already exists")
		return
	}
	var req authRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if len(req.Username) < 3 || len(req.Username) > 64 {
		fail(w, http.StatusBadRequest, "VALIDATION", "username must be 3-64 characters")
		return
	}
	if len(req.Password) < 8 {
		fail(w, http.StatusBadRequest, "VALIDATION", "password must be at least 8 characters")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		fail(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	id, err := s.Store.CreateUser(r.Context(), req.Username, string(hash))
	if err != nil {
		fail(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	jsonOut(w, http.StatusCreated, map[string]any{"id": encID(id), "username": req.Username})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req authRequest
	if !decodeBody(w, r, &req) {
		return
	}
	key := req.Username + "|" + clientIP(r)
	if !s.Limiter.Allowed(key) {
		fail(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many failed attempts, try again later")
		return
	}
	u, err := s.Store.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		fail(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid username or password")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
		fail(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid username or password")
		return
	}
	s.Limiter.Reset(key)
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: s.Sessions.NewToken(u.ID), Path: "/",
		HttpOnly: true, MaxAge: 86400, SameSite: http.SameSiteLaxMode,
	})
	jsonOut(w, http.StatusOK, map[string]any{"id": encID(u.ID), "username": u.Username})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	jsonOut(w, http.StatusOK, map[string]bool{"ok": true})
}
