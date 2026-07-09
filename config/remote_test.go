package config

import (
	"strings"
	"testing"
)

func TestIsRemoteRef(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		// local files
		{"ecosystem.config.js", false},
		{"ecosystem.config.json", false},
		{"/abs/path/ecosystem.config.js", false},
		{"./ecosystem.config.json", false},

		// GitHub owner/repo shorthand
		{"bizshuk/pm2", true},
		{"some-org/some-repo", true},
		{"a/b", true},
		{"owner-with-dots.here/repo", true},
		{"owner/repo-name", true},

		// GitHub HTTPS URLs
		{"https://github.com/bizshuk/pm2", true},
		{"http://github.com/bizshuk/pm2", true},
		{"https://github.com/bizshuk/pm2.git", true},

		// Not remote refs
		{"some/thing/else", false},           // too many slashes
		{"just-a-file.js", false},
		{"", false},
	}
	for _, tc := range tests {
		got := IsRemoteRef(tc.path)
		if got != tc.expected {
			t.Errorf("IsRemoteRef(%q) = %v, want %v", tc.path, got, tc.expected)
		}
	}
}

func TestParseGitHubRef(t *testing.T) {
	tests := []struct {
		ref         string
		wantOwner   string
		wantRepo    string
		wantErr     bool
		errContains string
	}{
		{"bizshuk/pm2", "bizshuk", "pm2", false, ""},
		{"owner/repo-name", "owner", "repo-name", false, ""},
		{"https://github.com/bizshuk/pm2", "bizshuk", "pm2", false, ""},
		{"https://github.com/bizshuk/pm2.git", "bizshuk", "pm2", false, ""},
		{"http://github.com/owner/repo", "owner", "repo", false, ""},
		{"https://gitlab.com/owner/repo", "", "", true, "only github.com"},
		{"not-a-ref", "", "", true, "not a valid"},
		{"https://github.com/just-owner", "", "", true, "invalid GitHub URL"},
	}
	for _, tc := range tests {
		owner, repo, err := parseGitHubRef(tc.ref)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseGitHubRef(%q) expected error", tc.ref)
			} else if tc.errContains != "" {
				errStr := err.Error()
				if !strings.Contains(errStr, tc.errContains) {
					t.Errorf("parseGitHubRef(%q) error %q should contain %q", tc.ref, errStr, tc.errContains)
				}
			}
			continue
		}
		if err != nil {
			t.Errorf("parseGitHubRef(%q) unexpected error: %v", tc.ref, err)
			continue
		}
		if owner != tc.wantOwner {
			t.Errorf("parseGitHubRef(%q) owner = %q, want %q", tc.ref, owner, tc.wantOwner)
		}
		if repo != tc.wantRepo {
			t.Errorf("parseGitHubRef(%q) repo = %q, want %q", tc.ref, repo, tc.wantRepo)
		}
	}
}
