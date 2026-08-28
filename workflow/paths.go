package workflow

import "path/filepath"

// Dir is the workflow subtree of the pm2 state root.
func Dir(root string) string { return filepath.Join(root, "workflows") }

// DumpPath holds the registered workflow definitions.
//
// It is a separate file from the task dump on purpose. Changing the
// shape of dump.json would fire its "format incompatible — run pm2
// delete all" message on every existing installation at upgrade; and
// more fundamentally, a workflow definition is not process state. It
// needs loading at boot so cron can be armed, never replaying.
func DumpPath(root string) string { return filepath.Join(Dir(root), "dump.json") }
