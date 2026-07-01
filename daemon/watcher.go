package daemon

import (
	"log/slog"
	"time"

	"github.com/bizshuk/pm2/model"
	"github.com/fsnotify/fsnotify"
)

// debounceDuration is how long the watcher waits after a file event before
// actually triggering a restart. Prevents restart storms during fast saves.
const debounceDuration = 500 * time.Millisecond

// startFileWatcher creates an fsnotify.Watcher on req.Script and returns it.
// A goroutine is launched that debounces events for debounceDuration before
// calling s.restartByName. Returns (nil, nil) if req.Watch is false or the
// watcher could not be created.
func (s *Server) startFileWatcher(req *model.AppStartReq, name string) (*fsnotify.Watcher, error) {
	if !req.Watch {
		return nil, nil
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Info("create watcher error", "name", name, "err", err)
		return nil, err
	}
	if err := w.Add(req.Script); err != nil {
		_ = w.Close()
		slog.Info("watch error", "name", name, "err", err)
		return nil, err
	}
	go func(pName string, w *fsnotify.Watcher) {
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
						slog.Info("File changed, restarting", "event", event.Name, "name", pName)
						_ = s.restartByName(pName)
					})
				}
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				slog.Info("watcher error", "name", pName, "err", err)
			}
		}
	}(name, w)
	return w, nil
}