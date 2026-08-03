package views

import (
	"strings"
	"testing"

	"github.com/bizshuk/pm2/sysmon"
)

func systemFixture() sysmon.System {
	return sysmon.System{
		CPU:     sysmon.CPU{Cores: 10, UsedPercent: 24, UserPercent: 9, SysPercent: 15},
		Memory:  sysmon.Memory{TotalBytes: 16 << 30, UsedBytes: 15 << 30, UsedPercent: 93.7, AvailableBytes: 3 << 30, SwapTotalBytes: 8 << 30, SwapUsedBytes: 1 << 30},
		Load:    sysmon.Load{One: 3.1, Five: 5.9, Fifteen: 6.3},
		Network: sysmon.Network{Interface: "en0", RxBytesPerSecond: 1 << 20, TxBytesPerSecond: 1 << 18},
		DiskIO:  sysmon.DiskIO{BytesPerSecond: 8 << 20, TransfersPerSecond: 947},
		Disks:   []sysmon.Disk{{Mount: "/", TotalBytes: 228 << 30, UsedBytes: 11 << 30, UsedPercent: 5.1}},
	}
}

// plain strips ANSI styling so assertions describe layout, not colour.
func plain(text string) string {
	var out strings.Builder
	inEscape := false
	for _, r := range text {
		switch {
		case r == '\x1b':
			inEscape = true
		case inEscape && r == 'm':
			inEscape = false
		case !inEscape:
			out.WriteRune(r)
		}
	}
	return out.String()
}

func TestRenderHostPanelHasOneLinePerResource(t *testing.T) {
	lines := strings.Split(plain(RenderHostPanel(systemFixture(), 120)), "\n")

	if len(lines) != dashboardHostRows {
		t.Fatalf("got %d lines, want %d — the layout reserves a fixed panel height", len(lines), dashboardHostRows)
	}
	for index, want := range []string{"cpu", "mem", "net", "disk"} {
		if !strings.Contains(lines[index], want) {
			t.Errorf("line %d = %q, want it to lead with %q", index, lines[index], want)
		}
	}
	if !strings.Contains(lines[1], "3.0gb available") {
		t.Errorf("memory line = %q, want the available-bytes figure beside the percentage", lines[1])
	}
}

func TestHostPanelShowsCombinedDiskIOWithoutASplit(t *testing.T) {
	// macOS reports one combined throughput figure. Drawing separate
	// read and write arrows would invent a split that does not exist.
	lines := strings.Split(plain(RenderHostPanel(systemFixture(), 120)), "\n")
	if !strings.Contains(lines[3], "⇅") {
		t.Errorf("disk line = %q, want the combined-throughput glyph", lines[3])
	}

	split := systemFixture()
	split.DiskIO = sysmon.DiskIO{ReadWriteSplit: true, ReadBytesPerSecond: 1 << 20, WriteBytesPerSecond: 1 << 19}
	lines = strings.Split(plain(RenderHostPanel(split, 120)), "\n")
	if !strings.Contains(lines[3], "⇣") || !strings.Contains(lines[3], "⇡") {
		t.Errorf("disk line = %q, want separate read and write arrows where the platform reports them", lines[3])
	}
}

func TestGaugeFillsProportionally(t *testing.T) {
	cases := map[float64]int{0: 0, 50: 5, 100: 10, -5: 0, 150: 10}
	for percent, wantFilled := range cases {
		got := strings.Count(plain(gauge(percent, 10)), "█")
		if got != wantFilled {
			t.Errorf("gauge(%v) filled %d cells, want %d", percent, got, wantFilled)
		}
		if width := screen.StringWidth(plain(gauge(percent, 10))); width != 10 {
			t.Errorf("gauge(%v) rendered %d columns, want a fixed 10", percent, width)
		}
	}
}

func dashboardFixture() DashboardContext {
	task := sysmon.Task{
		ID: 1, Namespace: "Service", Name: "api", Status: "online", PID: 1978,
		CPUPercent: 2, MemoryBytes: 1 << 20, TreeCPUPercent: 20, TreeMemoryBytes: 4 << 20,
		Command:  "/srv/run.sh",
		Children: []sysmon.Proc{{PID: 2001, PPID: 1978, CPUPercent: 18, MemoryBytes: 3 << 20, Command: "/bin/worker"}},
		Ports:    []sysmon.Port{{PID: 2001, Protocol: "tcp", Address: "0.0.0.0", Port: 8080, State: "LISTEN"}},
	}
	return DashboardContext{
		Width:  120,
		Height: 30,
		Scope:  ScopeTasks,
		SortBy: "cpu",
		Snapshot: sysmon.Snapshot{
			System: systemFixture(),
			Host:   sysmon.Host{Hostname: "box", Cores: 10},
			Tasks:  []sysmon.Task{task},
		},
		Task:     &task,
		Children: task.Children,
		Ports:    task.Ports,
	}
}

