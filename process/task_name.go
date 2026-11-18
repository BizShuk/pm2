package process

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Task names follow `<Purpose> - <Frequency>`: every Purpose word starts
// upper-case (or with a digit), and the Frequency is one of TaskFrequencies.
// Example: `Classify Photo - Daily`.

const (
	FREQUENCY_HOURLY    = "Hourly"
	FREQUENCY_DAILY     = "Daily"
	FREQUENCY_WEEKLY    = "Weekly"
	FREQUENCY_MONTHLY   = "Monthly"
	FREQUENCY_ALWAYS    = "Always"
	FREQUENCY_ON_DEMAND = "On Demand"
)

// TaskFrequencies lists the accepted Frequency suffixes.
var TaskFrequencies = []string{
	FREQUENCY_HOURLY, FREQUENCY_DAILY, FREQUENCY_WEEKLY,
	FREQUENCY_MONTHLY, FREQUENCY_ALWAYS, FREQUENCY_ON_DEMAND,
}

var taskNamePattern = regexp.MustCompile(
	`^[A-Z0-9]\S*( [A-Z0-9]\S*)* - (` + strings.Join(TaskFrequencies, "|") + `)$`,
)

// ValidateTaskName rejects a name outside `<Purpose> - <Frequency>`.
func ValidateTaskName(name string) error {
	if taskNamePattern.MatchString(name) {
		return nil
	}
	return fmt.Errorf(
		"task name %q must be `<Purpose> - <Frequency>` "+
			"(Purpose in Capital Case, Frequency one of %s), e.g. `Classify Photo - Daily`",
		name, strings.Join(TaskFrequencies, ", "),
	)
}

// FormatTaskName builds a valid name from a free-form purpose and the
// app's schedule.
func FormatTaskName(purpose string, app AppConfig) string {
	return fmt.Sprintf("%s - %s", capitalWords(purpose), TaskFrequency(app))
}

// TaskFrequency derives the Frequency term from the app's schedule:
// a cron task by its fields, an optional task On Demand, otherwise Always.
func TaskFrequency(app AppConfig) string {
	fields := strings.Fields(app.Cron)
	switch {
	case len(fields) != 5:
		if app.Optional {
			return FREQUENCY_ON_DEMAND
		}
		return FREQUENCY_ALWAYS
	case !isFixed(fields[1]):
		return FREQUENCY_HOURLY
	case fields[2] != "*":
		return FREQUENCY_MONTHLY
	case fields[4] != "*":
		return FREQUENCY_WEEKLY
	default:
		return FREQUENCY_DAILY
	}
}

func isFixed(field string) bool {
	return strings.IndexFunc(field, func(r rune) bool { return !unicode.IsDigit(r) }) < 0
}

func capitalWords(s string) string {
	words := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == '-' || r == '_' })
	for i, w := range words {
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	if len(words) == 0 {
		return "Task"
	}
	return strings.Join(words, " ")
}
