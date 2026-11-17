package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"paihub/internal/sysenv"
)

type ModelOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type AgentConfig struct {
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	Command         string        `json:"command"`
	TaskArgs        []string      `json:"taskArgs"`
	ResumeArgs      []string      `json:"resumeArgs"`
	InteractiveArgs []string      `json:"interactiveArgs"`
	ModelFlag       string        `json:"modelFlag,omitempty"`
	EffortFlag      string        `json:"effortFlag,omitempty"`
	SessionFlag     string        `json:"sessionFlag,omitempty"`
	Models          []ModelOption `json:"models,omitempty"`
	Efforts         []string      `json:"efforts,omitempty"`
	Installed       bool          `json:"installed"`
	ResolvedPath    string        `json:"resolvedPath,omitempty"`
}

type persistedAgent struct {
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	Command         string        `json:"command"`
	TaskArgs        []string      `json:"taskArgs"`
	ResumeArgs      []string      `json:"resumeArgs"`
	InteractiveArgs []string      `json:"interactiveArgs"`
	ModelFlag       string        `json:"modelFlag,omitempty"`
	EffortFlag      string        `json:"effortFlag,omitempty"`
	SessionFlag     string        `json:"sessionFlag,omitempty"`
	Models          []ModelOption `json:"models,omitempty"`
	Efforts         []string      `json:"efforts,omitempty"`
}

type Manager struct {
	mu     sync.RWMutex
	path   string
	agents []persistedAgent

	procMu       sync.Mutex
	activeCmd    *exec.Cmd
	activeCancel context.CancelFunc
	activeAgent  string
}

