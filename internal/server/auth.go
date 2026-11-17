package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"paihub/internal/config"
)

const (
	sessionCookieName  = "paihub_session"
	maxLoginFailures   = 5
	loginLockoutPeriod = 15 * time.Second
)

type loginAttempt struct {
	failures    int
	lockedUntil time.Time
}

type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]loginAttempt
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{attempts: make(map[string]loginAttempt)}
}

func clientIPKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil || host == "" {
		return r.RemoteAddr
	}
	return host
}

func (l *loginLimiter) isBlocked(key string) (bool, time.Duration) {
	if l == nil {
		return false, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	att, ok := l.attempts[key]
	if !ok {
		return false, 0
	}
	if time.Now().Before(att.lockedUntil) {
		return true, time.Until(att.lockedUntil).Round(time.Second)
	}
	return false, 0
}

func (l *loginLimiter) recordFailure(key string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	if len(l.attempts) > 256 {
		for k, v := range l.attempts {
			if now.After(v.lockedUntil) && v.failures >= maxLoginFailures {
				delete(l.attempts, k)
			}
		}
	}

	att := l.attempts[key]
	if now.After(att.lockedUntil) && att.failures >= maxLoginFailures {
		att.failures = 0
	}
	att.failures++
	if att.failures >= maxLoginFailures {
		att.lockedUntil = now.Add(loginLockoutPeriod)
	}
	l.attempts[key] = att
}

func (l *loginLimiter) reset(key string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	delete(l.attempts, key)
	l.mu.Unlock()
}

func (s *Server) expectedToken(pin string) string {
	mac := hmac.New(sha256.New, s.authSecret)
	mac.Write([]byte(pin))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Server) isAuthenticated(r *http.Request) bool {
	cfg := s.cfgStore.Get()
	if cfg.Pin == "" {
		return true
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return false
	}
	exp := s.expectedToken(cfg.Pin)
	return subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(exp)) == 1
}

func (s *Server) setSessionCookie(w http.ResponseWriter, pin string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    s.expectedToken(pin),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400 * 30,
	})
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.isAuthenticated(r) {
			writeError(w, http.StatusUnauthorized, "PIN required")
			return
		}
		next(w, r)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": AppVersion})
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfgStore.Get()
	writeJSON(w, http.StatusOK, map[string]any{
		"pinRequired":   cfg.Pin != "",
		"authenticated": s.isAuthenticated(r),
	})
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	ipKey := clientIPKey(r)
	if blocked, retryAfter := s.loginLimiter.isBlocked(ipKey); blocked {
		if retryAfter < time.Second {
			retryAfter = time.Second
		}
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf("Too many PIN attempts. Try again in %s.", retryAfter))
		return
	}

	var body struct {
		Pin string `json:"pin"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := s.cfgStore.Get()
	if cfg.Pin != "" && subtle.ConstantTimeCompare([]byte(body.Pin), []byte(cfg.Pin)) != 1 {
		s.loginLimiter.recordFailure(ipKey)
		writeError(w, http.StatusUnauthorized, "Invalid PIN")
		return
	}
	s.loginLimiter.reset(ipKey)
	s.setSessionCookie(w, cfg.Pin)
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true})
}

func (s *Server) handleAuthPin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Pin string `json:"pin"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	newPin := strings.TrimSpace(body.Pin)
	if err := s.cfgStore.Update(func(c *config.Config) {
		c.Pin = newPin
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if newPin != "" {
		s.setSessionCookie(w, newPin)
	} else {
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   -1,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"pinRequired": newPin != ""})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := s.hub.subscribe()
	defer s.hub.unsubscribe(ch)

	fmt.Fprintf(w, "event: connected\ndata: {\"ok\":true}\n\n")
	flusher.Flush()

	keepAlive := time.NewTicker(25 * time.Second)
	defer keepAlive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", msg.event, msg.data)
			flusher.Flush()
		case <-keepAlive.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}
