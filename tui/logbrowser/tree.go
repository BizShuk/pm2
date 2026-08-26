package logbrowser

import (
	"fmt"

	"github.com/bizshuk/pm2/tui/views"
)

type treeRowKind uint8

const (
	treeTask treeRowKind = iota
	treeFile
)

type treeRow struct {
	kind      treeRowKind
	taskIndex int
	fileIndex int
}

func (m Model) visibleTreeRows() []treeRow {
	rows := make([]treeRow, 0, len(m.tasks))
	for taskIndex, task := range m.tasks {
		rows = append(rows, treeRow{
			kind:      treeTask,
			taskIndex: taskIndex,
			fileIndex: -1,
		})
		if !m.expanded[task.Task] {
			continue
		}
		for fileIndex := range task.Files {
			rows = append(rows, treeRow{
				kind:      treeFile,
				taskIndex: taskIndex,
				fileIndex: fileIndex,
			})
		}
	}
	return rows
}

func (m Model) selectedTreeRow() (treeRow, bool) {
	rows := m.visibleTreeRows()
	if len(rows) == 0 {
		return treeRow{}, false
	}
	return rows[clampIndex(m.treeCursor, len(rows))], true
}

func (m Model) selectedFilePath() string {
	row, ok := m.selectedTreeRow()
	if !ok || row.kind != treeFile {
		return ""
	}
	files := m.tasks[row.taskIndex].Files
	if row.fileIndex < 0 || row.fileIndex >= len(files) {
		return ""
	}
	return files[row.fileIndex].Path
}

func (m Model) treeIndexForTask(taskIndex int) int {
	for index, row := range m.visibleTreeRows() {
		if row.kind == treeTask && row.taskIndex == taskIndex {
			return index
		}
	}
	return 0
}

func (m Model) treeItems() []string {
	rows := m.visibleTreeRows()
	items := make([]string, len(rows))
	for index, row := range rows {
		if row.kind == treeTask {
			items[index] = m.taskTreeItem(row.taskIndex)
			continue
		}
		items[index] = m.fileTreeItem(row.taskIndex, row.fileIndex)
	}
	return items
}

// nameColumn is the width the application and file name columns share so the
// size column stays visible inside the 40% tree pane. Longer names are cut
// rather than allowed to push the columns behind the pane edge.
const nameColumn = 22

func (m Model) taskTreeItem(taskIndex int) string {
	task := m.tasks[taskIndex]
	marker := "▸"
	if m.expanded[task.Task] {
		marker = "▾"
	}
	return fmt.Sprintf("%s %-*s  %3d files  %8s",
		marker,
		nameColumn,
		// A task is identified by the head of its name; a log file
		// by the tail, where the stream and rotation date live.
		views.CropRight(task.Task, nameColumn),
		len(task.Files),
		formatFileSize(task.TotalSize()),
	)
}

func (m Model) fileTreeItem(taskIndex, fileIndex int) string {
	file := m.tasks[taskIndex].Files[fileIndex]
	// The diamond is the whole current/archive distinction; an "archive"
	// word in its place would cost seven columns to say "not that one".
	marker := "  "
	if file.Current {
		marker = "🔶"
	}
	return fmt.Sprintf("   %s %-*s  %8s  %s",
		marker,
		nameColumn,
		views.Crop(file.Name, nameColumn),
		formatFileSize(file.Size),
		file.ModTime.Format("2006-01-02 15:04:05"),
	)
}

func formatFileSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	value := float64(size)
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	for _, label := range units {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, label)
		}
	}
	return fmt.Sprintf("%.1f PiB", value/unit)
}