func TestRenderDashboardFitsTheTerminal(t *testing.T) {
	// A frame wider than the terminal wraps and destroys the layout; one
	// taller than it scrolls the header off the top.
	ctx := dashboardFixture()
	lines := strings.Split(plain(RenderDashboard(ctx)), "\n")

	if len(lines) != ctx.Height {
		t.Errorf("frame is %d lines, want exactly %d", len(lines), ctx.Height)
	}
	for index, line := range lines {
		if width := screen.StringWidth(line); width > ctx.Width {
			t.Errorf("line %d is %d columns wide, want at most %d:\n%s", index, width, ctx.Width, line)
		}
	}
}

func TestRenderDashboardListRowsStayOnOneLine(t *testing.T) {
	ctx := dashboardFixture()
	width := dashboardListWidth(ctx.Width)

	rows := dashboardRows(ctx, width)
	for index, row := range rows {
		if strings.Contains(row, "\n") {
			t.Errorf("row %d wrapped onto a second line, which silently halves the visible list:\n%q", index, row)
		}
		if got := screen.StringWidth(plain(row)); got > width {
			t.Errorf("row %d is %d columns, want at most %d", index, got, width)
		}
	}
}

func TestRenderDashboardDetailShowsTreeAndPorts(t *testing.T) {
	ctx := dashboardFixture()
	detail := plain(RenderDashboardDetail(ctx, 70, 30))

	for _, want := range []string{"SERVICE:API", "tree 20.0%", "SUB-PROCESSES (1)", "/bin/worker", "LISTENING PORTS (1)", "0.0.0.0:8080"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail is missing %q\n%s", want, detail)
		}
	}
}

func TestRenderDashboardDetailWithNoSelection(t *testing.T) {
	ctx := dashboardFixture()
	ctx.Task, ctx.Children, ctx.Ports = nil, nil, nil
	if got := plain(RenderDashboardDetail(ctx, 70, 30)); !strings.Contains(got, "nothing selected") {
		t.Errorf("detail = %q, want the empty-selection notice", got)
	}
}

func TestRenderDashboardDetailReportsEmptyTreeExplicitly(t *testing.T) {
	// "none" is a finding; a blank section reads like a rendering bug.
	ctx := dashboardFixture()
	ctx.Children, ctx.Ports = nil, nil
	detail := plain(RenderDashboardDetail(ctx, 70, 30))

	if !strings.Contains(detail, "SUB-PROCESSES (0)") || !strings.Contains(detail, "LISTENING PORTS (0)") {
		t.Errorf("detail should still show both sections with a zero count\n%s", detail)
	}
	if strings.Count(detail, "none") != 2 {
		t.Errorf("detail should say \"none\" under each empty section\n%s", detail)
	}
}

func TestScrollWindowKeepsTheCursorVisible(t *testing.T) {
	// A 600-process table must not be handed to lipgloss in full and left
	// to clip whichever end it likes.
	rows := make([]string, 100)
	for index := range rows {
		rows[index] = "row"
	}

	if got := len(scrollWindow(rows, 50, 10)); got != 10 {
		t.Errorf("window is %d rows, want 10", got)
	}
	if got := len(scrollWindow(rows[:5], 0, 10)); got != 5 {
		t.Errorf("short list = %d rows, want all 5 unpadded", got)
	}

	rows[0], rows[99] = "first", "last"
	if window := scrollWindow(rows, 0, 10); window[0] != "first" {
		t.Error("window did not clamp to the start of the list")
	}
	if window := scrollWindow(rows, 99, 10); window[len(window)-1] != "last" {
		t.Error("window did not clamp to the end of the list")
	}
}

func TestFormatUptimeSeconds(t *testing.T) {
	cases := map[int64]string{
		0:      "—",
		90:     "1m",
		3700:   "1h 1m",
		90000:  "1d 1h",
		180000: "2d 2h",
	}
	for seconds, want := range cases {
		if got := formatUptimeSeconds(seconds); got != want {
			t.Errorf("formatUptimeSeconds(%d) = %q, want %q", seconds, got, want)
		}
	}
}

func TestFormatRate(t *testing.T) {
	if got := formatRate(1 << 20); got != "1.0mb/s" {
		t.Errorf("formatRate(1MiB) = %q, want 1.0mb/s", got)
	}
	if got := formatRate(-5); got != "0b/s" {
		t.Errorf("formatRate(-5) = %q, want 0b/s", got)
	}
}
