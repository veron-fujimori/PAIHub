package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paihub/internal/agents"
	"paihub/internal/config"
	"paihub/internal/git"
	"paihub/internal/shell"
)

func setupTestApp(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()

	pubDir := filepath.Join(dir, "public")
	if err := os.MkdirAll(pubDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pubDir, "index.html"), []byte("<!doctype html><html><body>ok</body></html>"), 0644); err != nil {
		t.Fatal(err)
	}

	cfgStore, err := config.LoadStore(filepath.Join(dir, "config.json"), dir)
	if err != nil {
		t.Fatalf("LoadStore failed: %v", err)
	}
	agentMgr, err := agents.NewManager(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	a := &Server{
		selfDir:      dir,
		cfgStore:     cfgStore,
		agentMgr:     agentMgr,
		termRunner:   shell.NewRunner(),
		hub:          newEventHub(),
		authSecret:   []byte("test-secret-key-32-bytes-long!!!"),
		loginLimiter: newLoginLimiter(),
	}
	return a, dir
}

func TestHealthAndStatsEndpoints(t *testing.T) {
	a, _ := setupTestApp(t)
	h := a.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on /api/health, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/system/stats", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on /api/system/stats, got %d", rec.Code)
	}
	var stats map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("invalid JSON stats: %v", err)
	}
	if _, ok := stats["allocMB"]; !ok {
		t.Fatalf("expected allocMB in stats, got %v", stats)
	}
}

func TestAgentsCRUD(t *testing.T) {
	a, _ := setupTestApp(t)
	h := a.Routes()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/agents", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/agents failed: %d", rec.Code)
	}
	var initial []agents.AgentConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &initial); err != nil {
		t.Fatal(err)
	}
	if len(initial) < 3 {
		t.Fatalf("expected at least 3 default agents, got %d", len(initial))
	}
	if initial[0].ID != "agy" {
		t.Fatalf("expected first default agent to be agy, got %s", initial[0].ID)
	}

	createBody := `{"id":"opencode","name":"OpenCode Test","command":"opencode","taskArgs":["-p","{{prompt}}"]}`
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/agents", strings.NewReader(createBody)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/agents failed: %d %s", rec.Code, rec.Body.String())
	}

	updateBody := `{"id":"opencode","name":"OpenCode Updated","command":"opencode","taskArgs":["run","{{prompt}}"]}`
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/agents/opencode", strings.NewReader(updateBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/agents/opencode failed: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/agents/opencode", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/agents/opencode failed: %d %s", rec.Code, rec.Body.String())
	}
}

func TestWorkspaceBrowseMkdirAndFiles(t *testing.T) {
	a, dir := setupTestApp(t)
	h := a.Routes()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workspaces/browse", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("browse failed: %d %s", rec.Code, rec.Body.String())
	}

	mkBody, _ := json.Marshal(map[string]string{"parent": dir, "name": "new-project"})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/workspaces/mkdir", bytes.NewReader(mkBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("mkdir failed: %d %s", rec.Code, rec.Body.String())
	}
	subDir := filepath.Join(dir, "new-project")
	if _, err := os.Stat(subDir); err != nil {
		t.Fatalf("created subdir missing: %v", err)
	}

	selBody, _ := json.Marshal(map[string]string{"path": subDir})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/workspaces/select", bytes.NewReader(selBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("select failed: %d %s", rec.Code, rec.Body.String())
	}

	targetFile := filepath.Join(subDir, "hello.txt")
	writeBody, _ := json.Marshal(map[string]string{"path": targetFile, "content": "Hello Workspace"})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/workspaces/file", bytes.NewReader(writeBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("file write failed: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workspaces/file?path="+targetFile, nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Hello Workspace") {
		t.Fatalf("file read unexpected: %d %s", rec.Code, rec.Body.String())
	}
}

