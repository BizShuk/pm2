package config

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// githubRepoRE matches GitHub owner/repo shorthand.
// Owner: alphanumeric, may contain single hyphens/underscores/dots between alphanumeric segments.
// Repo:  same rules as owner.
var githubRepoRE = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?/[a-zA-Z0-9._-]+$`)

// defaultBranches is the ordered list of branch names to probe
// when fetching a remote ecosystem config.
var defaultBranches = []string{"main", "master"}

// configFileNames is the ordered list of ecosystem config filenames to probe.
var configFileNames = []string{"ecosystem.config.js", "ecosystem.config.json"}

// IsRemoteRef returns true if path looks like a remote reference
// (GitHub owner/repo shorthand or HTTPS URL) rather than a local file.
func IsRemoteRef(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".js" || ext == ".json" {
		return false
	}
	if strings.HasPrefix(path, "https://github.com/") || strings.HasPrefix(path, "http://github.com/") {
		return true
	}
	return githubRepoRE.MatchString(path)
}

// ResolveRemote fetches the ecosystem config from a GitHub repo via
// the raw.githubusercontent.com blob endpoint and writes it to
// cacheDir. Returns the local path to the downloaded config file.
//
// remoteRef is either:
//   - owner/repo  (e.g. "bizshuk/pm2")
//   - https://github.com/owner/repo[.git]
func ResolveRemote(remoteRef, cacheDir string) (string, error) {
	owner, repo, err := parseGitHubRef(remoteRef)
	if err != nil {
		return "", err
	}

	repoCacheDir := filepath.Join(cacheDir, owner, repo)
	if err := os.MkdirAll(repoCacheDir, 0o755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	// Probe (branch, filename) combos in order.
	for _, branch := range defaultBranches {
		for _, fn := range configFileNames {
			rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/refs/heads/%s/%s", owner, repo, branch, fn)
			body, status, err := fetchRaw(rawURL)

			if status == http.StatusNotFound {
				// Try next combo.
				continue
			}
			if err != nil {
				// Non-404 error: network problem, etc.
				return "", fmt.Errorf("fetch %s: %w", rawURL, err)
			}

			// Success — write to cache.
			localPath := filepath.Join(repoCacheDir, fn)
			if err := os.WriteFile(localPath, body, 0o644); err != nil {
				return "", fmt.Errorf("write config: %w", err)
			}
			return localPath, nil
		}
	}

	return "", fmt.Errorf(
		"no ecosystem.config.js or ecosystem.config.json found in %s/%s (tried branches: %s)",
		owner, repo, strings.Join(defaultBranches, ", "),
	)
}

// fetchRaw GETs a URL and returns the body bytes, HTTP status, and any error.
// Non-2xx status is NOT treated as error — the caller decides how to handle it.
func fetchRaw(rawURL string) ([]byte, int, error) {
	resp, err := http.Get(rawURL)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.StatusCode, nil
	}
	return body, resp.StatusCode, nil
}

// parseGitHubRef extracts (owner, repo) from a remote reference.
func parseGitHubRef(ref string) (owner, repo string, err error) {
	if strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "http://") {
		u, uErr := url.Parse(ref)
		if uErr != nil {
			return "", "", fmt.Errorf("invalid URL: %w", uErr)
		}
		if u.Host != "github.com" {
			return "", "", fmt.Errorf("only github.com is supported, got: %s", u.Host)
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) < 2 {
			return "", "", fmt.Errorf("invalid GitHub URL: %s", ref)
		}
		owner = parts[0]
		repo = strings.TrimSuffix(parts[1], ".git")
		if owner == "" || repo == "" {
			return "", "", fmt.Errorf("invalid GitHub URL: %s", ref)
		}
		return owner, repo, nil
	}

	if githubRepoRE.MatchString(ref) {
		parts := strings.SplitN(ref, "/", 2)
		return parts[0], parts[1], nil
	}

	return "", "", fmt.Errorf("not a valid GitHub reference: %s", ref)
}
