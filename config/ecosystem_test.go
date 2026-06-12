package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveScriptPath(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pm2-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Test 1: Absolute path should remain unchanged
	absPath := "/usr/bin/node"
	res := resolveScriptPath(tempDir, absPath)
	if res != absPath {
		t.Errorf("Expected %q, got %q", absPath, res)
	}

	// Test 2: Script path with separator should be resolved to absolute path
	relPath := "./bin/server"
	expectedAbs := filepath.Join(tempDir, relPath)
	res = resolveScriptPath(tempDir, relPath)
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
	res = resolveScriptPath(tempDir, scriptName)
	if res != expectedAbs2 {
		t.Errorf("Expected %q, got %q", expectedAbs2, res)
	}

	// Test 4: Bare filename that does not exist in baseDir should be left as-is (e.g. system command on PATH)
	cmdName := "python3"
	res = resolveScriptPath(tempDir, cmdName)
	if res != cmdName {
		t.Errorf("Expected %q, got %q", cmdName, res)
	}
}
