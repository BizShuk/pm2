# Log Rotation and TUI Browser Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `executing-plans` to
> implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** 為 managed CLI output 加上逐行日期時間、每天將舊內容輪替為
`<base>.<YYYY-MM-DD>.<ext>`，並將 `pm2 logs` 改為可瀏覽與確認刪除檔案的
interactive TUI。

**Architecture:** 新增獨立 `logfile` package，單一擁有 timestamp writer、
rotation 與 related-file discovery。`daemon/executor` 僅注入 writer；
`tui/logbrowser` 擁有 application/file/viewer/delete-confirm state machine，
純 rendering 仍由 `tui/views` 擁有，`cmd/logs.go` 維持薄 Cobra shell。

**Tech Stack:** Go 1.26.3、stdlib filesystem/I/O、Cobra、Bubble Tea、
既有 `process.ProcessInfo` 與 Unix socket RPC。

**Status:** Completed and verified on 2026-07-30.

## Global Constraints

- 使用繁體中文 + English Terminology。
- one file one responsibility；one package/folder one domain。
- `daemon.log` / `daemon.err` 只保留最新日期，歷史檔名為
  `daemon.<YYYY-MM-DD>.log` / `daemon.<YYYY-MM-DD>.err`。
- 每個 child stdout/stderr logical line 使用
  `[YYYY-MM-DD HH:MM:SS] ` prefix。
- `pm2 logs` flow 為 application list → log file list → log viewer。
- `d` 刪除前必須進入 explicit `y/N` confirmation。
- 保留使用者既有 `go.mod` / `go.sum` 變更，不建立 compatibility wrapper。

---

### Task 1: Daily Log Rotation

**Files:**

- Create: `logfile/rotation.go`
- Create: `logfile/rotation_test.go`

**Interfaces:**

- Produces:
  `func Rotate(path string, now time.Time) error`
  and
  `func ArchivePath(path, date string) string`

- [x] **Step 1: Write failing rotation tests**

```go
func TestRotateMovesEveryLeadingPreviousDateBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.log")
	writeFile(t, path,
		"[2026-07-27 23:59:59] first\n"+
			"[2026-07-28 00:00:00] second\n"+
			"[2026-07-29 00:00:00] latest\n")

	require.NoError(t, Rotate(path, time.Date(2026, 7, 29, 12, 0, 0, 0, time.Local)))
	require.Equal(t, "[2026-07-29 00:00:00] latest\n", readFile(t, path))
	require.Equal(t, "[2026-07-27 23:59:59] first\n",
		readFile(t, filepath.Join(filepath.Dir(path), "daemon.2026-07-27.log")))
	require.Equal(t, "[2026-07-28 00:00:00] second\n",
		readFile(t, filepath.Join(filepath.Dir(path), "daemon.2026-07-28.log")))
}
```

另以獨立 tests 驗證：同日不輪替、legacy first line 不輪替、`.err` 命名、
existing archive 採 append、沒有 trailing newline 時不遺失 bytes。

- [x] **Step 2: Verify RED**

Run: `go test ./logfile -run 'TestRotate|TestArchivePath' -count=1`

Expected: FAIL because package/functions do not exist.

- [x] **Step 3: Implement prefix-block rotation**

```go
func ArchivePath(path, date string) string {
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	return base + "." + date + ext
}

func Rotate(path string, now time.Time) error {
	// Read lines in order, archive only consecutive leading date blocks
	// whose parsed date is before local today, append each block to its
	// date archive, and atomically replace path with the remaining bytes.
}
```

Implementation 使用同目錄 temporary file 保存 remaining bytes；任何
unparseable line 跟隨目前 block，遇到第一個 today/future block 後停止
rotation，後續 bytes 原樣保留。

- [x] **Step 4: Verify GREEN**

Run: `go test ./logfile -run 'TestRotate|TestArchivePath' -count=1`

Expected: PASS.

### Task 2: Timestamped Reopening Writer

**Files:**

- Create: `logfile/writer.go`
- Create: `logfile/writer_test.go`

**Interfaces:**

- Consumes: `Rotate(path string, now time.Time) error`
- Produces:
  `func Open(path string) (*Writer, error)`;
  `(*Writer).Write([]byte) (int, error)`;
  `(*Writer).Close() error`

- [x] **Step 1: Write failing writer tests**

```go
func TestWriterPrefixesEveryLogicalLineAcrossChunkBoundaries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.log")
	w, err := openWithClock(path, fixedClock("2026-07-30T08:09:10Z"))
	require.NoError(t, err)
	_, err = w.Write([]byte("one\ntw"))
	require.NoError(t, err)
	_, err = w.Write([]byte("o\n"))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	require.Equal(t,
		"[2026-07-30 08:09:10] one\n[2026-07-30 08:09:10] two\n",
		readFile(t, path))
}
```

