// Package config handles ecosystem config loading and remote resolution.
//
// Remote resolution uses codeload.github.com tarballs (same mechanism as
// bizshuk/skills svc/plugin/fetch.go), which requires no authentication and
// has no rate-limiting issues unlike raw.githubusercontent.com.
package config

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// githubRepoRE matches GitHub owner/repo shorthand.
var githubRepoRE = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?/[a-zA-Z0-9._-]+$`)

// configFileName is the ecosystem config filename to look for at the repo root.
const configFileName = "ecosystem.config.js"

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

// ResolveRemote resolves a remote GitHub reference to a local ecosystem
// config file path. It first checks the cache directory; a cached file is
// reused as-is. Otherwise it downloads the repo tarball from codeload.
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

	// If a cached config already exists, use it.
	cached := filepath.Join(repoCacheDir, configFileName)
	if st, err := os.Stat(cached); err == nil && st.Size() > 0 {
		return cached, nil
	}

	if err := os.MkdirAll(repoCacheDir, 0o755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	archiveURL := fmt.Sprintf("https://codeload.github.com/%s/%s/tar.gz/HEAD", owner, repo)

	for attempt := 1; attempt <= 5; attempt++ {
		found, err := downloadAndExtractConfig(archiveURL, repoCacheDir)
		if err == nil {
			return found, nil
		}
		if !isRetryable(err) || attempt == 5 {
			return "", err
		}
		time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
	}

	return "", fmt.Errorf("no %s found in %s/%s", configFileName, owner, repo)
}

// downloadAndExtractConfig fetches a tarball from codeload and extracts
// ecosystem.config.js into dest. Returns the path to the config file.
func downloadAndExtractConfig(archiveURL, dest string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, archiveURL, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "pm2/1.0.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", retryable(fmt.Errorf("download: %w", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return "", retryable(fmt.Errorf("HTTP %d from %s", resp.StatusCode, archiveURL))
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, archiveURL)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return "", retryable(fmt.Errorf("gzip: %w", err))
	}
	defer gz.Close()

	tr := tar.NewReader(gz)

	// The archive has a top-level "<repo>-<branch>/" prefix. We learn it
	// from the first entry's path and strip it from all subsequent paths.
	var topPrefix string

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", retryable(fmt.Errorf("tar: %w", err))
		}

		name := strings.TrimPrefix(hdr.Name, "./")

		if topPrefix == "" {
			if i := strings.Index(name, "/"); i >= 0 {
				topPrefix = name[:i+1]
			}
		}

		rel := strings.TrimPrefix(name, topPrefix)

		// Only care about ecosystem.config.js at the repo root.
		if rel != configFileName || hdr.Typeflag != tar.TypeReg {
			continue
		}

		target := filepath.Join(dest, rel)
		f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return "", fmt.Errorf("write %s: %w", rel, err)
		}
		if _, err := io.Copy(f, tr); err != nil {
			f.Close()
			return "", fmt.Errorf("copy %s: %w", rel, err)
		}
		f.Close()

		return target, nil
	}

	return "", fmt.Errorf("no %s found in tarball", configFileName)
}

// retryable wraps an error that may succeed on retry (network blip, 5xx).
type retryableError struct{ err error }

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

func retryable(err error) error { return &retryableError{err: err} }

func isRetryable(err error) bool {
	_, ok := err.(*retryableError)
	return ok
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
