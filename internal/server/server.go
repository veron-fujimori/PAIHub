package server

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"paihub/internal/agents"
	"paihub/internal/config"
	"paihub/internal/shell"
)

const (
	AppVersion     = "1.0.0"
	maxRequestBody = 4 << 20
)

type eventHub struct {
	mu      sync.Mutex
	clients map[chan sseMsg]struct{}
}

type sseMsg struct {
	event string
	data  string
}

func newEventHub() *eventHub {
	return &eventHub{
		clients: make(map[chan sseMsg]struct{}),
	}
}

func (h *eventHub) subscribe() chan sseMsg {
	ch := make(chan sseMsg, 16)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *eventHub) unsubscribe(ch chan sseMsg) {
	h.mu.Lock()
	delete(h.clients, ch)
	close(ch)
	h.mu.Unlock()
}

func (h *eventHub) broadcast(event string, payload any) {
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	msg := sseMsg{event: event, data: string(b)}
	h.mu.Lock()
	for ch := range h.clients {
		select {
		case ch <- msg:
		default:
		}
	}
	h.mu.Unlock()
}

type sseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	mu      sync.Mutex
}

func newSSEWriter(w http.ResponseWriter) (*sseWriter, bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	return &sseWriter{w: w, flusher: flusher}, true
}

func (sw *sseWriter) send(event string, payload any) {
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	sw.mu.Lock()
	fmt.Fprintf(sw.w, "event: %s\ndata: %s\n\n", event, string(b))
	sw.flusher.Flush()
	sw.mu.Unlock()
}

type Server struct {
	selfDir      string
	cfgStore     *config.Store
	agentMgr     *agents.Manager
	termRunner   *shell.Runner
	hub          *eventHub
	authSecret   []byte
	loginLimiter *loginLimiter
	sessionsMu   sync.Mutex
	publicFS     fs.FS
}

func New(selfDir string, cfgStore *config.Store, agentMgr *agents.Manager, publicFS fs.FS) (*Server, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	return &Server{
		selfDir:      selfDir,
		cfgStore:     cfgStore,
		agentMgr:     agentMgr,
		termRunner:   shell.NewRunner(),
		hub:          newEventHub(),
		authSecret:   secret,
		loginLimiter: newLoginLimiter(),
		publicFS:     publicFS,
	}, nil
}

func (s *Server) Stop() {
	s.agentMgr.Stop()
	s.termRunner.Stop()
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/auth/status", s.handleAuthStatus)
	mux.HandleFunc("POST /api/auth/login", s.handleAuthLogin)
	mux.HandleFunc("POST /api/auth/pin", s.withAuth(s.handleAuthPin))

	mux.HandleFunc("GET /api/events", s.withAuth(s.handleEvents))

	mux.HandleFunc("GET /api/workspaces", s.withAuth(s.handleWorkspacesGet))
	mux.HandleFunc("GET /api/sessions", s.withAuth(s.handleSessionsGet))
	mux.HandleFunc("PUT /api/sessions", s.withAuth(s.handleSessionsPut))
	mux.HandleFunc("GET /api/workspaces/browse", s.withAuth(s.handleWorkspacesBrowse))
	mux.HandleFunc("POST /api/workspaces/mkdir", s.withAuth(s.handleWorkspacesMkdir))
	mux.HandleFunc("POST /api/workspaces/pick-native", s.withAuth(s.handleWorkspacesPickNative))
	mux.HandleFunc("POST /api/workspaces/select", s.withAuth(s.handleWorkspacesSelect))
	mux.HandleFunc("GET /api/workspaces/file", s.withAuth(s.handleFileRead))
	mux.HandleFunc("POST /api/workspaces/file", s.withAuth(s.handleFileWrite))

	mux.HandleFunc("GET /api/github/repos", s.withAuth(s.handleGitHubRepos))
	mux.HandleFunc("POST /api/github/clone", s.withAuth(s.handleGitHubClone))

	mux.HandleFunc("GET /api/agents", s.withAuth(s.handleAgentsList))
	mux.HandleFunc("POST /api/agents", s.withAuth(s.handleAgentsCreate))
	mux.HandleFunc("PUT /api/agents/{id}", s.withAuth(s.handleAgentsUpdate))
	mux.HandleFunc("DELETE /api/agents/{id}", s.withAuth(s.handleAgentsDelete))

	mux.HandleFunc("POST /api/chat/stream", s.withAuth(s.handleChatStream))
	mux.HandleFunc("POST /api/chat/stop", s.withAuth(s.handleChatStop))

	mux.HandleFunc("GET /api/terminal/tools", s.withAuth(s.handleTerminalTools))
	mux.HandleFunc("POST /api/terminal/exec", s.withAuth(s.handleTerminalExec))
	mux.HandleFunc("POST /api/terminal/stop", s.withAuth(s.handleTerminalStop))
	mux.HandleFunc("POST /api/terminal/login-window", s.withAuth(s.handleTerminalLoginWindow))

	mux.HandleFunc("GET /api/system/stats", s.withAuth(s.handleSystemStats))
	mux.HandleFunc("GET /api/config/defaults", s.withAuth(s.handleDefaultsGet))
	mux.HandleFunc("POST /api/config/defaults", s.withAuth(s.handleDefaultsSet))

	publicDir := filepath.Join(s.selfDir, "public")
	var staticFS http.FileSystem
	if info, err := os.Stat(filepath.Join(publicDir, "index.html")); err == nil && !info.IsDir() {
		staticFS = http.Dir(publicDir)
	} else if s.publicFS != nil {
		staticFS = http.FS(s.publicFS)
	} else {
		staticFS = http.Dir(publicDir)
	}
	fileServer := http.FileServer(staticFS)
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		switch strings.ToLower(filepath.Ext(r.URL.Path)) {
		case ".js", ".mjs":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		case ".svg":
			w.Header().Set("Content-Type", "image/svg+xml")
		}
		fileServer.ServeHTTP(w, r)
	}))

	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	return json.NewDecoder(r.Body).Decode(dst)
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
