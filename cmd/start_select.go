package cmd

import (
	"fmt"
	"strings"

	"github.com/bizshuk/pm2/process"
)

// selectApps prepares every app in an ecosystem file for registration,
// applying the install policy carried by process.AppConfig.Optional.
//
// Policy:
//   - Optional == false (the zero value) — always selected.
//   - Optional == true — always selected, but registered paused unless all is
//     set or the app is named in with (matched on "name" or
//     "namespace:name").
//
// The policy is applied uniformly to local and remote ecosystem files:
// `optional` is a property of the app, not of how the config was fetched.
//
// An entry in with that matches no app is an error rather than a silent
// no-op, so a typo does not quietly leave the app paused.
func selectApps(apps []process.AppConfig, all bool, with []string) (selected, paused []process.AppConfig, err error) {
	wanted := make(map[string]bool, len(with))
	for _, w := range with {
		w = strings.TrimSpace(w)
		if w != "" {
			wanted[w] = false // value tracks "was matched"
		}
	}

	for _, app := range apps {
		// Match before the Optional check so that naming a required app
		// is a harmless no-op rather than an "unknown app" error.
		named := false
		for _, key := range []string{app.Name, app.Namespace + ":" + app.Name} {
			if _, ok := wanted[key]; ok {
				wanted[key] = true
				named = true
			}
		}

		if app.Optional {
			app.Paused = !all && !named
			if app.Paused {
				paused = append(paused, app)
			}
		}
		selected = append(selected, app)
	}

	var unknown []string
	for _, w := range with {
		w = strings.TrimSpace(w)
		if w != "" && !wanted[w] {
			unknown = append(unknown, w)
		}
	}
	if len(unknown) > 0 {
		return nil, nil, fmt.Errorf("--with names no app in this ecosystem file: %s", strings.Join(unknown, ", "))
	}

	return selected, paused, nil
}
