package logbrowser

import (
	"fmt"

	"github.com/bizshuk/pm2/process"
)

type treeRowKind uint8

const (
	treeApplication treeRowKind = iota
	treeFile
)

type treeRow struct {
	kind             treeRowKind
	applicationIndex int
	fileIndex        int
}

func (m Model) visibleTreeRows() []treeRow {
	rows := make([]treeRow, 0, len(m.applications))
	for applicationIndex := range m.applications {
		rows = append(rows, treeRow{
			kind:             treeApplication,
			applicationIndex: applicationIndex,
			fileIndex:        -1,
		})
		if !m.expanded[applicationIndex] {
			continue
		}
		for fileIndex := range m.filesByApplication[applicationIndex] {
			rows = append(rows, treeRow{
				kind:             treeFile,
				applicationIndex: applicationIndex,
				fileIndex:        fileIndex,
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
	files := m.filesByApplication[row.applicationIndex]
	if row.fileIndex < 0 || row.fileIndex >= len(files) {
		return ""
	}
	return files[row.fileIndex].Path
}

func (m Model) treeIndexForApplication(applicationIndex int) int {
	for index, row := range m.visibleTreeRows() {
		if row.kind == treeApplication &&
			row.applicationIndex == applicationIndex {
			return index
		}
	}
	return 0
}

func (m Model) treeItems() []string {
	rows := m.visibleTreeRows()
	items := make([]string, len(rows))
	for index, row := range rows {
		if row.kind == treeApplication {
			items[index] = m.applicationTreeItem(row.applicationIndex)
			continue
		}
		items[index] = m.fileTreeItem(row.applicationIndex, row.fileIndex)
	}
	return items
}

func (m Model) applicationTreeItem(applicationIndex int) string {
	application := m.applications[applicationIndex]
	namespace := application.Namespace
	if namespace == "" {
		namespace = process.DefaultNamespace
	}
	marker := "▸"
	if m.expanded[applicationIndex] {
		marker = "▾"
	}
	return fmt.Sprintf("[%d] %s %s:%s  %s",
		application.ID,
		marker,
		namespace,
		application.Name,
		application.Status,
	)
}

func (m Model) fileTreeItem(applicationIndex, fileIndex int) string {
	file := m.filesByApplication[applicationIndex][fileIndex]
	kind := "archive"
	if file.Current {
		kind = "🔶"
	}
	return fmt.Sprintf("    %-7s  %-28s  %8s  %s",
		kind,
		file.Name,
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
