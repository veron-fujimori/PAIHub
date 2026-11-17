package workspace

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"paihub/internal/sysenv"
)

const (
	maxPreviewFileBytes = 2 * 1024 * 1024
	maxBrowseEntries    = 1000
)

type Shortcut struct {
	Label      string `json:"label"`
	Path       string `json:"path"`
	IsSelfEdit bool   `json:"isSelfEdit,omitempty"`
}

type Entry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"`
}

type BrowseResult struct {
	Path       string     `json:"path"`
	Parent     string     `json:"parent"`
	IsGit      bool       `json:"isGit"`
	IsSelfEdit bool       `json:"isSelfEdit"`
	Shortcuts  []Shortcut `json:"shortcuts"`
	Entries    []Entry    `json:"entries"`
	Truncated  bool       `json:"truncated,omitempty"`
}

func Shortcuts(selfDir string) []Shortcut {
	var out []Shortcut
	if selfDir != "" {
		out = append(out, Shortcut{
			Label:      "Creator Mode (This App)",
			Path:       filepath.Clean(selfDir),
			IsSelfEdit: true,
		})
	}

	homeDir, err := os.UserHomeDir()
	if err != nil || homeDir == "" {
		homeDir = os.Getenv("USERPROFILE")
	}
	if homeDir != "" {
		candidates := []struct {
			label string
			rel   string
		}{
			{"Documents", "Documents"},
			{"Desktop", "Desktop"},
			{"User Home", ""},
		}
		for _, c := range candidates {
			full := homeDir
			if c.rel != "" {
				full = filepath.Join(homeDir, c.rel)
			}
			if info, err := os.Stat(full); err == nil && info.IsDir() {
				out = append(out, Shortcut{Label: c.label, Path: filepath.Clean(full)})
			}
		}
	}
	return out
}

func Browse(selfDir, targetPath string) (*BrowseResult, error) {
	if strings.TrimSpace(targetPath) == "" {
		targetPath = selfDir
	}
	cleaned := filepath.Clean(targetPath)
	info, err := os.Stat(cleaned)
	if err != nil {
		return nil, fmt.Errorf("failed to access folder: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", cleaned)
	}

	dirEntries, err := os.ReadDir(cleaned)
	if err != nil {
		return nil, fmt.Errorf("failed to read folder contents: %w", err)
	}

	entries := make([]Entry, 0, len(dirEntries))
	for _, de := range dirEntries {
		name := de.Name()
		if name == "." || name == ".." {
			continue
		}
		var size int64
		var modTime string
		if fi, err := de.Info(); err == nil {
			size = fi.Size()
			modTime = fi.ModTime().Format(time.RFC3339)
		}
		entries = append(entries, Entry{
			Name:    name,
			Path:    filepath.Join(cleaned, name),
			IsDir:   de.IsDir(),
			Size:    size,
			ModTime: modTime,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	truncated := false
	if len(entries) > maxBrowseEntries {
		entries = entries[:maxBrowseEntries]
		truncated = true
	}

	parent := filepath.Dir(cleaned)
	if parent == cleaned {
		parent = ""
	}

	_, gitErr := os.Stat(filepath.Join(cleaned, ".git"))

	return &BrowseResult{
		Path:       cleaned,
		Parent:     parent,
		IsGit:      gitErr == nil,
		IsSelfEdit: sysenv.SamePath(cleaned, selfDir),
		Shortcuts:  Shortcuts(selfDir),
		Entries:    entries,
		Truncated:  truncated,
	}, nil
}

func Mkdir(parentPath, name string) (string, error) {
	parentPath = strings.TrimSpace(parentPath)
	name = strings.TrimSpace(name)
	if parentPath == "" || name == "" {
		return "", errors.New("parent location and folder name cannot be empty")
	}
	if name == "." || name == ".." || strings.ContainsAny(name, `<>:"/\|?*`) {
		return "", errors.New("folder name contains invalid characters")
	}

	parentClean := filepath.Clean(parentPath)
	info, err := os.Stat(parentClean)
	if err != nil || !info.IsDir() {
		return "", errors.New("parent directory not found")
	}

	target := filepath.Join(parentClean, name)
	if err := os.MkdirAll(target, 0755); err != nil {
		return "", fmt.Errorf("failed to create folder: %w", err)
	}
	return target, nil
}

type FileContent struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Content   string `json:"content"`
	Size      int64  `json:"size"`
	IsBinary  bool   `json:"isBinary"`
	Truncated bool   `json:"truncated"`
}

func ReadFile(filePath string) (*FileContent, error) {
	cleaned := filepath.Clean(strings.TrimSpace(filePath))
	info, err := os.Stat(cleaned)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, errors.New("path is a directory, not a file")
	}

	f, err := os.Open(cleaned)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	limit := int64(maxPreviewFileBytes)
	buf := make([]byte, minInt64(info.Size(), limit))
	n, err := f.Read(buf)
	if err != nil && n == 0 && info.Size() > 0 {
		return nil, err
	}
	buf = buf[:n]

	isBinary := bytes.IndexByte(buf, 0) >= 0
	content := ""
	if !isBinary {
		content = string(buf)
	}

	return &FileContent{
		Path:      cleaned,
		Name:      filepath.Base(cleaned),
		Content:   content,
		Size:      info.Size(),
		IsBinary:  isBinary,
		Truncated: info.Size() > limit,
	}, nil
}

func WriteFile(filePath, content string) error {
	cleaned := filepath.Clean(strings.TrimSpace(filePath))
	if cleaned == "" || cleaned == "." {
		return errors.New("invalid file path")
	}
	if info, err := os.Stat(cleaned); err == nil && info.IsDir() {
		return errors.New("target is a directory")
	}
	if err := os.MkdirAll(filepath.Dir(cleaned), 0755); err != nil {
		return err
	}
	return os.WriteFile(cleaned, []byte(content), 0644)
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