var (
	idSanitizer  = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,40}$`)
	ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
)

func NewManager(agentsPath string) (*Manager, error) {
	m := &Manager{path: agentsPath}
	if err := m.load(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			m.agents = defaultAgents()
			return m.saveLocked()
		}
		return err
	}

	var list []persistedAgent
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("failed to parse agents.json: %w", err)
	}
	m.agents = normalizeSlice(list)
	return nil
}

func defaultAgents() []persistedAgent {
	return []persistedAgent{
		{
			ID:              "agy",
			Name:            "Antigravity",
			Command:         "agy",
			TaskArgs:        []string{"-p", "{{prompt}}", "--dangerously-skip-permissions"},
			ResumeArgs:      []string{"-c", "-p", "{{prompt}}", "--dangerously-skip-permissions"},
			InteractiveArgs: []string{},
			ModelFlag:       "--model",
			EffortFlag:      "",
			SessionFlag:     "--conversation",
			Models: []ModelOption{
				{ID: "claude-opus-5-5-low", Label: "Claude Opus 5.5 (Low)"},
				{ID: "claude-opus-5-5-medium", Label: "Claude Opus 5.5 (Medium)"},
				{ID: "claude-opus-5-5-high", Label: "Claude Opus 5.5 (High)"},
				{ID: "claude-sonnet-5-5-low", Label: "Claude Sonnet 5.5 (Low)"},
				{ID: "claude-sonnet-5-5-medium", Label: "Claude Sonnet 5.5 (Medium)"},
				{ID: "claude-sonnet-5-5-high", Label: "Claude Sonnet 5.5 (High)"},
				{ID: "gemini-4-argon-low", Label: "Gemini 4 Argon (Low)"},
				{ID: "gemini-4-argon-medium", Label: "Gemini 4 Argon (Medium)"},
				{ID: "gemini-4-argon-high", Label: "Gemini 4 Argon (High)"},
				{ID: "gemini-4-hydrogen-low", Label: "Gemini 4 Hydrogen (Low)"},
				{ID: "gemini-4-hydrogen-medium", Label: "Gemini 4 Hydrogen (Medium)"},
				{ID: "gemini-4-hydrogen-high", Label: "Gemini 4 Hydrogen (High)"},
				{ID: "gemini-4-hydrogen-xhigh", Label: "Gemini 4 Hydrogen (XHigh)"},
				{ID: "gemini-3.8-flash-low", Label: "Gemini 3.8 Flash (Low)"},
				{ID: "gemini-3.8-flash-medium", Label: "Gemini 3.8 Flash (Medium)"},
				{ID: "gemini-3.8-flash-high", Label: "Gemini 3.8 Flash (High)"},
				{ID: "gpt-oss-120b-medium", Label: "GPT-OSS 120B (Medium)"},
				{ID: "gemini-pro-orchestrator-g1-pro", Label: "Orchestrator (Argon)"},
			},
			Efforts: []string{},
		},
		{
			ID:              "claude",
			Name:            "Claude Code",
			Command:         "claude",
			TaskArgs:        []string{"-p", "{{prompt}}", "--dangerously-skip-permissions"},
			ResumeArgs:      []string{"-c", "-p", "{{prompt}}", "--dangerously-skip-permissions"},
			InteractiveArgs: []string{},
			ModelFlag:       "--model",
			EffortFlag:      "--effort",
			SessionFlag:     "--resume",
			Models: []ModelOption{
				{ID: "opus", Label: "Claude Opus"},
				{ID: "sonnet", Label: "Claude Sonnet"},
			},
			Efforts: []string{"low", "medium", "high", "xhigh", "max"},
		},
		{
			ID:              "codex",
			Name:            "ChatGPT Codex",
			Command:         "codex",
			TaskArgs:        []string{"exec", "--full-auto", "{{prompt}}"},
			ResumeArgs:      []string{"exec", "resume", "--last", "--full-auto", "{{prompt}}"},
			InteractiveArgs: []string{},
			ModelFlag:       "--model",
			EffortFlag:      "",
			SessionFlag:     "--conversation",
			Models: []ModelOption{
				{ID: "gpt-5-codex", Label: "GPT-5 Codex"},
				{ID: "o3", Label: "OpenAI o3"},
			},
			Efforts: []string{},
		},
		{
			ID:              "ollama",
			Name:            "Ollama (Local)",
			Command:         "ollama",
			TaskArgs:        []string{"{{prompt}}"},
			ResumeArgs:      []string{"{{prompt}}"},
			InteractiveArgs: []string{},
			ModelFlag:       "run",
			EffortFlag:      "",
			Models:          []ModelOption{},
			Efforts:         []string{},
		},
	}
}

func normalizeSlice(in []persistedAgent) []persistedAgent {
	for i := range in {
		if in[i].TaskArgs == nil {
			in[i].TaskArgs = []string{}
		}
		if in[i].ResumeArgs == nil {
			in[i].ResumeArgs = []string{}
		}
		if in[i].InteractiveArgs == nil {
			in[i].InteractiveArgs = []string{}
		}
		if in[i].Models == nil {
			in[i].Models = []ModelOption{}
		}
		if in[i].Efforts == nil {
			in[i].Efforts = []string{}
		}
	}
	return in
}

func (m *Manager) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(m.path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m.agents, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.path, append(data, '\n'), 0644)
}

func toAgentConfig(p persistedAgent) AgentConfig {
	_, _, resolvedPath, found := sysenv.ResolveCommand(p.Command, nil)
	models := append([]ModelOption(nil), p.Models...)
	if strings.EqualFold(p.ID, "ollama") && found {
		if localModels := fetchOllamaLocalModels(); len(localModels) > 0 {
			models = localModels
		}
	}
	return AgentConfig{
		ID:              p.ID,
		Name:            p.Name,
		Command:         p.Command,
		TaskArgs:        append([]string(nil), p.TaskArgs...),
		ResumeArgs:      append([]string(nil), p.ResumeArgs...),
		InteractiveArgs: append([]string(nil), p.InteractiveArgs...),
		ModelFlag:       p.ModelFlag,
		EffortFlag:      p.EffortFlag,
		SessionFlag:     p.SessionFlag,
		Models:          models,
		Efforts:         append([]string(nil), p.Efforts...),
		Installed:       found,
		ResolvedPath:    resolvedPath,
	}
}

func fetchOllamaLocalModels() []ModelOption {
	client := &http.Client{Timeout: 300 * time.Millisecond}
	resp, err := client.Get("http://127.0.0.1:11434/api/tags")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var payload struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil
	}
	out := make([]ModelOption, 0, len(payload.Models))
	for _, m := range payload.Models {
		name := strings.TrimSpace(m.Name)
		if name != "" {
			out = append(out, ModelOption{ID: name, Label: name})
		}
	}
	return out
}

func (m *Manager) List() []AgentConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]AgentConfig, 0, len(m.agents))
	for _, a := range m.agents {
		out = append(out, toAgentConfig(a))
	}
	return out
}

func (m *Manager) Get(id string) (AgentConfig, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, a := range m.agents {
		if strings.EqualFold(a.ID, id) {
			return toAgentConfig(a), true
		}
	}
	return AgentConfig{}, false
}

func (m *Manager) Create(cfg AgentConfig) (AgentConfig, error) {
	p, err := validatePersisted(cfg)
	if err != nil {
		return AgentConfig{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, existing := range m.agents {
		if strings.EqualFold(existing.ID, p.ID) {
			return AgentConfig{}, fmt.Errorf("agent with ID '%s' already exists", p.ID)
		}
	}

	m.agents = append(m.agents, p)
	if err := m.saveLocked(); err != nil {
		return AgentConfig{}, err
	}
	return toAgentConfig(p), nil
}

func (m *Manager) Update(id string, cfg AgentConfig) (AgentConfig, error) {
	if strings.TrimSpace(cfg.ID) == "" {
		cfg.ID = id
	}
	p, err := validatePersisted(cfg)
	if err != nil {
		return AgentConfig{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for i, existing := range m.agents {
		if strings.EqualFold(existing.ID, id) {
			p.ID = existing.ID
			if p.ModelFlag == "" && existing.ModelFlag != "" {
				p.ModelFlag = existing.ModelFlag
			}
			if p.EffortFlag == "" && existing.EffortFlag != "" {
				p.EffortFlag = existing.EffortFlag
			}
			if len(p.Models) == 0 && len(existing.Models) > 0 {
				p.Models = existing.Models
			}
			if len(p.Efforts) == 0 && len(existing.Efforts) > 0 {
				p.Efforts = existing.Efforts
			}
			m.agents[i] = p
			if err := m.saveLocked(); err != nil {
				return AgentConfig{}, err
			}
			return toAgentConfig(p), nil
		}
	}
	return AgentConfig{}, fmt.Errorf("agent '%s' not found", id)
}

func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, existing := range m.agents {
		if strings.EqualFold(existing.ID, id) {
			m.agents = append(m.agents[:i], m.agents[i+1:]...)
			return m.saveLocked()
		}
	}
	return fmt.Errorf("agent '%s' not found", id)
}

func validatePersisted(cfg AgentConfig) (persistedAgent, error) {
	id := strings.TrimSpace(strings.ToLower(cfg.ID))
	name := strings.TrimSpace(cfg.Name)
	cmd := strings.TrimSpace(cfg.Command)
	if !idSanitizer.MatchString(id) {
		return persistedAgent{}, errors.New("ID must contain only letters, numbers, hyphens, or underscores (1-40 characters)")
	}
	if name == "" || cmd == "" {
		return persistedAgent{}, errors.New("name and CLI command are required")
	}
	p := persistedAgent{
		ID:              id,
		Name:            name,
		Command:         cmd,
		TaskArgs:        nonNilSlice(cfg.TaskArgs),
		ResumeArgs:      nonNilSlice(cfg.ResumeArgs),
		InteractiveArgs: nonNilSlice(cfg.InteractiveArgs),
		ModelFlag:       strings.TrimSpace(cfg.ModelFlag),
		EffortFlag:      strings.TrimSpace(cfg.EffortFlag),
		SessionFlag:     strings.TrimSpace(cfg.SessionFlag),
		Models:          append([]ModelOption(nil), cfg.Models...),
		Efforts:         append([]string(nil), cfg.Efforts...),
	}
	if p.Models == nil {
		p.Models = []ModelOption{}
	}
	if p.Efforts == nil {
		p.Efforts = []string{}
	}
	return p, nil
}

func nonNilSlice(in []string) []string {
	if in == nil {
		return []string{}
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (m *Manager) IsRunning() (bool, string) {
	m.procMu.Lock()
	defer m.procMu.Unlock()
	return m.activeCmd != nil, m.activeAgent
}

func (m *Manager) Stop() bool {
	m.procMu.Lock()
	cmd := m.activeCmd
	cancel := m.activeCancel
	m.procMu.Unlock()

	if cmd == nil {
		return false
	}
	if cancel != nil {
		cancel()
	}
	sysenv.KillProcessTree(cmd)
	return true
}

const selfEditInstruction = `[CREATOR MODE: You are in Creator Mode inside PAIHub's data/plugins/ directory. Create or edit a new plugin inside a <id>/plugin.json subfolder (containing id, name, description, command, args, enabled: true) along with its supporting scripts without modifying paihub.exe. Please complete the following user request:]`

func isRawShellCommand(command string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(command)))
	switch base {
	case "powershell", "powershell.exe", "pwsh", "pwsh.exe", "cmd", "cmd.exe", "bash", "sh":
		return true
	default:
		return false
	}
}

