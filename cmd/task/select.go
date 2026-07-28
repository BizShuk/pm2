package task

import (
	"fmt"
	"strconv"
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
		for _, key := range []string{app.Name, appSelectionKey(app)} {
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

func selectSingleApp(apps []process.AppConfig, choice string) (process.AppConfig, error) {
	if len(apps) == 0 {
		return process.AppConfig{}, fmt.Errorf("cannot select a single app: ecosystem has no apps")
	}

	choice = strings.TrimSpace(choice)
	if choice == "" {
		return process.AppConfig{}, fmt.Errorf("single app selection cannot be empty")
	}

	if number, err := strconv.Atoi(choice); err == nil {
		if number < 1 || number > len(apps) {
			return process.AppConfig{}, fmt.Errorf(
				"single app choice %q is out of range (1-%d)", choice, len(apps),
			)
		}
		app := apps[number-1]
		app.Paused = false
		return app, nil
	}

	var matches []process.AppConfig
	for _, app := range apps {
		if strings.Contains(choice, ":") {
			if appSelectionKey(app) == choice {
				matches = append(matches, app)
			}
			continue
		}
		if app.Name == choice {
			matches = append(matches, app)
		}
	}

	switch len(matches) {
	case 0:
		return process.AppConfig{}, fmt.Errorf("single app choice %q names no app in this ecosystem file", choice)
	case 1:
		matches[0].Paused = false
		return matches[0], nil
	default:
		return process.AppConfig{}, fmt.Errorf(
			"single app choice %q is ambiguous; use namespace:name or a number", choice,
		)
	}
}

func appSelectionKey(app process.AppConfig) string {
	if app.Namespace == "" {
		return app.Name
	}
	return app.Namespace + ":" + app.Name
}
