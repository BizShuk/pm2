package task

import (
	"fmt"

	"github.com/bizshuk/pm2/process"
)

// registeredTasks is the daemon's process list indexed so apply can tell
// an app it is about to replace from one it registers for the first time.
//
// It mirrors the daemon's own launch lookup (findExistingForLaunchUnderLock):
// an exact namespace:name hit first, then the same name declared by the
// same ecosystem file — which is how an app whose namespace changed in the
// file is still recognised as the task it replaces.
type registeredTasks struct {
	byKey  map[string]bool // namespace:name -> paused
	byFile map[string]bool // config file + NUL + name -> paused
}

func newRegisteredTasks(infos []process.ProcessInfo) registeredTasks {
	registered := registeredTasks{
		byKey:  make(map[string]bool, len(infos)),
		byFile: make(map[string]bool, len(infos)),
	}
	for _, info := range infos {
		paused := info.Status == process.StatusPaused
		registered.byKey[info.Namespace+":"+info.Name] = paused
		if info.ConfigFile != "" {
			registered.byFile[info.ConfigFile+"\x00"+info.Name] = paused
		}
	}
	return registered
}

// pausedState reports whether app is already registered and, when it is,
// whether it is paused. An app with several instances counts as paused only
// when every registered instance is: one running instance means the user
// left the app enabled.
func (r registeredTasks) pausedState(app process.AppConfig) (paused, found bool) {
	namespace := app.Namespace
	if namespace == "" {
		namespace = process.DefaultNamespace
	}
	instances := app.Instances
	if instances <= 0 {
		instances = process.DefaultInstances
	}

	paused = true
	for i := 0; i < instances; i++ {
		name := app.Name
		if instances > 1 {
			name = fmt.Sprintf("%s-%d", app.Name, i)
		}
		instancePaused, ok := r.byKey[namespace+":"+name]
		if !ok && app.ConfigFile != "" {
			instancePaused, ok = r.byFile[app.ConfigFile+"\x00"+name]
		}
		if !ok {
			continue
		}
		found = true
		paused = paused && instancePaused
	}
	return paused && found, found
}
