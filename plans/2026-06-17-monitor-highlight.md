# Monitor Highlight Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Provide a bright highlight style for the currently selected process in both `pm2 m` and `pm2 m -d`.

**Architecture:** Modify color theme styles and cell rendering logic in Bubbletea TUI to apply bold, bright cyan text to the selected process name, bright text for general metadata, and keep bold status colors.

**Tech Stack:** Go 1.24+, charmbracelet/lipgloss, charmbracelet/bubbletea

---

### Task 1: Update Styling Variables and Selection Colors

**Files:**

- Modify: `tui/model.go`

- [ ] **Step 1: Replace selection color variables**

Replace the existing `clSelBg` color and add `clSelName` and `clSelText` in `tui/model.go` around line 38.

```go
 clSelBg   = lipgloss.AdaptiveColor{Light: "#e0e7ff", Dark: "#2e3440"}
 clSelName = lipgloss.AdaptiveColor{Light: "#0891b2", Dark: "#06b6d4"}
 clSelText = lipgloss.AdaptiveColor{Light: "#0f172a", Dark: "#ffffff"}
```

- [ ] **Step 2: Modify buildLeft process list selection row rendering**

Modify `buildLeft(w, h int)` in `tui/model.go` to use the new colors and font weights when rendering the selected row.

```go
 var rows []string
 for i, p := range m.procs {
  dot := dotFor(p.Status)
  name := crop(p.Name, nameW)
  up := shortUptime(p)

  var line string
  if i == m.selected {
   nameSt := lipgloss.NewStyle().Bold(true).Foreground(clSelName)
   upSt := lipgloss.NewStyle().Foreground(clSelText)
   line = fmt.Sprintf("%s %s %s",
    dot,
    nameSt.Width(nameW).Render(name),
    upSt.Render(up),
   )
  } else {
   line = fmt.Sprintf("%s %-*s %s",
    dot, nameW, name,
    lipgloss.NewStyle().Foreground(clMuted).Render(up),
   )
  }
  st := lipgloss.NewStyle().Width(w).Padding(0, 1)
  if i == m.selected {
   st = st.Background(clSelBg)
  }
  rows = append(rows, st.Render(line))
 }
```

- [ ] **Step 3: Modify buildListTUI row rendering for selected rows**

Modify the row rendering loop in `buildListTUI()` in `tui/model.go` to split selection styling logic and regular styling logic.

```go
 // Render rows
 rowStyle := lipgloss.NewStyle()
 borderStyle := lipgloss.NewStyle().Foreground(clBorder)
 for i, p := range m.procs {
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

   style := rowStyle.Width(col.width)
   if col.align == lipgloss.Right {
    style = style.Align(lipgloss.Right)
   } else {
    style = style.Align(lipgloss.Left)
   }

   if i == m.selected {
    style = style.Background(clSelBg)
   }

   var renderedVal string
   if i == m.selected {
    switch col.name {
    case "name":
     renderedVal = style.Bold(true).Foreground(clSelName).Render(val)
    case "id", "status":
     renderedVal = style.Bold(true).Foreground(getStatusColor(p.Status)).Render(val)
    case "cpu":
     cpuSt := style.Bold(true)
     if p.Status == process.StatusOnline {
      cpuSt = cpuSt.Foreground(clOnline)
     } else {
      cpuSt = cpuSt.Foreground(clSelText)
     }
     renderedVal = cpuSt.Render(val)
    case "mem":
     memSt := style.Bold(true)
     if p.Status == process.StatusOnline {
      memSt = memSt.Foreground(clSelText)
     } else {
      memSt = memSt.Foreground(clSelText)
     }
     renderedVal = memSt.Render(val)
    default:
     renderedVal = style.Bold(true).Foreground(clSelText).Render(val)
    }
   } else {
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
   }

   var cell string
   if i == m.selected {
    cellSt := lipgloss.NewStyle().Background(clSelBg)
    cell = cellSt.Render(" ") + renderedVal + cellSt.Render(" ")
   } else {
    cell = " " + renderedVal + " "
   }
   rowParts = append(rowParts, cell)
  }

  line := borderStyle.Render("│") + strings.Join(rowParts, borderStyle.Render("│")) + borderStyle.Render("│")
  lines = append(lines, line)
 }
```

- [ ] **Step 4: Run unit tests to verify there are no compilation issues**

Run: `go test ./tui/...`
Expected: PASS

- [ ] **Step 5: Build binary and run manual tests**

Run: `go build -o pm2 main.go`
Expected: Compilation succeeds.

- [ ] **Step 6: Commit changes**

Run:

```bash
git add tui/model.go
git commit -m "feat: implement bright highlight style for selected process in monit dashboards"
```