func BuildFinalPrompt(prompt string, isSelfEdit bool) string {
	prompt = strings.TrimSpace(prompt)
	if !isSelfEdit || prompt == "" {
		return prompt
	}
	return selfEditInstruction + "\n\n" + prompt
}

func BuildArgs(agent AgentConfig, prompt string, resume bool, model, effort, sessionID string) []string {
	sid := strings.TrimSpace(sessionID)
	useSessionFlag := resume && sid != "" && strings.TrimSpace(agent.SessionFlag) != ""

	template := agent.TaskArgs
	if resume && !useSessionFlag && len(agent.ResumeArgs) > 0 {
		template = agent.ResumeArgs
	}

	args := make([]string, 0, len(template)+7)
	if useSessionFlag {
		args = append(args, strings.TrimSpace(agent.SessionFlag), sid)
	}
	if m := strings.TrimSpace(model); m != "" && strings.TrimSpace(agent.ModelFlag) != "" {
		args = append(args, strings.TrimSpace(agent.ModelFlag), m)
	}
	if e := strings.TrimSpace(effort); e != "" && strings.TrimSpace(agent.EffortFlag) != "" {
		args = append(args, strings.TrimSpace(agent.EffortFlag), e)
	}

	replaced := false
	for _, t := range template {
		if strings.Contains(t, "{{prompt}}") {
			args = append(args, strings.ReplaceAll(t, "{{prompt}}", prompt))
			replaced = true
		} else {
			args = append(args, t)
		}
	}
	if !replaced && prompt != "" {
		args = append(args, prompt)
	}
	return args
}

