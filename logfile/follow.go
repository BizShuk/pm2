package logfile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

const followPollInterval = 25 * time.Millisecond

type followState struct {
	source  Source
	info    os.FileInfo
	offset  int64
	pending []byte
}

// Follow starts at each existing source's current end and emits subsequently
// appended complete logical lines until ctx is cancelled. New, replaced, and
// truncated source paths are followed from byte zero. Both returned channels
// close when following stops.
func Follow(ctx context.Context, sources []Source) (<-chan Entry, <-chan error) {
	return follow(ctx, sources, followPollInterval, time.Now)
}

func follow(
	ctx context.Context,
	sources []Source,
	interval time.Duration,
	now func() time.Time,
) (<-chan Entry, <-chan error) {
	entries := make(chan Entry)
	errs := make(chan error, max(1, len(sources)))
	states := make([]followState, len(sources))
	initialErrors := make([]error, 0, len(sources))

	for index, source := range sources {
		states[index].source = source
		info, err := os.Stat(source.Path)
		switch {
		case err == nil:
			states[index].info = info
			states[index].offset = info.Size()
		case errors.Is(err, os.ErrNotExist):
		default:
			initialErrors = append(initialErrors, followError(source, err))
		}
	}

	go func() {
		defer close(entries)
		defer close(errs)

		for _, err := range initialErrors {
			sendFollowError(errs, err)
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for index := range states {
					polled, err := pollFollowState(&states[index], now())
					if err != nil {
						sendFollowError(errs, followError(states[index].source, err))
						continue
					}
					for _, entry := range polled {
						select {
						case entries <- entry:
						case <-ctx.Done():
							return
						}
					}
				}
			}
		}
	}()

	return entries, errs
}

func pollFollowState(state *followState, observedAt time.Time) ([]Entry, error) {
	info, err := os.Stat(state.source.Path)
	if errors.Is(err, os.ErrNotExist) {
		state.info = nil
		state.offset = 0
		state.pending = nil
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat path: %w", err)
	}

	if state.info == nil ||
		!os.SameFile(state.info, info) ||
		info.Size() < state.offset {
		state.offset = 0
		state.pending = nil
	}
	state.info = info
	if info.Size() == state.offset {
		return nil, nil
	}

	data, err := readFollowBytes(state.source.Path, state.offset, info.Size()-state.offset)
	if err != nil {
		return nil, err
	}
	state.offset += int64(len(data))
	return completeEntries(state, data, observedAt), nil
}

func readFollowBytes(path string, offset, length int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open path: %w", err)
	}
	data, readErr := io.ReadAll(io.NewSectionReader(file, offset, length))
	closeErr := file.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read path: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close path: %w", closeErr)
	}
	return data, nil
}

func completeEntries(state *followState, data []byte, observedAt time.Time) []Entry {
	combined := make([]byte, 0, len(state.pending)+len(data))
	combined = append(combined, state.pending...)
	combined = append(combined, data...)

	var entries []Entry
	lineStart := 0
	for index, value := range combined {
		if value != '\n' {
			continue
		}
		line := string(combined[lineStart:index])
		entries = append(entries, newEntry(state.source, line, observedAt))
		lineStart = index + 1
	}
	state.pending = append(state.pending[:0], combined[lineStart:]...)
	return entries
}

func followError(source Source, err error) error {
	return fmt.Errorf("follow %s log %q: %w", source.AppName, source.Path, err)
}

func sendFollowError(errs chan<- error, err error) {
	select {
	case errs <- err:
	default:
	}
}
