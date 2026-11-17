package server

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"paihub/internal/config"
	"paihub/internal/git"
	"paihub/internal/sysenv"
	"paihub/internal/workspace"
)

func (s *Server) pluginsDir() string {
	return filepath.Join(s.selfDir, "data", "plugins")
}

func (s *Server) workspaceSummary() map[string]any {
	cfg := s.cfgStore.Get()
	st := git.GetStatus(cfg.ActiveWorkspace)
	return map[string]any{
		"activeWorkspace":  cfg.ActiveWorkspace,
		"recentWorkspaces": cfg.RecentWorkspaces,
		"autoPush":         cfg.AutoPush,
		"defaultAgent":     cfg.DefaultAgent,
		"defaultModel":     cfg.DefaultModel,
		"defaultEffort":    cfg.DefaultEffort,
		"selfDir":          s.selfDir,
		"pluginsDir":       s.pluginsDir(),
		"isSelfEdit":       sysenv.SamePath(cfg.ActiveWorkspace, s.pluginsDir()) || sysenv.SamePath(cfg.ActiveWorkspace, s.selfDir),
		"isGit":            st.IsGit,
		"shortcuts":        workspace.Shortcuts(s.selfDir),
		"git":              st,
	}
}

func (s *Server) handleWorkspacesGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.workspaceSummary())
}

func (s *Server) handleWorkspacesBrowse(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if strings.TrimSpace(path) == "" {
		path = s.cfgStore.Get().ActiveWorkspace
	}
	res, err := workspace.Browse(s.selfDir, path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleWorkspacesMkdir(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Parent string `json:"parent"`
		Name   string `json:"name"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := workspace.Mkdir(body.Parent, body.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": created})
}

func (s *Server) handleWorkspacesSelect(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	target := filepath.Clean(strings.TrimSpace(body.Path))
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		writeError(w, http.StatusBadRequest, "folder not found")
		return
	}
	if err := s.cfgStore.Update(func(c *config.Config) {
		c.ActiveWorkspace = target
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	summary := s.workspaceSummary()
	s.hub.broadcast("workspace-changed", summary)
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) handleFileRead(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	res, err := workspace.ReadFile(p)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleFileWrite(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := workspace.WriteFile(body.Path, body.Content); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	publicRoot := filepath.Clean(filepath.Join(s.selfDir, "public"))
	cleanTarget := filepath.Clean(body.Path)
	if strings.HasPrefix(strings.ToLower(cleanTarget), strings.ToLower(publicRoot)) {
		s.hub.broadcast("ui-reload", map[string]any{"reason": "file-saved"})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleWorkspacesPickNative(w http.ResponseWriter, r *http.Request) {
	if runtime.GOOS != "windows" {
		writeError(w, http.StatusBadRequest, "Native folder picker is only supported on Windows")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	psScript := `Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.Application]::EnableVisualStyles(); $d = New-Object System.Windows.Forms.OpenFileDialog; $d.Title = 'Select PAIHub Workspace Folder'; $d.ValidateNames = $false; $d.CheckFileExists = $false; $d.CheckPathExists = $true; $d.FileName = 'Select This Folder'; if ($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { Write-Output (Split-Path -Parent $d.FileName) }`
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-STA", "-Command", psScript)
	out, err := cmd.Output()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to open File Explorer: "+err.Error())
		return
	}
	picked := strings.TrimSpace(string(out))
	if picked == "" {
		writeJSON(w, http.StatusOK, map[string]any{"cancelled": true})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": picked})
}

func (s *Server) handleGitHubRepos(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, git.ListGitHubRepos())
}

func (s *Server) handleGitHubClone(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Repo       string `json:"repo"`
		DestParent string `json:"destParent"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	dest := strings.TrimSpace(body.DestParent)
	if dest == "" {
		dest = filepath.Dir(s.selfDir)
	}

	clonedDir, err := git.CloneRepo(body.Repo, dest)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.cfgStore.Update(func(c *config.Config) {
		c.ActiveWorkspace = clonedDir
	})
	summary := s.workspaceSummary()
	s.hub.broadcast("workspace-changed", summary)
	writeJSON(w, http.StatusOK, summary)
}
