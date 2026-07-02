package executor

import (
	"log/slog"
	"time"

	"github.com/fsnotify/fsnotify"
)

// debounceDuration is how long the watcher waits after a file event
// before triggering onDetect. Prevents restart storms during fast saves.
const debounceDuration = 500 * time.Millisecond

// NewFileWatcher creates an fsnotify.Watcher on `path` and launches a
// goroutine that debounces Write/Rename events for debounceDuration,
// then calls onDetect.
//
// Returns (nil, nil) if the watcher could not be created or the path
// could not be added — onDetect is never called in that case.
//
// `onDetect` is the caller-provided restart hook (the Server passes a
// closure that calls its restartByName). onDetect may be nil; in that
// case the watcher logs the change but does not act on it.
//
// This is a free function (not a method) so the executor package has
// no per-instance state dependency on file watching.
func NewFileWatcher(path string, onDetect func()) (*fsnotify.Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Info("create watcher error", "path", path, "err", err)
		return nil, err
	}
	if err := w.Add(path); err != nil {
		_ = w.Close()
		slog.Info("watch error", "path", path, "err", err)
		return nil, err
	}
	go func() {
		var timer *time.Timer
		for {
			select {
			case event, ok := <-w.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Rename) {
					if timer != nil {
						timer.Stop()
					}
					timer = time.AfterFunc(debounceDuration, func() {
						slog.Info("File changed, restarting", "event", event.Name, "path", path)
						if onDetect != nil {
							onDetect()
						}
					})
				}
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				slog.Info("watcher error", "path", path, "err", err)
			}
		}
	}()
	return w, nil
}