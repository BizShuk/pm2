package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/bizshuk/pm2/process"
)

func TestResolveScriptPath(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pm2-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Test 1: Absolute path should remain unchanged
	absPath := "/usr/bin/node"
	res := process.ResolveScriptPath(tempDir, absPath)
	if res != absPath {
		t.Errorf("Expected %q, got %q", absPath, res)
	}

	// Test 2: Script path with separator should be resolved to absolute path
	relPath := "./bin/server"
	expectedAbs := filepath.Join(tempDir, relPath)
	res = process.ResolveScriptPath(tempDir, relPath)
	if res != expectedAbs {
		t.Errorf("Expected %q, got %q", expectedAbs, res)
	}

	// Test 3: Bare filename that exists in baseDir should be resolved
	scriptName := "run.sh"
	f, err := os.Create(filepath.Join(tempDir, scriptName))
	if err != nil {
		t.Fatalf("failed to create dummy script: %v", err)
	}
	f.Close()

	expectedAbs2 := filepath.Join(tempDir, scriptName)
	res = process.ResolveScriptPath(tempDir, scriptName)
	if res != expectedAbs2 {
		t.Errorf("Expected %q, got %q", expectedAbs2, res)
	}

	// Test 4: Bare filename that does not exist in baseDir but exists in PATH should be resolved to absolute path
	cmdName := "sh"
	expectedPath, err := exec.LookPath(cmdName)
	if err == nil {
		if abs, err := filepath.Abs(expectedPath); err == nil {
			expectedPath = abs
		}
		res = process.ResolveScriptPath(tempDir, cmdName)
		if res != expectedPath {
			t.Errorf("Expected %q, got %q", expectedPath, res)
		}
	}

	// Test 5: Bare filename that does not exist in baseDir nor in PATH should be left as-is
	nonExistentCmd := "nonexistentcommand12345"
	res = process.ResolveScriptPath(tempDir, nonExistentCmd)
	if res != nonExistentCmd {
		t.Errorf("Expected %q, got %q", nonExistentCmd, res)
	}
}
