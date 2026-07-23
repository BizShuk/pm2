package cmd

import (
	"fmt"
	"strings"

	"github.com/bizshuk/pm2/process"
)

// selectApps splits the apps of an ecosystem file into the ones to start
// and the optional ones being skipped, applying the install policy carried
// by process.AppConfig.Optional.
//
// Policy:
//   - Optional == false (the zero value) — always selected.
//   - Optional == true  — selected only when all is set, or when the app is
//     named in with (matched on "name" or "namespace:name").
//
// The policy is applied uniformly to local and remote ecosystem files:
// `optional` is a property of the app, not of how the config was fetched.
//
// An entry in with that matches no app is an error rather than a silent
// no-op, so a typo does not quietly leave the app unstarted.
func selectApps(apps []process.AppConfig, all bool, with []string) (selected, skipped []process.AppConfig, err error) {
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

		if !app.Optional || all || named {
			selected = append(selected, app)
		} else {
			skipped = append(skipped, app)
		}
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

	return selected, skipped, nil
}
