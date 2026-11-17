package shell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"paihub/internal/sysenv"
)

type ToolStatus struct {
	Name         string `json:"name"`
	Installed    bool   `json:"installed"`
	ResolvedPath string `json:"resolvedPath,omitempty"`
}

type Runner struct {
	mu           sync.Mutex
	activeCmd    *exec.Cmd
	activeCancel context.CancelFunc
}

func NewRunner() *Runner {
	return &Runner{}
}

func CheckTools(extraCommands []string) []ToolStatus {
	sysenv.EnrichPath()
	seen := make(map[string]bool)
	var list []string
	for _, base := range []string{"git", "gh", "go", "node", "npm"} {
		seen[strings.ToLower(base)] = true
		list = append(list, base)
	}
	for _, cmd := range extraCommands {
		k := strings.ToLower(strings.TrimSpace(cmd))
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		list = append(list, strings.TrimSpace(cmd))
	}

	out := make([]ToolStatus, 0, len(list))
	for _, item := range list {
		_, _, resolved, found := sysenv.ResolveCommand(item, nil)
		out = append(out, ToolStatus{
			Name:         item,
			Installed:    found,
			ResolvedPath: resolved,
		})
	}
	return out
}

func (r *Runner) ExecStream(ctx context.Context, dir, commandLine string, onChunk func(stream, text string)) (int, error) {
	commandLine = strings.TrimSpace(commandLine)
	if commandLine == "" {
		return -1, errors.New("terminal command cannot be empty")
	}
	sysenv.EnrichPath()

	r.mu.Lock()
	if r.activeCmd != nil {
		r.mu.Unlock()
		return -1, errors.New("another terminal command is currently running; click Stop first")
	}
	r.mu.Unlock()

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(runCtx, "powershell.exe", "-NoProfile", "-Command", commandLine)
	} else {
		cmd = exec.CommandContext(runCtx, "sh", "-c", commandLine)
	}
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = os.Environ()

	defer func() {
		r.mu.Lock()
		r.activeCmd = nil
		r.activeCancel = nil
		r.mu.Unlock()
	}()

	return sysenv.StreamCommand(
		runCtx,
		cmd,
		func(started *exec.Cmd) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			if r.activeCmd != nil {
				return errors.New("another terminal command is currently running; click Stop first")
			}
			r.activeCmd = started
			r.activeCancel = cancel
			return nil
		},
		nil,
		onChunk,
	)
}

func (r *Runner) Stop() bool {
	r.mu.Lock()
	cmd := r.activeCmd
	cancel := r.activeCancel
	r.mu.Unlock()

	if cmd == nil {
		return false
	}
	if cancel != nil {
		cancel()
	}
	sysenv.KillProcessTree(cmd)
	return true
}

func LaunchInteractiveWindow(command string, args []string, dir string) error {
	if runtime.GOOS != "windows" {
		return errors.New("opening an interactive window is only supported on a Windows PC host")
	}
	sysenv.EnrichPath()
	_, _, resolved, found := sysenv.ResolveCommand(command, args)
	if !found {
		return fmt.Errorf("CLI '%s' is not installed on this PC", command)
	}

	psCmd := "& '" + strings.ReplaceAll(resolved, "'", "''") + "'"
	for _, a := range args {
		if a != "" {
			psCmd += " '" + strings.ReplaceAll(a, "'", "''") + "'"
		}
	}

	startArgs := []string{"/c", "start", "PAIHub CLI"}
	if dir != "" {
		startArgs = append(startArgs, "/D", dir)
	}
	startArgs = append(startArgs, "powershell.exe", "-NoExit", "-Command", psCmd)
	return exec.Command("cmd.exe", startArgs...).Start()
}
