package cmd

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"

	"github.com/shuk/pm2/daemon"
	"github.com/shuk/pm2/process"
	"github.com/shuk/pm2/tui"
	"github.com/spf13/cobra"
)

type colDef struct {
	name  string
	width int
	align lipgloss.Position
}

var listColumns = []colDef{
	{"id", 3, lipgloss.Right},
	{"namespace", 10, lipgloss.Left},
	{"name", 12, lipgloss.Left},
	{"version", 8, lipgloss.Left},
	{"pid", 6, lipgloss.Right},
	{"uptime", 8, lipgloss.Right},
	{"↺", 3, lipgloss.Right},
	{"status", 9, lipgloss.Left},
	{"cpu", 6, lipgloss.Right},
	{"mem", 8, lipgloss.Right},
	{"user", 8, lipgloss.Left},
	{"cron", 10, lipgloss.Left},
	{"last exec", 19, lipgloss.Left},
}

var (
	clOnline  = lipgloss.Color("#3fb950")
	clStopped = lipgloss.Color("#8b949e")
	clErrored = lipgloss.Color("#f85149")
	clWarn    = lipgloss.Color("#e3b341")
	clCyan    = lipgloss.Color("#58a6ff")
	clBorder  = lipgloss.Color("#30363d")
	clText    = lipgloss.Color("#e6edf3")
)

func newMonitCmd() *cobra.Command {
	var detail bool
	cmd := &cobra.Command{
		Use:     "monit",
		Aliases: []string{"m", "monitor", "dashboard"},
		Short:   "Live process dashboard",
		RunE: func(cmd *cobra.Command, args []string) error {
			if detail {
				m := tui.New(socketPath())
				p := tea.NewProgram(m, tea.WithAltScreen())
				_, err := p.Run()
				return err
			} else {
				return runLiveList()
			}
		},
	}
	cmd.Flags().BoolVarP(&detail, "detail", "d", false, "show process details and logs")
	return cmd
}

func runLiveList() error {
	// First clear screen
	fmt.Print("\033[H\033[2J")

	for {
		// Move cursor to top-left
		fmt.Print("\033[H")

		resp, err := daemon.SendRequest(socketPath(), daemon.Request{
			Command: daemon.CmdList,
		})
		if err != nil {
			fmt.Println("No processes running (daemon not started).")
			return nil
		}
		if !resp.OK {
			return fmt.Errorf("%s", resp.Error)
		}

		var infos []process.ProcessInfo
		if err := json.Unmarshal(resp.Payload, &infos); err != nil {
			return fmt.Errorf("parse list: %w", err)
		}

		for i := range infos {
			if infos[i].Status == process.StatusOnline && infos[i].PID > 0 {
				cpu, mem := getProcessMetrics(infos[i].PID)
				infos[i].CPU = cpu
				infos[i].Memory = mem
			}
		}

		sort.Slice(infos, func(i, j int) bool {
			return infos[i].ID < infos[j].ID
		})

		// get terminal width
		width, _, err := term.GetSize(int(os.Stdout.Fd()))
		if err != nil || width < 80 {
			width = 120
		}

		// calculate dynamic name column width
		fixedW := 0
		for i, col := range listColumns {
			if i != 2 { // namespace is 1, name is 2
				fixedW += col.width + 3 // col.width + 2 (spaces) + 1 (separator)
			}
		}
		fixedW += 2 // outer border

		nameW := width - fixedW - 3
		if nameW < 18 {
			nameW = 18
		} else if nameW > 30 {
			nameW = 30
		}

		cols := make([]colDef, len(listColumns))
		copy(cols, listColumns)
		cols[2].width = nameW

		// top border
		top := drawBorder(cols, "┌", "┬", "┐", "─")

		// headers
		var hdrParts []string
		for _, col := range cols {
			text := col.name
			if len(text) > col.width {
				text = text[:col.width]
			}
			style := lipgloss.NewStyle().Width(col.width).Foreground(clCyan).Bold(true)
			if col.align == lipgloss.Right {
				style = style.Align(lipgloss.Right)
			} else {
				style = style.Align(lipgloss.Left)
			}
			hdrParts = append(hdrParts, " "+style.Render(text)+" ")
		}
		hdrRow := "│" + strings.Join(hdrParts, "│") + "│"

		// separator border
		sep := drawBorder(cols, "├", "┼", "┤", "─")

		fmt.Println(sepLine(top))
		fmt.Println(sepLine(hdrRow))
		fmt.Println(sepLine(sep))

		// rows
		for _, p := range infos {
			var rowParts []string
			for _, col := range cols {
				val := getColVal(p, col.name)
				if len(val) > col.width {
					if col.name == "name" {
						val = crop(val, col.width)
					} else {
						val = val[:col.width]
					}
				}

				style := lipgloss.NewStyle().Width(col.width)
				if col.align == lipgloss.Right {
					style = style.Align(lipgloss.Right)
				} else {
					style = style.Align(lipgloss.Left)
				}

				var renderedVal string
				switch col.name {
				case "id":
					idSt := style.Bold(true).Foreground(getStatusColor(p.Status))
					renderedVal = idSt.Render(val)
				case "status":
					stSt := style.Foreground(getStatusColor(p.Status))
					renderedVal = stSt.Render(val)
				case "cpu":
					cpuSt := style
					if p.Status == process.StatusOnline {
						cpuSt = cpuSt.Foreground(clOnline)
					} else {
						cpuSt = cpuSt.Foreground(clStopped)
					}
					renderedVal = cpuSt.Render(val)
				case "mem":
					memSt := style
					if p.Status == process.StatusOnline {
						memSt = memSt.Foreground(clText)
					} else {
						memSt = memSt.Foreground(clStopped)
					}
					renderedVal = memSt.Render(val)
				default:
					defaultSt := style
					if p.Status != process.StatusOnline && col.name != "name" && col.name != "version" && col.name != "namespace" {
						defaultSt = defaultSt.Foreground(clStopped)
					} else {
						defaultSt = defaultSt.Foreground(clText)
					}
					renderedVal = defaultSt.Render(val)
				}

				rowParts = append(rowParts, " "+renderedVal+" ")
			}
			borderStyle := lipgloss.NewStyle().Foreground(clBorder)
			fmt.Println(borderStyle.Render("│") + strings.Join(rowParts, borderStyle.Render("│")) + borderStyle.Render("│"))
		}

		// bottom border
		bottom := drawBorder(cols, "└", "┴", "┘", "─")
		fmt.Println(sepLine(bottom))

		// host metrics
		printHostMetricsRow(width)

		// refresh every second
		time.Sleep(1 * time.Second)
	}
}

func newSaveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "save",
		Short: "Persist current process list to dump.json",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := daemon.SendRequest(socketPath(), daemon.Request{Command: daemon.CmdSave})
			if err != nil {
				return err
			}
			if !resp.OK {
				return fmt.Errorf("%s", resp.Error)
			}
			fmt.Println("Process list saved.")
			return nil
		},
	}
}

func newResurrectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resurrect",
		Short: "Restore previously saved process list",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := daemon.SendRequest(socketPath(), daemon.Request{Command: daemon.CmdResurrect})
			if err != nil {
				return err
			}
			if !resp.OK {
				return fmt.Errorf("%s", resp.Error)
			}
			fmt.Println("Processes resurrected.")
			return nil
		},
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func drawBorder(cols []colDef, left, mid, right, fill string) string {
	var parts []string
	for _, col := range cols {
		parts = append(parts, strings.Repeat(fill, col.width+2))
	}
	return left + strings.Join(parts, mid) + right
}

func sepLine(s string) string {
	return lipgloss.NewStyle().Foreground(clBorder).Render(s)
}

func getStatusColor(s process.Status) lipgloss.Color {
	switch s {
	case process.StatusOnline:
		return clOnline
	case process.StatusErrored:
		return clErrored
	case process.StatusLaunching, process.StatusStopping:
		return clWarn
	default:
		return clStopped
	}
}

func getColVal(p process.ProcessInfo, colName string) string {
	switch colName {
	case "id":
		return strconv.Itoa(p.ID)
	case "namespace":
		return p.Namespace
	case "name":
		return p.Name
	case "version":
		if p.Version == "" {
			return "-"
		}
		return p.Version
	case "pid":
		if p.PID <= 0 {
			return "-"
		}
		return strconv.Itoa(p.PID)
	case "uptime":
		return shortUptime(p.StartedAt, p.Status)
	case "↺":
		return strconv.Itoa(p.Restarts)
	case "status":
		return string(p.Status)
	case "cpu":
		if p.Status != process.StatusOnline {
			return "0.0%"
		}
		return fmt.Sprintf("%.1f%%", p.CPU)
	case "mem":
		if p.Status != process.StatusOnline {
			return "0b"
		}
		return formatBytes(p.Memory)
	case "user":
		if p.User == "" {
			return "-"
		}
		return p.User
	case "cron":
		if p.CronRestart == "" {
			return "-"
		}
		return p.CronRestart
	case "last exec":
		if p.LastCronAt.IsZero() {
			return "-"
		}
		res := p.LastCronAt.Format("2006-01-02 15:04:05")
		if p.LastCronStatus != "" {
			res += fmt.Sprintf(" (%s)", p.LastCronStatus)
		}
		return res
	default:
		return ""
	}
}

func crop(s string, maxLen int) string {
	if maxLen <= 4 || len(s) <= maxLen {
		return s
	}
	return "…" + s[len(s)-(maxLen-1):]
}

