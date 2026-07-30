package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/bizshuk/pm2/logfile"
	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
)

func streamLogs(
	ctx context.Context,
	socket string,
	target string,
	stdout io.Writer,
	stderr io.Writer,
) error {
	applications, err := loadLogApplications(socket)
	if err != nil {
		return err
	}
	sources, err := logSources(applications, target)
	if err != nil {
		return err
	}
	entries, followErrors := logfile.Follow(ctx, sources)
	return writeLogStream(ctx, entries, followErrors, stdout, stderr)
}

func loadLogApplications(socket string) ([]process.ProcessInfo, error) {
	response, err := model.SendRequest(socket, model.Request{Command: model.CmdList})
	if err != nil {
		return nil, fmt.Errorf("list applications for log stream: %w", err)
	}
	if response == nil {
		return nil, errors.New("list applications for log stream: empty response")
	}
	if !response.OK {
		return nil, fmt.Errorf("list applications for log stream: %s", response.Error)
	}

	var applications []process.ProcessInfo
	if err := json.Unmarshal(response.Payload, &applications); err != nil {
		return nil, fmt.Errorf("decode applications for log stream: %w", err)
	}
	return applications, nil
}

func logSources(applications []process.ProcessInfo, target string) ([]logfile.Source, error) {
	matched := matchLogApplications(applications, target)
	if len(matched) == 0 {
		if target == "" || target == "all" {
			return nil, errors.New("no managed applications to stream")
		}
		return nil, fmt.Errorf("no managed application matches %q", target)
	}

	sources := make([]logfile.Source, 0, len(matched)*2)
	for _, application := range matched {
		if application.LogFile != "" {
			sources = append(sources, logfile.Source{
				AppName: application.Name,
				Path:    application.LogFile,
				Stream:  logfile.StreamStdout,
			})
		}
		if application.ErrorFile != "" {
			sources = append(sources, logfile.Source{
				AppName: application.Name,
				Path:    application.ErrorFile,
				Stream:  logfile.StreamStderr,
			})
		}
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("matched application %q has no configured log paths", target)
	}
	return sources, nil
}

func matchLogApplications(
	applications []process.ProcessInfo,
	target string,
) []process.ProcessInfo {
	if target == "" || target == "all" {
		return applications
	}

	for _, application := range applications {
		namespace := application.Namespace
		if namespace == "" {
			namespace = process.DefaultNamespace
		}
		if namespace+":"+application.Name == target {
			return []process.ProcessInfo{application}
		}
	}

	if id, err := strconv.Atoi(target); err == nil {
		for _, application := range applications {
			if application.ID == id {
				return []process.ProcessInfo{application}
			}
		}
	}

	matched := make([]process.ProcessInfo, 0)
	for _, application := range applications {
		if application.Name == target {
			matched = append(matched, application)
		}
	}
	if len(matched) > 0 {
		return matched
	}

	for _, application := range applications {
		namespace := application.Namespace
		if namespace == "" {
			namespace = process.DefaultNamespace
		}
		if namespace == target {
			matched = append(matched, application)
		}
	}
	return matched
}

func writeLogStream(
	ctx context.Context,
	entries <-chan logfile.Entry,
	followErrors <-chan error,
	stdout io.Writer,
	stderr io.Writer,
) error {
	for entries != nil || followErrors != nil {
		select {
		case <-ctx.Done():
			return nil
		case entry, ok := <-entries:
			if !ok {
				entries = nil
				continue
			}
			output := stdout
			if entry.Stream == logfile.StreamStderr {
				output = stderr
			}
			if _, err := fmt.Fprintln(output, entry.String()); err != nil {
				return fmt.Errorf("write %s log stream: %w", entry.Stream, err)
			}
		case err, ok := <-followErrors:
			if !ok {
				followErrors = nil
				continue
			}
			return err
		}
	}
	return nil
}