func (m *Manager) RunPromptStream(
	ctx context.Context,
	agentID, dir, prompt string,
	resume, isSelfEdit bool,
	model, effort, sessionID string,
	turbo bool,
	onChunk func(stream, text string),
) (int, error) {
	if strings.TrimSpace(prompt) == "" {
		return -1, errors.New("prompt cannot be empty")
	}
	agent, ok := m.Get(agentID)
	if !ok {
		return -1, fmt.Errorf("agent '%s' not found", agentID)
	}

	injectSelfEdit := isSelfEdit && !isRawShellCommand(agent.Command)
	finalPrompt := BuildFinalPrompt(prompt, injectSelfEdit)
	rawArgs := BuildArgs(agent, finalPrompt, resume, model, effort, sessionID)
	bin, finalArgs, _, found := sysenv.ResolveCommand(agent.Command, rawArgs)
	if !found {
		return -1, fmt.Errorf("CLI '%s' (%s) is not installed on this PC. Run scripts/install-clis.ps1 or check the Terminal tab", agent.Name, agent.Command)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if turbo {
		switch strings.ToLower(agent.ID) {
		case "agy":
			finalArgs = append(finalArgs, "--yolo")
		case "claude":
			finalArgs = append(finalArgs, "--dangerously-skip-permissions")
		case "codex":
			finalArgs = append(finalArgs, "--full-auto")
		}
	}

	m.procMu.Lock()
	if m.activeCmd != nil {
		m.procMu.Unlock()
		return -1, errors.New("an AI process is already running; click Stop first")
	}
	m.procMu.Unlock()

	cmd := exec.CommandContext(runCtx, bin, finalArgs...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = os.Environ()

	defer func() {
		m.procMu.Lock()
		m.activeCmd = nil
		m.activeCancel = nil
		m.activeAgent = ""
		m.procMu.Unlock()
	}()

	return sysenv.StreamCommand(
		runCtx,
		cmd,
		func(started *exec.Cmd) error {
			m.procMu.Lock()
			defer m.procMu.Unlock()
			if m.activeCmd != nil {
				return errors.New("an AI process is already running; click Stop first")
			}
			m.activeCmd = started
			m.activeCancel = cancel
			m.activeAgent = agent.ID
			return nil
		},
		func(text string) string {
			return ansiEscapeRe.ReplaceAllString(text, "")
		},
		onChunk,
	)
}
