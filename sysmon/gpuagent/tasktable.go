package gpuagent

import (
	"strconv"
	"strings"

	"github.com/bizshuk/pm2/sysmon"
)

// columnGapSpaces is the minimum run of spaces powermetrics puts between
// two columns. One space is not a separator: process names contain
// single spaces ("Google Chrome Helper") and splitting on them would
// shift every field after the name by one.
const columnGapSpaces = 2

// column is one header cell and the character span it occupies.
type column struct {
	name  string
	start int
	end   int
}

// taskTable extracts per-process GPU time from the "Running tasks" block
// that `--samplers tasks --show-process-gpu` prints.
//
// It is driven by the header rather than by fixed offsets. The man page
// says per-process GPU time is "only available on certain hardware", and
// each --show-process-* flag adds its own column, so the set and order
// of columns is a runtime fact. Reading the header means an unsupported
// machine reports nothing instead of reporting whatever number happens
// to sit at the offset a supported machine would have used.
// The layout outlives any one sample: powermetrics prints the header
// when the table's shape changes, not necessarily once per block, so a
// parser that forgot the columns at every boundary would attribute GPU
// time to the first sample and nothing after it.
type taskTable struct {
	idColumn  *column
	gpuColumn *column

	// supported records that a GPU column was seen at all, which is how
	// "this hardware cannot attribute GPU time" is told apart from "no
	// process used the GPU during this sample".
	supported bool
	rows      []sysmon.ProcGPU
}

// apply offers one line to the table. A line only becomes a row if the
// cell under the ID heading is a real PID, which is the guard that lets
// every unrecognised line be offered here safely: section markers, blank
// lines and the aggregate ALL_TASKS row all fail it.
func (t *taskTable) apply(line string) {
	columns := splitColumns(line)
	if findColumn(columns, "ID") != nil {
		t.readHeader(columns)
		return
	}
	t.appendRow(line)
}

func (t *taskTable) readHeader(columns []column) {
	t.idColumn = findColumn(columns, "ID")
	t.gpuColumn = findPrefixColumn(columns, "GPU")
	if t.gpuColumn != nil {
		t.supported = true
	}
}

// appendRow records one process row, skipping anything whose ID is not a
// real PID — powermetrics ends the table with aggregate rows such as
// ALL_TASKS, and a summary counted as a process would double every
// number in the join.
func (t *taskTable) appendRow(line string) {
	if t.idColumn == nil || t.gpuColumn == nil {
		return
	}

	pid, err := strconv.Atoi(valueUnder(line, *t.idColumn))
	if err != nil || pid <= 0 {
		return
	}
	milliseconds, err := strconv.ParseFloat(valueUnder(line, *t.gpuColumn), 64)
	if err != nil || milliseconds <= 0 {
		// A process with no GPU time is the overwhelming majority of the
		// table. Dropping those rows is what keeps the published file a
		// few hundred bytes instead of a few hundred kilobytes.
		return
	}
	t.rows = append(t.rows, sysmon.ProcGPU{PID: pid, MillisecondsPerSecond: milliseconds})
}

// splitColumns breaks a line into its cells and records where each one
// sits, so a value can later be matched to a header by overlap rather
// than by counting fields.
func splitColumns(line string) []column {
	var columns []column
	start := -1
	spaces := 0

	for index, char := range line {
		if char == ' ' {
			spaces++
			if start >= 0 && spaces >= columnGapSpaces {
				columns = append(columns, column{
					name:  strings.TrimSpace(line[start : index-spaces+1]),
					start: start,
					end:   index - spaces + 1,
				})
				start = -1
			}
			continue
		}
		spaces = 0
		if start < 0 {
			start = index
		}
	}
	if start >= 0 {
		columns = append(columns, column{name: strings.TrimSpace(line[start:]), start: start, end: len(line)})
	}
	return columns
}

// valueUnder returns the cell of row that overlaps the header column.
//
// Numbers are right-aligned under their heading and names are left-
// aligned, so neither a value wider than its heading nor one narrower
// lines up exactly. Overlap is the only relation that holds for both.
func valueUnder(row string, header column) string {
	for _, cell := range splitColumns(row) {
		if cell.start < header.end && header.start < cell.end {
			return cell.name
		}
	}
	return ""
}

func findColumn(columns []column, name string) *column {
	for index := range columns {
		if columns[index].name == name {
			return &columns[index]
		}
	}
	return nil
}

func findPrefixColumn(columns []column, prefix string) *column {
	for index := range columns {
		if strings.HasPrefix(columns[index].name, prefix) {
			return &columns[index]
		}
	}
	return nil
}
