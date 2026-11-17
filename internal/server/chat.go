package server

import (
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"paihub/internal/agents"
	"paihub/internal/git"
	"paihub/internal/shell"
	"paihub/internal/sysenv"
)

func (s *Server) handleAgentsList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.agentMgr.List())
}

func (s *Server) handleAgentsCreate(w http.ResponseWriter, r *http.Request) {
	var cfg agents.AgentConfig
	if err := decodeJSON(w, r, &cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := s.agentMgr.Create(cfg)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleAgentsUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var cfg agents.AgentConfig
	if err := decodeJSON(w, r, &cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := s.agentMgr.Update(id, cfg)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleAgentsDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.agentMgr.Delete(id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AgentID      string `json:"agentId"`
		Prompt       string `json:"prompt"`
		Resume       bool   `json:"resume"`
		Workspace    string `json:"workspace"`
		Model        string `json:"model"`
		Effort       string `json:"effort"`
		SessionID    string `json:"sessionId"`
		Turbo        bool   `json:"turbo"`
		GhDirect     bool   `json:"ghDirect"`
		GhDirectRepo string `json:"ghDirectRepo"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	sse, ok := newSSEWriter(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	cfg := s.cfgStore.Get()
	agentID := strings.TrimSpace(body.AgentID)
	if agentID == "" {
		agentID = cfg.DefaultAgent
	}
	targetDir := cfg.ActiveWorkspace
	if strings.TrimSpace(body.Workspace) != "" {
		targetDir = filepath.Clean(body.Workspace)
	}
	isSelfEdit := sysenv.SamePath(targetDir, s.pluginsDir()) || sysenv.SamePath(targetDir, s.selfDir)

	sse.send("meta", map[string]any{
		"agentId":    agentID,
		"workspace":  targetDir,
		"isSelfEdit": isSelfEdit,
		"resume":     body.Resume,
		"model":      body.Model,
		"effort":     body.Effort,
	})

	prompt := body.Prompt
	if body.GhDirect {
		repoHint := ""
		if strings.TrimSpace(body.GhDirectRepo) != "" {
			repoHint = " Target repository: `" + strings.TrimSpace(body.GhDirectRepo) + "`."
		}
		prompt = "[GitHub Direct mode (gh) active:" + repoHint + " Use `gh` CLI commands (`gh api`, `gh repo`, `gh pr`, etc.) to read, edit, and commit directly to the GitHub repository without cloning locally, unless the user explicitly asks to clone.]\n\n" + prompt
	}

	exitCode, runErr := s.agentMgr.RunPromptStream(
		r.Context(),
		agentID,
		targetDir,
		prompt,
		body.Resume,
		isSelfEdit,
		body.Model,
		body.Effort,
		body.SessionID,
		body.Turbo,
		func(stream, text string) {
			sse.send("chunk", map[string]string{
				"stream": stream,
				"text":   text,
			})
		},
	)

	var errStr string
	if runErr != nil {
		errStr = runErr.Error()
	}

	gitSt := git.GetStatus(targetDir)
	autoPushed := false
	autoPushMsg := ""

	if runErr == nil && cfg.AutoPush && gitSt.IsGit && gitSt.HasChanges {
		sse.send("status", map[string]string{"message": "Running Auto-Commit & Push to GitHub..."})
		commitMsg := buildAutoCommitMessage(body.Prompt)
		pushOut, pushErr := git.CommitAndPush(targetDir, commitMsg)
		if pushErr != nil {
			autoPushMsg = "Auto-Push failed: " + pushErr.Error()
		} else {
			autoPushed = true
			autoPushMsg = pushOut
			gitSt = git.GetStatus(targetDir)
		}
	}

	sse.send("done", map[string]any{
		"exitCode":          exitCode,
		"error":             errStr,
		"isSelfEdit":        isSelfEdit,
		"reloadRecommended": isSelfEdit && runErr == nil,
		"autoPushed":        autoPushed,
		"autoPushMessage":   autoPushMsg,
		"git":               gitSt,
	})

	if isSelfEdit && runErr == nil {
		go func() {
			time.Sleep(350 * time.Millisecond)
			s.hub.broadcast("ui-reload", map[string]any{"reason": "self-edit-complete"})
		}()
	}
}

func buildAutoCommitMessage(prompt string) string {
	cleaned := strings.Join(strings.Fields(prompt), " ")
	if len(cleaned) > 64 {
		cleaned = cleaned[:61] + "..."
	}
	if cleaned == "" {
		return "ai: update code"
	}
	return "ai: " + cleaned
}

func (s *Server) handleChatStop(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"stopped": s.agentMgr.Stop()})
}

func (s *Server) handleTerminalTools(w http.ResponseWriter, r *http.Request) {
	list := s.agentMgr.List()
	cmds := make([]string, 0, len(list))
	for _, ag := range list {
		cmds = append(cmds, ag.Command)
	}
	writeJSON(w, http.StatusOK, shell.CheckTools(cmds))
}

func (s *Server) handleTerminalExec(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Command string `json:"command"`
		Dir     string `json:"dir"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sse, ok := newSSEWriter(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	dir := s.cfgStore.Get().ActiveWorkspace
	if strings.TrimSpace(body.Dir) != "" {
		dir = filepath.Clean(body.Dir)
	}

	code, err := s.termRunner.ExecStream(r.Context(), dir, body.Command, func(stream, text string) {
		sse.send("chunk", map[string]string{"stream": stream, "text": text})
	})
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	sse.send("done", map[string]any{"exitCode": code, "error": errMsg})
}

func (s *Server) handleTerminalStop(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"stopped": s.termRunner.Stop()})
}

func (s *Server) handleTerminalLoginWindow(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Target string `json:"target"`
		Model  string `json:"model"`
		Effort string `json:"effort"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	target := strings.TrimSpace(body.Target)
	model := strings.TrimSpace(body.Model)
	effort := strings.TrimSpace(body.Effort)
	dir := s.cfgStore.Get().ActiveWorkspace

	var command string
	var args []string

	if strings.EqualFold(target, "gh") {
		command = "gh"
		args = []string{"auth", "login"}
	} else {
		ag, ok := s.agentMgr.Get(target)
		if !ok {
			writeError(w, http.StatusBadRequest, "unknown CLI login target")
			return
		}
		command = ag.Command
		args = append([]string(nil), ag.InteractiveArgs...)
		switch strings.ToLower(ag.ID) {
		case "ollama":
			if model != "" {
				args = []string{"run", model}
			}
		case "codex":
			if model != "" {
				args = append(args, "--model", model)
			}
			if effort != "" {
				args = append(args, "-c", "model_reasoning_effort="+effort)
			}
		case "claude":
			if model != "" {
				args = append(args, "--model", model)
			}
			if effort != "" {
				args = append(args, "--effort", effort)
			}
		default:
			if model != "" {
				args = append(args, "--model", model)
			}
		}
	}

	if err := shell.LaunchInteractiveWindow(command, args, dir); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"launched": true, "command": command})
}
