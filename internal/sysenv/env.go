package sysenv

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	pathOnce      sync.Once
	cmdCacheMu    sync.RWMutex
	cmdResolveTTL = 30 * time.Second
	cmdResolveMap = make(map[string]cachedResolution)
)

type cachedResolution struct {
	kind         string
	bin          string
	scriptPath   string
	resolvedPath string
	found        bool
	expiresAt    time.Time
}

func EnrichPath() {
	pathOnce.Do(func() {
		userProfile := os.Getenv("USERPROFILE")
		appData := os.Getenv("APPDATA")
		localAppData := os.Getenv("LOCALAPPDATA")

		candidates := []string{
			`C:\Program Files\Git\cmd`,
			`C:\Program Files\GitHub CLI`,
			`C:\Program Files\Go\bin`,
			`C:\Program Files\nodejs`,
			`C:\nvm4w\nodejs`,
		}
		if nvmSymlink := os.Getenv("NVM_SYMLINK"); nvmSymlink != "" {
			candidates = append(candidates, nvmSymlink)
		}
		if appData != "" {
			candidates = append(candidates, filepath.Join(appData, "npm"))
		}
		if localAppData != "" {
			candidates = append(candidates,
				filepath.Join(localAppData, "agy", "bin"),
				filepath.Join(localAppData, "Programs", "nodejs"),
				filepath.Join(localAppData, "Programs", "Ollama"),
			)
		}
		if userProfile != "" {
			candidates = append(candidates,
				filepath.Join(userProfile, ".npm-global"),
				filepath.Join(userProfile, ".cache", "codex-runtimes", "codex-primary-runtime", "dependencies", "node", "bin"),
			)
		}

		current := os.Getenv("PATH")
		parts := strings.Split(current, string(os.PathListSeparator))
		seen := make(map[string]bool, len(parts)+len(candidates))
		var finalParts []string

		addPart := func(p string) {
			p = strings.TrimSpace(p)
			if p == "" {
				return
			}
			key := strings.ToLower(filepath.Clean(p))
			if seen[key] {
				return
			}
			seen[key] = true
			finalParts = append(finalParts, p)
		}

		for _, p := range parts {
			addPart(p)
		}
		for _, c := range candidates {
			if info, err := os.Stat(c); err == nil && info.IsDir() {
				addPart(c)
			}
		}

		_ = os.Setenv("PATH", strings.Join(finalParts, string(os.PathListSeparator)))
	})
}

var npmShimPattern = regexp.MustCompile(`(?i)"%dp0%\\([^"]+\.(?:js|mjs|cjs))"`)

func ResolveCommand(command string, args []string) (bin string, finalArgs []string, resolvedPath string, found bool) {
	EnrichPath()
	command = strings.TrimSpace(command)
	if command == "" {
		return "", nil, "", false
	}

	cacheKey := strings.ToLower(command)
	now := time.Now()
	cmdCacheMu.RLock()
	cached, ok := cmdResolveMap[cacheKey]
	cmdCacheMu.RUnlock()
	if ok && now.Before(cached.expiresAt) {
		return applyCachedResolution(cached, command, args)
	}

	entry := computeResolution(command)
	entry.expiresAt = now.Add(cmdResolveTTL)
	cmdCacheMu.Lock()
	cmdResolveMap[cacheKey] = entry
	cmdCacheMu.Unlock()

	return applyCachedResolution(entry, command, args)
}

func computeResolution(command string) cachedResolution {
	resolved, err := exec.LookPath(command)
	if err != nil {
		return cachedResolution{kind: "missing", found: false}
	}

	ext := strings.ToLower(filepath.Ext(resolved))
	if ext == ".cmd" || ext == ".bat" {
		if nodeBin, scriptPath, ok := extractNpmNodeTarget(resolved); ok {
			return cachedResolution{
				kind:         "node",
				bin:          nodeBin,
				scriptPath:   scriptPath,
				resolvedPath: resolved,
				found:        true,
			}
		}
		return cachedResolution{
			kind:         "cmd",
			bin:          "cmd.exe",
			resolvedPath: resolved,
			found:        true,
		}
	}

	return cachedResolution{
		kind:         "direct",
		bin:          resolved,
		resolvedPath: resolved,
		found:        true,
	}
}

func applyCachedResolution(c cachedResolution, command string, args []string) (string, []string, string, bool) {
	if !c.found {
		return command, args, "", false
	}
	switch c.kind {
	case "node":
		outArgs := make([]string, 0, len(args)+1)
		outArgs = append(outArgs, c.scriptPath)
		outArgs = append(outArgs, args...)
		return c.bin, outArgs, c.resolvedPath, true
	case "cmd":
		outArgs := make([]string, 0, len(args)+2)
		outArgs = append(outArgs, "/c", c.resolvedPath)
		outArgs = append(outArgs, args...)
		return "cmd.exe", outArgs, c.resolvedPath, true
	default:
		return c.bin, args, c.resolvedPath, true
	}
}

func extractNpmNodeTarget(cmdPath string) (nodeBin string, scriptPath string, ok bool) {
	data, err := os.ReadFile(cmdPath)
	if err != nil {
		return "", "", false
	}
	matches := npmShimPattern.FindSubmatch(data)
	if len(matches) < 2 {
		return "", "", false
	}
	relScript := string(matches[1])
	candidateScript := filepath.Clean(filepath.Join(filepath.Dir(cmdPath), relScript))
	if _, err := os.Stat(candidateScript); err != nil {
		return "", "", false
	}

	localNode := filepath.Join(filepath.Dir(cmdPath), "node.exe")
	if _, err := os.Stat(localNode); err == nil {
		return localNode, candidateScript, true
	}
	if globalNode, err := exec.LookPath("node"); err == nil {
		return globalNode, candidateScript, true
	}
	return "", "", false
}

func LocalIPs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var ips []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			v4 := ipNet.IP.To4()
			if v4 == nil || v4.IsLoopback() || v4.IsLinkLocalUnicast() {
				continue
			}
			ips = append(ips, v4.String())
		}
	}
	return ips
}

func SamePath(a, b string) bool {
	if strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" {
		return false
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
