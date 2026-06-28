package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// getAppVersion looks for a package.json up to 5 parent directories above
// the script path and returns its version. Returns "-" when no package.json
// is found or it has no version field.
func getAppVersion(scriptPath string) string {
	dir := filepath.Dir(scriptPath)
	for i := 0; i < 5; i++ {
		pkgPath := filepath.Join(dir, "package.json")
		if _, err := os.Stat(pkgPath); err == nil {
			data, err := os.ReadFile(pkgPath)
			if err == nil {
				var pkg struct {
					Version string `json:"version"`
				}
				if err := json.Unmarshal(data, &pkg); err == nil && pkg.Version != "" {
					return pkg.Version
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "-"
}