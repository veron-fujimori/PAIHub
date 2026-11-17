package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const maxRecentWorkspaces = 12

type Config struct {
	Port             int      `json:"port"`
	Pin              string   `json:"pin"`
	ActiveWorkspace  string   `json:"activeWorkspace"`
	RecentWorkspaces []string `json:"recentWorkspaces"`
	AutoPush         bool     `json:"autoPush"`
	DefaultAgent     string   `json:"defaultAgent"`
	DefaultModel     string   `json:"defaultModel"`
	DefaultEffort    string   `json:"defaultEffort"`
}

type Store struct {
	mu   sync.RWMutex
	path string
	cfg  Config
}

func LoadStore(configPath, defaultWorkspace string) (*Store, error) {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		defaultWorkspace = home
	}
	s := &Store{
		path: configPath,
		cfg: Config{
			Port:            67,
			ActiveWorkspace: defaultWorkspace,
			DefaultAgent:    "agy",
			DefaultEffort:   "medium",
		},
	}

	data, err := os.ReadFile(configPath)
	if err == nil {
		var loaded Config
		if err := json.Unmarshal(data, &loaded); err == nil {
			s.cfg = loaded
		}
	}

	if s.cfg.Port <= 0 || s.cfg.Port > 65535 {
		s.cfg.Port = 67
	}
	if strings.TrimSpace(s.cfg.DefaultAgent) == "" {
		s.cfg.DefaultAgent = "agy"
	}

	if strings.TrimSpace(s.cfg.ActiveWorkspace) == "" {
		s.cfg.ActiveWorkspace = defaultWorkspace
	} else if info, err := os.Stat(s.cfg.ActiveWorkspace); err != nil || !info.IsDir() {
		s.cfg.ActiveWorkspace = defaultWorkspace
	}

	if s.cfg.RecentWorkspaces == nil {
		s.cfg.RecentWorkspaces = []string{}
	}
	s.recordRecentLocked(s.cfg.ActiveWorkspace)
	_ = s.saveLocked()
	return s, nil
}

func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := s.cfg
	cp.RecentWorkspaces = append([]string(nil), s.cfg.RecentWorkspaces...)
	return cp
}

func (s *Store) Update(fn func(*Config)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.cfg)
	if strings.TrimSpace(s.cfg.DefaultAgent) == "" {
		s.cfg.DefaultAgent = "agy"
	}
	if s.cfg.ActiveWorkspace != "" {
		s.recordRecentLocked(s.cfg.ActiveWorkspace)
	}
	return s.saveLocked()
}

func (s *Store) recordRecentLocked(dir string) {
	dir = filepath.Clean(strings.TrimSpace(dir))
	if dir == "" {
		return
	}
	out := []string{dir}
	for _, existing := range s.cfg.RecentWorkspaces {
		cleaned := filepath.Clean(strings.TrimSpace(existing))
		if cleaned == "" || strings.EqualFold(cleaned, dir) {
			continue
		}
		out = append(out, cleaned)
		if len(out) >= maxRecentWorkspaces {
			break
		}
	}
	s.cfg.RecentWorkspaces = out
}

func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, append(data, '\n'), 0644)
}
