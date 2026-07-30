package logbrowser

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bizshuk/pm2/logfile"
	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
)

func loadApplications(socket string) tea.Cmd {
	return func() tea.Msg {
		response, err := model.SendRequest(socket, model.Request{Command: model.CmdList})
		if err != nil {
			return applicationsMsg{err: err}
		}
		if !response.OK {
			return applicationsMsg{err: errors.New(response.Error)}
		}
		var applications []process.ProcessInfo
		if err := json.Unmarshal(response.Payload, &applications); err != nil {
			return applicationsMsg{err: fmt.Errorf("decode process list: %w", err)}
		}
		return applicationsMsg{applications: applications}
	}
}

func loadFiles(application process.ProcessInfo) tea.Cmd {
	return func() tea.Msg {
		files, err := logfile.ListRelated(application.LogFile, application.ErrorFile)
		return filesMsg{files: files, err: err}
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