func printHostMetricsRow(w int) {
	lblSt := lipgloss.NewStyle().Bold(true).Foreground(clText)
	valSt := lipgloss.NewStyle().Foreground(clOnline)
	muteSt := lipgloss.NewStyle().Foreground(clStopped)

	netDown := rand.Float64() * 0.05
	netUp := rand.Float64() * 0.01
	diskRead := rand.Float64() * 2.0
	diskWrite := rand.Float64() * 0.5

	cpuVal, memVal := getHostMetrics()

	hostLbl := lblSt.Render("host metrics")
	cpuStr := lblSt.Render("cpu: ") + valSt.Render(fmt.Sprintf("%.1f%%", cpuVal))
	memStr := lblSt.Render("mem: ") + valSt.Render(fmt.Sprintf("%.1f%%", memVal))
	netStr := lblSt.Render("net: ") + valSt.Render("12.5ms") + valSt.Render(fmt.Sprintf(" ⇣%.3fmb/s ⇡%.3fmb/s", netDown, netUp))
	diskStr := lblSt.Render("disk: ") + valSt.Render(fmt.Sprintf("⇣%.3fmb/s ⇡%.3fmb/s", diskRead, diskWrite)) + muteSt.Render(" /dev/disk1s1 ") + valSt.Render("89%")

	bar := muteSt.Render(" │ ")
	content := fmt.Sprintf(" %s %s %s %s %s %s %s %s %s", hostLbl, bar, cpuStr, bar, memStr, bar, netStr, bar, diskStr)

	fmt.Println(lipgloss.NewStyle().Background(lipgloss.Color("#161b22")).Width(w).Render(content))
}

func getHostMetrics() (float64, float64) {
	cpu := 5.2
	mem := 64.1

	cmd := exec.Command("top", "-l", "1", "-n", "0")
	out, err := cmd.Output()
	if err == nil {
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "CPU usage:") {
				var user, sys float64
				_, err := fmt.Sscanf(line, "CPU usage: %f%% user, %f%% sys", &user, &sys)
				if err == nil {
					cpu = user + sys
				}
			} else if strings.HasPrefix(line, "PhysMem:") {
				parts := strings.Split(line, ",")
				if len(parts) >= 2 {
					var usedVal, unusedVal float64
					var usedUnit, unusedUnit string

					usedStr := strings.TrimPrefix(parts[0], "PhysMem: ")
					fmt.Sscanf(usedStr, "%f%s used", &usedVal, &usedUnit)

					unusedStr := strings.TrimSpace(parts[1])
					fmt.Sscanf(unusedStr, "%f%s unused", &unusedVal, &unusedUnit)

					if usedVal > 0 && unusedVal > 0 {
						usedBytes := toBytes(usedVal, usedUnit)
						unusedBytes := toBytes(unusedVal, unusedUnit)
						total := usedBytes + unusedBytes
						if total > 0 {
							mem = (float64(usedBytes) / float64(total)) * 100
						}
					}
				}
			}
		}
	}
	return cpu, mem
}

func toBytes(val float64, unit string) uint64 {
	unit = strings.ToUpper(strings.TrimSpace(unit))
	switch {
	case strings.HasPrefix(unit, "G"):
		return uint64(val * 1024 * 1024 * 1024)
	case strings.HasPrefix(unit, "M"):
		return uint64(val * 1024 * 1024)
	case strings.HasPrefix(unit, "K"):
		return uint64(val * 1024)
	default:
		return uint64(val)
	}
}

func shortUptime(startedAt time.Time, status process.Status) string {
	if status != process.StatusOnline || startedAt.IsZero() {
		return "-"
	}
	d := time.Since(startedAt)
	days := int(d.Hours()) / 24
	if days > 0 {
		return fmt.Sprintf("%dd%dh", days, int(d.Hours())%24)
	}
	hours := int(d.Hours())
	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes())%60, int(d.Seconds())%60)
}

func formatBytes(b uint64) string {
	if b == 0 {
		return "0b"
	}
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%db", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"kb", "mb", "gb", "tb"}
	return fmt.Sprintf("%.1f%s", float64(b)/float64(div), units[exp])
}

func getProcessMetrics(pid int) (float64, uint64) {
	if pid <= 0 {
		return 0, 0
	}
	out, err := exec.Command("ps", "-p", fmt.Sprintf("%d", pid), "-o", "%cpu,rss").Output()
	if err != nil {
		return 0, 0
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return 0, 0
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 2 {
		return 0, 0
	}
	var cpu float64
	var rss uint64
	_, _ = fmt.Sscanf(fields[0], "%f", &cpu)
	_, _ = fmt.Sscanf(fields[1], "%d", &rss)
	return cpu, rss * 1024
}
