# 替換 expandHome 為 gosdk 家目錄展開規範實作計畫 (Replace expandHome with gosdk path expansion standard)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 將 `daemon/helpers.go` 中的自訂 `expandHome` 函數移除，並改為符合 `gosdk` 規範的 `github.com/mitchellh/go-homedir` inline 展開。

**Architecture:** 移除 `daemon/helpers.go` 中的 `expandHome`，在 `daemon/server.go` 中直接使用 `homedir.Expand` 進行 inline 家目錄路徑展開 (Home Path Expansion)，並增加對應的單元測試進行驗證。

**Tech Stack:** Go 1.26+, `github.com/mitchellh/go-homedir`

---

## Proposed Changes

### Task 1: 新增與安裝依賴 (Add and Install Dependencies)

**Files:**
- Modify: [go.mod](file:///Users/shuk/projects/tmp/pm2/go.mod)

- [ ] **Step 1: 執行 go get 下載 github.com/mitchellh/go-homedir**
  
  在專案根目錄下執行：
  ```bash
  go get github.com/mitchellh/go-homedir
  ```
  Expected: 成功下載並將依賴寫入 `go.mod` 與 `go.sum`。

- [ ] **Step 2: 執行 go tidy 整理依賴**
  
  在專案根目錄下執行：
  ```bash
  go tidy
  ```
  Expected: 依賴整理完畢，無錯誤。

- [ ] **Step 3: 提交變更**
  
  ```bash
  git add go.mod go.sum
  git commit -m "deps: add github.com/mitchellh/go-homedir dependency"
  ```

---

### Task 2: 替換程式碼實作 (Replace Code Implementation)

**Files:**
- Modify: [daemon/helpers.go](file:///Users/shuk/projects/tmp/pm2/daemon/helpers.go) (移除 `expandHome`)
- Modify: [daemon/server.go](file:///Users/shuk/projects/tmp/pm2/daemon/server.go) (引入 homedir 並 inline 替換)

- [ ] **Step 1: 修改 daemon/server.go**
  
  在 `daemon/server.go` 中引入 `"github.com/mitchellh/go-homedir"`：
  ```go
  import (
      // ... 其它 imports ...
      "github.com/mitchellh/go-homedir"
  )
  ```
  
  將原本 `logFile = expandHome(logFile)` 和 `errFile = expandHome(errFile)` 的地方改為直接調用 `homedir.Expand`：
  ```diff
  -	} else {
  -		logFile = expandHome(logFile)
  -	}
  +	} else {
  +		if h, err := homedir.Expand(logFile); err == nil {
  +			logFile = h
  +		}
  +	}
  ```
  與
  ```diff
  -	} else {
  -		errFile = expandHome(errFile)
  -	}
  +	} else {
  +		if h, err := homedir.Expand(errFile); err == nil {
  +			errFile = h
  +		}
  +	}
  ```

- [ ] **Step 2: 修改 daemon/helpers.go**
  
  移除 `expandHome` 函數 (包括其註解 L11-L28) 並且從 `import` 區塊中移除不再使用的 `"os/user"`。
  
  在 `daemon/helpers.go` 中：
  ```go
  package daemon

  import (
  	"encoding/json"
  	"os"
  	"os/user" // 若 getCurrentUser 還在使用，則保留；若 helpers.go 的其它部分未使用則移除
  	"path/filepath"
  	"strings"
  )
  ```
  (註：`getCurrentUser` 在 L32 使用了 `user.Current`，所以 `"os/user"` 仍須保留於 `daemon/helpers.go`。)

- [ ] **Step 3: 執行 go build 確認編譯正常**
  
  ```bash
  go build ./...
  ```
  Expected: 編譯成功無錯誤。

- [ ] **Step 4: 提交變更**
  
  ```bash
  git add daemon/helpers.go daemon/server.go
  git commit -m "refactor: replace expandHome with inline homedir.Expand"
  ```

---

### Task 3: 單元測試與驗證 (Unit Testing and Verification)

**Files:**
- Modify: [daemon/server_test.go](file:///Users/shuk/projects/tmp/pm2/daemon/server_test.go)

- [ ] **Step 1: 在 daemon/server_test.go 中新增測試用例**
  
  在 `daemon/server_test.go` 末尾加入 `TestStartAppOutFileHomeExpansion` 測試，以驗證 `~/` 開頭的日誌路徑能正確被展開：
  ```go
  func TestStartAppOutFileHomeExpansion(t *testing.T) {
  	testDir := testDir(t)
  	s := NewServer(testDir)
  
  	req := &AppStartReq{
  		Namespace: "default",
  		Name:      "homeexpandcheck",
  		Script:    "/bin/sh",
  		Args:      []string{"-c", "sleep 1"},
  		Instances: 1,
  		OutFile:   "~/test-home-expand-out.log",
  		ErrorFile: "~/test-home-expand-err.log",
  	}
  
  	pi, err := s.startApp(req)
  	if err != nil {
  		t.Fatalf("startApp failed: %v", err)
  	}
  	defer s.stopByName("homeexpandcheck")
  
  	if !strings.HasPrefix(pi.LogFile, "/") || strings.Contains(pi.LogFile, "~") {
  		t.Errorf("LogFile path was not expanded: got %s", pi.LogFile)
  	}
  	if !strings.HasPrefix(pi.ErrorFile, "/") || strings.Contains(pi.ErrorFile, "~") {
  		t.Errorf("ErrorFile path was not expanded: got %s", pi.ErrorFile)
  	}
  }
  ```

- [ ] **Step 2: 執行 daemon 單元測試**
  
  ```bash
  go test -v ./daemon/... -run TestStartAppOutFileHomeExpansion
  ```
  Expected: 測試通過 (PASS)。

- [ ] **Step 3: 執行全案單元測試**
  
  ```bash
  go test ./...
  ```
  Expected: 所有測試皆通過 (ok)。

- [ ] **Step 4: 提交測試變更**
  
  ```bash
  git add daemon/server_test.go
  git commit -m "test: add TestStartAppOutFileHomeExpansion for home path expansion"
  ```

---

## Verification Plan

### Automated Tests
- 執行個別單元測試：
  ```bash
  go test -v ./daemon/... -run TestStartAppOutFileHomeExpansion
  ```
- 執行全部單元測試：
  ```bash
  go test ./...
  ```

### Manual Verification
- 啟動 pm2 daemon 後，運行一個指定輸出檔案為 `~/pm2-test.log` 的應用，檢查生成的 ProcessInfo 以及實體檔案路徑是否正確對應至家目錄絕對路徑。
