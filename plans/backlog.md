# 待辦清單選項 (Backlog Options)

以下為未來可供選擇的優化方案與待辦項目：

---

## 動態路徑包裝腳本方案 (Dynamic Path Wrapper Script Option)

目前將 `PATH` 硬編碼在 `.plist` 裡就像是把地圖直接印在衣服上，一旦系統路徑變了就得重新修改。最靈活、最優雅的動態解法是請一個「中間人」——也就是**包裝腳本 (Wrapper Script)**。

讓 `.plist` 只負責啟動這個腳本，再由腳本在執行的那一刻，**動態去抓取** macOS 系統當前最新的 `PATH`。

### 設定步驟

#### 1. 建立一個包裝腳本 (Wrapper Script)

建立一個新的 Shell 腳本（例如：`launch_wrapper.sh`），利用 macOS 內建的 `path_helper` 工具來動態產生路徑。

內容如下：

```bash
#!/bin/bash

# 1. 動態載入 macOS 系統的核心路徑設定
if [ -x /usr/libexec/path_helper ]; then
    eval "$(/usr/libexec/path_helper -s)"
fi

# 2. 動態偵測並加入常見的第三方套件路徑（如 Homebrew）
# 檢查 Apple Silicon (M1/M2/M3/M4) 的 Homebrew 路徑
if [ -d "/opt/homebrew/bin" ]; then
    export PATH="/opt/homebrew/bin:$PATH"
fi
# 檢查 Intel Mac 的 Homebrew 路徑
if [ -d "/usr/local/bin" ]; then
    export PATH="/usr/local/bin:$PATH"
fi

# 3. 執行真正想要跑的主程式
exec /path/to/your/actual_program --arguments
```

> 關鍵機制解析：
> * `path_helper`：這是 macOS 內建的工具，它會去讀取 `/etc/paths` 和 `/etc/paths.d/` 資料夾，把系統目前註冊的所有公用路徑動態組裝起來。
> * `exec`：用新進程直接取代目前的腳本進程，這樣主程式就能完美繼承前面剛算好的動態 `PATH`，且不會留下多餘的背景執行緒。

#### 2. 賦予腳本執行權限 (Execution Permission)

系統守護進程必須要有權限去執行這個新寫好的腳本。

```bash
chmod +x /path/to/launch_wrapper.sh
```

#### 3. 簡化 `.plist` 設定檔

現在 `.plist` 變得超級乾淨，完全不需要寫任何 `EnvironmentVariables`，只要把啟動目標指向包裝腳本即可。

將 `.plist` 的 `ProgramArguments` 修改如下：

```xml
<key>ProgramArguments</key>
<array>
    <string>/path/to/launch_wrapper.sh</string>
</array>
```

#### 4. 重啟服務以套用變更

叫 `launchd` 重新載入設定：

```bash
sudo launchctl unload /Library/LaunchDaemons/com.user.mydaemon.plist
sudo launchctl load /Library/LaunchDaemons/com.user.mydaemon.plist
```

此方案能使 Launch Daemon 在啟動時自動適應系統路徑的變更，避免手動修改 XML 設定檔。