另以獨立 tests 驗證：open 時輪替多日、midnight 下一行觸發輪替、final
partial line 仍有 timestamp、current path 被刪除後下一行自動 reopen。

- [x] **Step 2: Verify RED**

Run: `go test ./logfile -run 'TestWriter' -count=1`

Expected: FAIL because `Writer` does not exist.

- [x] **Step 3: Implement writer**

```go
type Writer struct {
	mu          sync.Mutex
	path        string
	file        *os.File
	now         func() time.Time
	atLineStart bool
	activeDate  string
}

func Open(path string) (*Writer, error) {
	return openWithClock(path, time.Now)
}
```

`Write` 在每個 logical line 開頭取得一次 clock、確認日期與 current path，
必要時 close → Rotate → reopen，再寫 `[YYYY-MM-DD HH:MM:SS] `。成功時
回傳 input bytes 的長度，而不是包含 prefix 的 output bytes。

- [x] **Step 4: Verify GREEN**

Run: `go test ./logfile -count=1`

Expected: PASS.

### Task 3: Executor Integration

**Files:**

- Modify: `daemon/executor/executor.go`
- Create: `daemon/executor/executor_log_test.go`

**Interfaces:**

- Consumes: `logfile.Open`
- Changes:
  `StartResult.OutF` and `StartResult.ErrF` from `*os.File` to
  `io.WriteCloser`; `Executor.Watch` accepts `io.Closer` for both streams.

- [x] **Step 1: Write failing end-to-end executor test**

```go
func TestStartTimestampsAndRotatesManagedOutput(t *testing.T) {
	// Seed daemon.log with two previous dates, start a shell command that
	// emits two stdout lines and one stderr line, wait through Watch, then
	// assert dated archives and timestamped current daemon.log/daemon.err.
}
```

- [x] **Step 2: Verify RED**

Run:
`go test ./daemon/executor -run TestStartTimestampsAndRotatesManagedOutput -count=1`

Expected: FAIL because current executor writes raw child output.

- [x] **Step 3: Inject logfile.Writer**

```go
outF, err := logfile.Open(logFile)
if err != nil {
	return nil, fmt.Errorf("open stdout log: %w", err)
}
errF, err := logfile.Open(errFile)
if err != nil {
	_ = outF.Close()
	return nil, fmt.Errorf("open stderr log: %w", err)
}
cmd.Stdout = outF
cmd.Stderr = errF
```

- [x] **Step 4: Verify GREEN and daemon regression scope**

Run:
`go test ./daemon/executor ./daemon -run 'TestStartTimestampsAndRotatesManagedOutput|TestStartApp|TestProcess' -count=1`

Expected: PASS.

### Task 4: Related Log File Discovery

**Files:**

- Create: `logfile/files.go`
- Create: `logfile/files_test.go`

**Interfaces:**

- Produces:
  `type FileInfo struct { Path string; Name string; Size int64; ModTime time.Time; Current bool }`
  and
  `func ListRelated(currentPaths ...string) ([]FileInfo, error)`

- [x] **Step 1: Write failing discovery tests**

```go
func TestListRelatedIncludesCurrentAndDatedArchivesOnly(t *testing.T) {
	// Create daemon.log, daemon.err, both dated archives, and unrelated.txt.
	// Assert four related files, no unrelated file, deduplication, and
	// current files before archives ordered newest-first.
}
```

- [x] **Step 2: Verify RED**

Run: `go test ./logfile -run TestListRelated -count=1`

Expected: FAIL because discovery API does not exist.

- [x] **Step 3: Implement exact basename/date matching**

```go
type FileInfo struct {
	Path    string
	Name    string
	Size    int64
	ModTime time.Time
	Current bool
}
```

每個 current path 只接受 exact basename 或
`<stem>.<YYYY-MM-DD><ext>`，並以 absolute cleaned path deduplicate。

- [x] **Step 4: Verify GREEN**

Run: `go test ./logfile -count=1`

Expected: PASS.

### Task 5: Log Browser State Machine and Views

**Files:**

- Create: `tui/logbrowser/model.go`
- Create: `tui/logbrowser/commands.go`
- Create: `tui/logbrowser/keys.go`
- Create: `tui/logbrowser/model_test.go`
- Create: `tui/views/log_browser.go`
- Create: `tui/views/log_browser_test.go`

**Interfaces:**

- Consumes: `model.CmdList`, `process.ProcessInfo`, `logfile.ListRelated`
- Produces:
  `func New(socket, initialTarget string) Model`
  and `func RenderLogBrowser(LogBrowserContext) string`

- [x] **Step 1: Write failing navigation and confirmation tests**

