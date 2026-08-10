package logbrowser

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bizshuk/pm2/logfile"
)

func loadApps(root string) tea.Cmd {
	return func() tea.Msg {
		apps, err := logfile.ListApps(root)
		return appsMsg{apps: apps, err: err}
	}
}

func loadFile(path string) tea.Cmd {
	return func() tea.Msg {
		lines, err := readFileLines(path)
		return fileMsg{path: path, lines: lines, err: err}
	}
}

func deleteFile(path string) tea.Cmd {
	return func() tea.Msg {
		err := os.Remove(path)
		if err != nil {
			err = fmt.Errorf("delete log file %q: %w", path, err)
		}
		return deletedMsg{path: path, err: err}
	}
}

func readFileLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open log file %q: %w", path, err)
	}
	// A close failure on this read-only descriptor cannot invalidate bytes
	// already copied into the returned strings.
	defer func() { _ = file.Close() }()

	reader := bufio.NewReader(file)
	var lines []string
	for {
		line, readErr := reader.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimSuffix(line, "\n")
			line = strings.TrimSuffix(line, "\r")
			lines = append(lines, line)
		}
		switch {
		case readErr == nil:
		case errors.Is(readErr, io.EOF):
			return lines, nil
		default:
			return nil, fmt.Errorf("read log file %q: %w", path, readErr)
		}
	}
}
