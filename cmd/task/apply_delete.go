package task

import (
	"fmt"
	"io"
	"strings"

	cliruntime "github.com/bizshuk/pm2/cmd/runtime"
	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
)

// deleteEcosystemApps removes every task declared by an ecosystem file from
// the daemon registry. Each app is addressed by its exact
// "namespace:name" key so a delete never reaches a same-named app that
// belongs to another ecosystem file.
//
// An app the daemon does not know is reported and skipped rather than
// aborting the sweep: an ecosystem file routinely describes more apps than
// are currently registered (optional apps never resumed, a partial apply).
// The command only fails when nothing at all matched, so a wrong file or a
// stale config is still visible as an error.
func deleteEcosystemApps(apps []process.AppConfig, out io.Writer) error {
	if len(apps) == 0 {
		return fmt.Errorf("ecosystem file declares no apps to delete")
	}

	var (
		deleted int
		missing []string
	)
	for _, app := range apps {
		key := appSelectionKey(app)
		resp, err := model.SendRequest(cliruntime.SocketPath(), model.Request{
			Command: model.CmdDelete,
			Name:    key,
		})
		if err != nil {
			return err
		}
		if !resp.OK {
			missing = append(missing, key)
			fmt.Fprintf(out, "skipped: %s (%s)\n", key, resp.Error)
			continue
		}
		deleted++
		fmt.Fprintf(out, "deleted: %s\n", key)
	}

	if deleted == 0 {
		return fmt.Errorf("no task from this ecosystem file is registered: %s",
			strings.Join(missing, ", "))
	}
	return nil
}
