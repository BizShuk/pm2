package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/shuk/pm2/daemon"
	"github.com/shuk/pm2/process"
	"github.com/spf13/cobra"
)

func newLogsCmd() *cobra.Command {
	var lines int
	cmd := &cobra.Command{
		Use:   "logs [name]",
		Short: "Tail process logs",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := daemon.SendRequest(socketPath(), daemon.Request{Command: daemon.CmdList})
			if err != nil {
				return err
			}
			var infos []process.ProcessInfo
			if err := json.Unmarshal(resp.Payload, &infos); err != nil {
				return err
			}

			filterName := ""
			if len(args) > 0 {
				filterName = args[0]
			}

			var matchedProcs []process.ProcessInfo
			if filterName == "" {
				matchedProcs = infos
			} else {
				// 1. 嘗試 ID 匹配
				var idVal int
				isID := false
				if _, err := fmt.Sscan(filterName, &idVal); err == nil {
					isID = true
				}
				if isID {
					for _, p := range infos {
						if p.ID == idVal {
							matchedProcs = append(matchedProcs, p)
						}
					}
				}
				// 2. 若非 ID，嘗試 Name 匹配
				if len(matchedProcs) == 0 {
					for _, p := range infos {
						if p.Name == filterName {
							matchedProcs = append(matchedProcs, p)
						}
					}
				}
				// 3. 若非 Name，嘗試 Namespace 匹配
				if len(matchedProcs) == 0 {
					for _, p := range infos {
						if p.Namespace == filterName {
							matchedProcs = append(matchedProcs, p)
						}
					}
				}
			}

			var logFiles []string
			for _, p := range matchedProcs {
				if p.LogFile != "" {
					logFiles = append(logFiles, p.LogFile)
				}
				if p.ErrorFile != "" {
					logFiles = append(logFiles, p.ErrorFile)
				}
			}

			if len(logFiles) == 0 {
				fmt.Println("No log files found.")
				return nil
			}

			// Tail all matching log files
			for _, lf := range logFiles {
				fmt.Printf("==> %s <==\n", lf)
				if err := tailFile(lf, lines); err != nil {
					fmt.Fprintf(os.Stderr, "tail %s: %v\n", lf, err)
				}
			}

			// Follow the first log file if -f is desired (simple implementation)
			fmt.Println("\n[Press Ctrl+C to exit]")
			if len(logFiles) > 0 {
				followFile(logFiles[0])
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&lines, "lines", "n", 20, "number of lines to show")
	return cmd
}

func tailFile(path string, n int) error {
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	var allLines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		allLines = append(allLines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	start := len(allLines) - n
	if start < 0 {
		start = 0
	}
	for _, l := range allLines[start:] {
		fmt.Println(l)
	}
	return nil
}

func followFile(path string) {
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Seek(0, io.SeekEnd)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fmt.Println(scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		// Ignore or log error for followFile since it doesn't return an error
	}
}
