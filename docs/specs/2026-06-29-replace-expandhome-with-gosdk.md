# 替換 expandHome 為 gosdk 家目錄展開規範規格書 (Replace expandHome with gosdk path expansion standard Specification)

## 概述 (Overview)

為了符合 `gosdk` 的設計規範，此變更移除了 `daemon/helpers.go` 中的自訂家目錄展開 (Home Path Expansion) 函數 `expandHome`，並將其替換為直接使用 `github.com/mitchellh/go-homedir` 的 `homedir.Expand` 進行 inline 展開。

此外，應使用者要求，將原先用來獲取目前使用者的輔助函數 `getCurrentUser` 也進行了移除，改為在呼叫端直接使用 `os/user` 進行 `user.Current()` 呼叫，不再透過額外的 helper 函數包裝。

## 變更詳情 (Changes Detail)

### 1. 移除舊封裝與輔助函數
- 在 [daemon/helpers.go](file:///Users/shuk/projects/tmp/pm2/daemon/helpers.go) 中刪除 `expandHome` 與 `getCurrentUser` 函數。
- 移除不再使用的 `"strings"` 與 `"os/user"` 引入包。

### 2. 引入 go-homedir 與 inline 替換
- 在 [daemon/server.go](file:///Users/shuk/projects/tmp/pm2/daemon/server.go) 中加入 `"github.com/mitchellh/go-homedir"` 與 `"os/user"` 引入包。
- 替換原日誌路徑展開邏輯，直接以 `homedir.Expand` 取代舊的 `expandHome`：
  ```go
  if h, err := homedir.Expand(logFile); err == nil {
      logFile = h
  }
  ```
- 替換獲取當前使用者邏輯，在 `launchProcess` 中直接獲取當前用戶：
  ```go
  currentUser := "unknown"
  if u, err := user.Current(); err == nil {
      currentUser = u.Username
  }
  ```
  並直接賦值給 `ProcessInfo.User` 欄位。

### 3. 測試用例與驗證
- 在 [daemon/server_test.go](file:///Users/shuk/projects/tmp/pm2/daemon/server_test.go) 中新增了 `TestStartAppOutFileHomeExpansion` 單元測試，專門驗證傳入 `~/` 前綴路徑時，能正確展開為絕對路徑且不留存 `~` 符號。
