package gpuagent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bizshuk/pm2/sysmon"
)

// exportFileMode makes the reading readable by everyone and writable
// only by the root process that produced it. That asymmetry is the
// entire security argument for this design, so it is set explicitly
// rather than left to whatever umask the supervisor happened to hand
// the agent.
const exportFileMode = 0o644

// publish writes one reading to path atomically.
//
// The temp file is created in the same directory as the target so the
// rename stays within one filesystem; a cross-device rename would fail
// and leave readers on the previous sample forever.
func publish(path string, gpu sysmon.GPU) error {
	encoded, err := json.Marshal(gpu)
	if err != nil {
		return fmt.Errorf("encode gpu reading: %w", err)
	}

	temp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp export: %w", err)
	}
	tempPath := temp.Name()

	if _, err := temp.Write(encoded); err != nil {
		temp.Close()
		os.Remove(tempPath)
		return fmt.Errorf("write temp export: %w", err)
	}
	if err := temp.Close(); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("close temp export: %w", err)
	}
	// CreateTemp opens at 0600; the point of the file is that an
	// unprivileged reader can see it.
	if err := os.Chmod(tempPath, exportFileMode); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("chmod temp export: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("publish export: %w", err)
	}
	return nil
}
