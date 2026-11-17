package git

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"paihub/internal/sysenv"
)

const cmdTimeout = 45 * time.Second

type FileChange struct {
	Code string `json:"code"`
	Path string `json:"path"`
}

type StatusResult struct {
	IsGit      bool         `json:"isGit"`
	HasChanges bool         `json:"hasChanges"`
	Changes    []FileChange `json:"changes"`
}

type GitHubRepo struct {
	NameWithOwner string `json:"nameWithOwner"`
	Description   string `json:"description"`
	IsPrivate     bool   `json:"isPrivate"`
	UpdatedAt     string `json:"updatedAt"`
	URL           string `json:"url"`
}

type GitHubReposResult struct {
	Authenticated bool         `json:"authenticated"`
	User          string       `json:"user"`
	Error         string       `json:"error,omitempty"`
	Repos         []GitHubRepo `json:"repos"`
}

func runCmd(dir, name string, args ...string) (string, error) {
	sysenv.EnrichPath()
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = os.Environ()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	combined := strings.TrimSpace(stdout.String())
	if stderrStr := strings.TrimSpace(stderr.String()); stderrStr != "" {
		if combined != "" {
			combined += "\n" + stderrStr
		} else {
			combined = stderrStr
		}
	}
	if err != nil {
		if combined != "" {
			return combined, fmt.Errorf("%w: %s", err, combined)
		}
		return "", err
	}
	return stdout.String(), nil
}

func GetStatus(dir string) *StatusResult {
	res := &StatusResult{
		Changes: []FileChange{},
	}
	if strings.TrimSpace(dir) == "" {
		return res
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return res
	}

	inside, err := runCmd(dir, "git", "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(inside) != "true" {
		return res
	}
	res.IsGit = true

	statusOut, err := runCmd(dir, "git", "status", "--porcelain=v1")
	if err == nil && statusOut != "" {
		raw := strings.TrimRight(statusOut, "\r\n")
		if raw != "" {
			for _, line := range strings.Split(raw, "\n") {
				line = strings.TrimRight(line, "\r")
				if len(line) < 4 {
					continue
				}
				code := strings.TrimSpace(line[:2])
				path := strings.TrimSpace(line[3:])
				path = strings.Trim(path, `"`)
				res.Changes = append(res.Changes, FileChange{
					Code: code,
					Path: path,
				})
			}
		}
	}
	res.HasChanges = len(res.Changes) > 0
	return res
}

var ghAccountRe = regexp.MustCompile(`Logged in to [^\s]+ account ([^\s(]+)`)

func ListGitHubRepos() *GitHubReposResult {
	res := &GitHubReposResult{
		Repos: []GitHubRepo{},
	}
	authOut, authErr := runCmd("", "gh", "auth", "status")
	if m := ghAccountRe.FindStringSubmatch(authOut); len(m) >= 2 {
		res.User = m[1]
	}
	if authErr != nil && res.User == "" {
		res.Error = "GitHub CLI is not signed in or not found. Run `gh auth login` in the Terminal tab."
		return res
	}
	res.Authenticated = true

	rawAPI, apiErr := runCmd("", "gh", "api", "--paginate", "user/repos?per_page=100&sort=updated&affiliation=owner,collaborator,organization_member")
	if apiErr == nil {
		if repos, pErr := ParsePaginatedUserRepos(rawAPI); pErr == nil {
			res.Repos = repos
			return res
		}
	}

	raw, err := runCmd("", "gh", "repo", "list", "--limit", "100", "--json", "nameWithOwner,description,isPrivate,updatedAt,url")
	if err != nil {
		res.Error = fmt.Sprintf("Failed to fetch repository list: %v", err)
		return res
	}
	if err := json.Unmarshal([]byte(raw), &res.Repos); err != nil {
		res.Error = fmt.Sprintf("Failed to parse repository list: %v", err)
		return res
	}
	return res
}