func TestChatStreamWithPowerShell(t *testing.T) {
	a, dir := setupTestApp(t)
	h := a.Routes()

	_, err := a.agentMgr.Create(agents.AgentConfig{
		ID:       "powershell",
		Name:     "PowerShell Test",
		Command:  "powershell.exe",
		TaskArgs: []string{"-NoProfile", "-Command", "{{prompt}}"},
	})
	if err != nil {
		t.Fatalf("failed to create test powershell agent: %v", err)
	}

	payload, _ := json.Marshal(map[string]any{
		"agentId":   "powershell",
		"prompt":    "Write-Output 'SSE_STREAM_OK'",
		"resume":    false,
		"workspace": dir,
		"model":     "custom-model",
		"effort":    "high",
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/chat/stream", bytes.NewReader(payload)))
	if rec.Code != http.StatusOK {
		t.Fatalf("chat stream failed: %d %s", rec.Code, rec.Body.String())
	}
	out := rec.Body.String()
	if !strings.Contains(out, "event: meta") || !strings.Contains(out, "SSE_STREAM_OK") || !strings.Contains(out, "event: done") {
		t.Fatalf("unexpected SSE output: %s", out)
	}
	if !strings.Contains(out, `"exitCode":0`) {
		t.Fatalf("expected exitCode 0, got: %s", out)
	}
}

func TestDefaultsAndModelEffortArgs(t *testing.T) {
	a, _ := setupTestApp(t)
	h := a.Routes()

	if list := a.agentMgr.List(); len(list) == 0 {
		t.Fatal("expected seeded default agents")
	}

	body := `{"defaultAgent":"claude","defaultModel":"opus","defaultEffort":"high"}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/config/defaults", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/config/defaults failed: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/config/defaults", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"defaultAgent":"claude"`) || !strings.Contains(rec.Body.String(), `"defaultEffort":"high"`) {
		t.Fatalf("unexpected defaults response: %d %s", rec.Code, rec.Body.String())
	}

	claude, ok := a.agentMgr.Get("claude")
	if !ok {
		t.Fatal("expected claude agent")
	}
	args := agents.BuildArgs(claude, "hello", true, "opus", "xhigh", "")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--model opus") || !strings.Contains(joined, "--effort xhigh") || !strings.Contains(joined, "-c") {
		t.Fatalf("unexpected BuildArgs output: %v", args)
	}
	argsByID := agents.BuildArgs(claude, "hello", true, "opus", "high", "sess-123")
	joinedByID := strings.Join(argsByID, " ")
	if !strings.Contains(joinedByID, "--resume sess-123") || strings.Contains(joinedByID, "-c") {
		t.Fatalf("expected --resume sess-123 without -c, got: %v", argsByID)
	}
}

func TestSelfEditPromptBuilder(t *testing.T) {
	plain := agents.BuildFinalPrompt("change header color", false)
	if plain != "change header color" {
		t.Fatalf("unexpected non-self-edit prompt: %s", plain)
	}
	selfEdit := agents.BuildFinalPrompt("change header color", true)
	if !strings.Contains(selfEdit, "[CREATOR MODE:") || !strings.Contains(selfEdit, "change header color") {
		t.Fatalf("expected self-edit context injected, got: %s", selfEdit)
	}
}

func TestParsePaginatedUserReposIncludesOrgs(t *testing.T) {
	raw := `[{"full_name":"user/personal-app","private":true,"html_url":"https://github.com/user/personal-app"}]
[{"full_name":"my-org/backend-api","private":true,"html_url":"https://github.com/my-org/backend-api"},{"full_name":"user/personal-app","private":true,"html_url":"https://github.com/user/personal-app"}]`
	repos, err := git.ParsePaginatedUserRepos(raw)
	if err != nil {
		t.Fatalf("ParsePaginatedUserRepos failed: %v", err)
	}
	if len(repos) != 2 || repos[0].NameWithOwner != "user/personal-app" || repos[1].NameWithOwner != "my-org/backend-api" {
		t.Fatalf("unexpected parsed repos: %+v", repos)
	}
}

func TestPinAuthenticationFlow(t *testing.T) {
	a, _ := setupTestApp(t)
	h := a.Routes()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/auth/pin", strings.NewReader(`{"pin":"1234"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("set pin failed: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/agents", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when PIN active without cookie, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"pin":"0000"}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on wrong PIN, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"pin":"1234"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on valid PIN, got %d", rec.Code)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie after login")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	req.AddCookie(cookies[0])
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with session cookie, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/auth/pin", strings.NewReader(`{"pin":""}`))
	req.AddCookie(cookies[0])
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"pinRequired":false`) {
		t.Fatalf("expected PIN disabled response, got %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/agents", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 after PIN disabled, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/auth/pin", strings.NewReader(`{"pin":"1234"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("re-enable pin failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("expected X-Content-Type-Options=nosniff, got %q", rec.Header().Get("X-Content-Type-Options"))
	}
	if rec.Header().Get("X-Frame-Options") != "SAMEORIGIN" {
		t.Fatalf("expected X-Frame-Options=SAMEORIGIN, got %q", rec.Header().Get("X-Frame-Options"))
	}

	jsDir := filepath.Join(a.selfDir, "public", "js")
	if err := os.MkdirAll(jsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jsDir, "main.js"), []byte("console.log('ok');"), 0644); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/js/main.js", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("expected javascript Content-Type on /js/main.js, got %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}

	for i := 0; i < maxLoginFailures; i++ {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"pin":"9999"}`)))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 on attempt %d, got %d", i+1, rec.Code)
		}
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"pin":"1234"}`)))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after %d failed PIN attempts, got %d", maxLoginFailures, rec.Code)
	}
}