```go
func TestApplicationToFilesToViewerAndBack(t *testing.T) {
	m := modelWithApplications(testProcesses())
	m = updateKey(t, m, "enter")
	require.Equal(t, screenFiles, m.screen)
	m = updateKey(t, m, "enter")
	require.Equal(t, screenViewer, m.screen)
	m = updateKey(t, m, "down")
	require.Equal(t, 1, m.lineCursor)
	m = updateKey(t, m, "esc")
	require.Equal(t, screenFiles, m.screen)
}

func TestDeleteRequiresExplicitYes(t *testing.T) {
	m := modelWithFiles(testFiles())
	m = updateKey(t, m, "d")
	require.Equal(t, screenConfirmDelete, m.screen)
	m = updateKey(t, m, "n")
	require.FileExists(t, testFiles()[0].Path)
	m = updateKey(t, m, "d")
	m, cmd := updateKeyWithCmd(t, m, "y")
	require.NotNil(t, cmd)
}
```

另測 `j/k` 與 arrows、empty states、viewer viewport clamp、delete failure
notice、current file delete 後回到 refreshed file list。

- [x] **Step 2: Verify RED**

Run: `go test ./tui/logbrowser ./tui/views -run 'TestApplication|TestDelete|TestLogBrowser' -count=1`

Expected: FAIL because log browser package/view do not exist.

- [x] **Step 3: Implement the state machine**

```go
type screen uint8

const (
	screenApplications screen = iota
	screenFiles
	screenViewer
	screenConfirmDelete
)
```

`Enter` 前進、`Esc` 回上一層、`up/down/j/k` 移動 selected row 或 viewer
line cursor、`d` 只在 file list/viewer 進入 confirmation、只有 `y` 執行
`os.Remove` command，其他鍵取消或停留。

- [x] **Step 4: Implement pure view**

```go
type LogBrowserContext struct {
	Title        string
	Items        []string
	Selected     int
	Lines        []string
	LineCursor   int
	ConfirmPath  string
	Notice       string
	Err          error
	Width, Height int
}
```

Renderer 顯示 current breadcrumb、selected marker、viewer visible window、
`d delete` hint，以及 `Delete <path>? [y/N]` confirmation。

- [x] **Step 5: Verify GREEN**

Run: `go test ./tui/logbrowser ./tui/views -count=1`

Expected: PASS.

### Task 6: Cobra Wiring, Documentation, and Completion Audit

**Files:**

- Modify: `cmd/logs.go`
- Modify: `cmd/root_test.go`
- Modify: `README.md`
- Modify: `CLAUDE.md`
- Modify: `README.todo`

**Interfaces:**

- Consumes: `logbrowser.New`
- Keeps: root command name `pm2 logs [name|id|namespace]`
- Removes: direct-tail `--lines` behavior and in-command filesystem reader

- [x] **Step 1: Write failing Cobra contract test**

```go
func TestLogsCommandIsInteractiveBrowser(t *testing.T) {
	require.Equal(t, "logs [name]", LogsCmd.Use)
	require.Nil(t, LogsCmd.Flags().Lookup("lines"))
	require.Contains(t, LogsCmd.Short, "browser")
}
```

- [x] **Step 2: Verify RED**

Run: `go test ./cmd -run TestLogsCommandIsInteractiveBrowser -count=1`

Expected: FAIL because `--lines` and direct-tail copy still exist.

- [x] **Step 3: Replace cmd shell**

```go
var LogsCmd = &cobra.Command{
	Use:   "logs [name]",
	Short: "Browse and manage process log files",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := ""
		if len(args) == 1 {
			target = args[0]
		}
		_, err := tea.NewProgram(
			logbrowser.New(cliruntime.SocketPath(), target),
			tea.WithAltScreen(),
		).Run()
		return err
	},
}
```

- [x] **Step 4: Update canonical docs**

README 業務使用方式改為 interactive flow；CLAUDE 新增 `logfile/` ownership、
`tui/logbrowser/` state ownership 與 rotation invariant；README.todo 新增本
feature 的 completed result 與 exact verification commands。

- [x] **Step 5: Run fresh full verification**

Run:

```bash
gofmt -w logfile daemon/executor tui/logbrowser tui/views cmd
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
go build ./...
git diff --check
```

Expected: every command exits 0.

- [x] **Step 6: Runtime smoke audit**

在 isolated temporary PM2 home 啟動會輸出 stdout/stderr 的 short-lived app，
確認 current files 每行 timestamp、seeded old dates 被拆到 dated archives；
以 PTY 啟動 `pm2 logs`，逐一操作 application → file → viewer、上下移動、`d`
後先 `n` 保留，再 `d` + `y` 刪除。

Expected: 四項 user requirements 都有 direct runtime evidence，且不寫入 real
`~/.pm2` / app config directory。
