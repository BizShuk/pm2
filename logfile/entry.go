package logfile

import (
	"fmt"
	"strings"
	"time"
)

// Stream identifies the managed output stream that produced an Entry.
type Stream string

const (
	// StreamStdout identifies a managed application's standard output.
	StreamStdout Stream = "stdout"
	// StreamStderr identifies a managed application's standard error.
	StreamStderr Stream = "stderr"
)

// Source identifies one current managed-log path to follow.
type Source struct {
	AppName string
	Path    string
	Stream  Stream
}

// Entry is one complete logical line observed from a followed log file.
type Entry struct {
	Time    time.Time
	AppName string
	Stream  Stream
	Message string
}

// String formats an Entry for terminal or service output.
func (e Entry) String() string {
	return fmt.Sprintf("[%s] %s | %s",
		e.Time.Format(timestampLayout),
		e.AppName,
		e.Message,
	)
}

func newEntry(source Source, line string, observedAt time.Time) Entry {
	entry := Entry{
		Time:    observedAt,
		AppName: source.AppName,
		Stream:  source.Stream,
		Message: line,
	}

	const prefixLength = len("[2006-01-02 15:04:05] ")
	if len(line) < prefixLength || line[0] != '[' || line[20:22] != "] " {
		return entry
	}
	logTime, err := time.ParseInLocation(timestampLayout, line[1:20], time.Local)
	if err != nil {
		return entry
	}
	entry.Time = logTime
	entry.Message = strings.TrimSuffix(line[prefixLength:], "\r")
	return entry
}