type apiRepoEntry struct {
	FullName    string `json:"full_name"`
	Description string `json:"description"`
	Private     bool   `json:"private"`
	UpdatedAt   string `json:"updated_at"`
	HTMLURL     string `json:"html_url"`
}

func ParsePaginatedUserRepos(raw string) ([]GitHubRepo, error) {
	dec := json.NewDecoder(strings.NewReader(raw))
	seen := make(map[string]bool)
	var out []GitHubRepo

	for {
		var page []apiRepoEntry
		if err := dec.Decode(&page); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		for _, item := range page {
			key := strings.ToLower(item.FullName)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, GitHubRepo{
				NameWithOwner: item.FullName,
				Description:   item.Description,
				IsPrivate:     item.Private,
				UpdatedAt:     item.UpdatedAt,
				URL:           item.HTMLURL,
			})
		}
	}
	if out == nil {
		out = []GitHubRepo{}
	}
	return out, nil
}

func CloneRepo(repo, destParent string) (string, error) {
	repo = strings.TrimSpace(repo)
	destParent = strings.TrimSpace(destParent)
	if repo == "" {
		return "", errors.New("GitHub repository name or URL cannot be empty")
	}
	if destParent == "" {
		return "", errors.New("clone destination folder cannot be empty")
	}
	if err := os.MkdirAll(destParent, 0755); err != nil {
		return "", err
	}

	folderName := extractRepoFolderName(repo)
	if folderName == "" {
		return "", errors.New("invalid repository name/URL format")
	}
	targetDir := filepath.Join(filepath.Clean(destParent), folderName)
	if _, err := os.Stat(targetDir); err == nil {
		return "", fmt.Errorf("folder %s already exists; open it directly via the Folder Picker or choose another destination folder", targetDir)
	}

	if _, err := runCmd(destParent, "gh", "repo", "clone", repo, targetDir); err != nil {
		gitTarget := repo
		if !strings.Contains(repo, "://") && !strings.HasPrefix(repo, "git@") {
			gitTarget = "https://github.com/" + strings.TrimPrefix(repo, "/") + ".git"
		}
		if _, gErr := runCmd(destParent, "git", "clone", gitTarget, targetDir); gErr != nil {
			return "", fmt.Errorf("failed to clone repository: %v", err)
		}
	}
	return targetDir, nil
}

func extractRepoFolderName(repo string) string {
	s := strings.TrimSpace(repo)
	s = strings.TrimSuffix(s, "/")
	s = strings.TrimSuffix(s, ".git")
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '/' || r == ':'
	})
	if len(parts) == 0 {
		return ""
	}
	name := parts[len(parts)-1]
	if name == "." || name == ".." || strings.ContainsAny(name, `<>:"/\|?*`) {
		return ""
	}
	return name
}

func CommitAndPush(dir, message string) (string, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		message = "chore: update via PAIHub"
	}
	if _, err := runCmd(dir, "git", "add", "-A"); err != nil {
		return "", fmt.Errorf("git add failed: %w", err)
	}

	var outLog strings.Builder
	if _, err := runCmd(dir, "git", "diff", "--cached", "--quiet"); err != nil {
		cOut, cErr := runCmd(dir, "git", "commit", "-m", message)
		if cErr != nil {
			return "", fmt.Errorf("git commit failed: %w", cErr)
		}
		outLog.WriteString(strings.TrimSpace(cOut) + "\n")
	} else {
		outLog.WriteString("No new changes to commit.\n")
	}

	if remote, err := runCmd(dir, "git", "remote", "get-url", "origin"); err == nil && strings.TrimSpace(remote) != "" {
		pOut, pErr := runCmd(dir, "git", "push", "-u", "origin", "HEAD")
		if pErr != nil {
			return outLog.String(), fmt.Errorf("git push failed: %w", pErr)
		}
		outLog.WriteString(strings.TrimSpace(pOut))
	} else {
		outLog.WriteString("Local commit succeeded (no 'origin' remote configured).")
	}

	return strings.TrimSpace(outLog.String()), nil
}
