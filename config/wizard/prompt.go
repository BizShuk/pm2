package wizard

import (
	"bufio"
	"fmt"
	"io"
	"math/rand/v2"
	"strconv"
	"strings"

	"github.com/bizshuk/pm2/process"
)

const (
	maxChoiceAttempts     = 5
	randomCronStartMinute = 2 * 60
	randomCronEndMinute   = 8 * 60
)

// promptLine reads a single line, trims whitespace, returns it.
// Empty input == def. EOF returns an error.
func promptLine(rdr *bufio.Reader, out io.Writer, label, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(out, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(out, "%s: ", label)
	}
	line, err := rdr.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	if err == io.EOF && line == "" {
		return "", nil
	}
	line = strings.TrimRight(line, "\r\n")
	return line, nil
}

// promptYesNo accepts y/yes/n/no (case-insensitive). Empty == def.
func promptYesNo(rdr *bufio.Reader, out io.Writer, label string, def bool) (bool, error) {
	hint := "y/N"
	if def {
		hint = "Y/n"
	}
	fmt.Fprintf(out, "%s [%s]: ", label, hint)
	line, err := rdr.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	line = strings.TrimSpace(strings.TrimRight(line, "\r\n"))
	if line == "" {
		return def, nil
	}
	switch strings.ToLower(line) {
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	}
	return def, nil
}

// promptChoice renders a numbered menu and returns a one-based selection.
// Blank input selects defaultChoice; invalid input is retried up to five times.
func promptChoice(
	rdr *bufio.Reader,
	out io.Writer,
	label string,
	options []string,
	defaultChoice int,
) (int, error) {
	if len(options) == 0 {
		return 0, fmt.Errorf("%s has no options", label)
	}
	if defaultChoice < 1 || defaultChoice > len(options) {
		return 0, fmt.Errorf("%s default choice %d is out of range", label, defaultChoice)
	}

	fmt.Fprintf(out, "%s:\n", label)
	for i, option := range options {
		fmt.Fprintf(out, "  %d. %s\n", i+1, option)
	}

	choiceLabel := "Choose " + strings.ToLower(label)
	for attempt := 0; attempt < maxChoiceAttempts; attempt++ {
		raw, err := promptLine(rdr, out, choiceLabel, strconv.Itoa(defaultChoice))
		if err != nil {
			return 0, err
		}
		if strings.TrimSpace(raw) == "" {
			return defaultChoice, nil
		}
		choice, err := strconv.Atoi(strings.TrimSpace(raw))
		if err == nil && choice >= 1 && choice <= len(options) {
			return choice, nil
		}
		fmt.Fprintf(out, "  (invalid choice, enter 1-%d)\n", len(options))
	}
	return 0, fmt.Errorf(
		"invalid %s choice after %d attempts",
		strings.ToLower(label),
		maxChoiceAttempts,
	)
}

// promptPositiveInt reads a positive integer with a soft retry loop and falls
// back to def after three invalid answers.
func promptPositiveInt(
	rdr *bufio.Reader,
	out io.Writer,
	label string,
	def int,
) (int, error) {
	if def <= 0 {
		return 0, fmt.Errorf("%s default %d must be positive", label, def)
	}

	defaultValue := strconv.Itoa(def)
	for i := 0; i < 3; i++ {
		s, err := promptLine(rdr, out, label, defaultValue)
		if err != nil {
			return 0, err
		}
		if s == "" {
			return def, nil
		}
		n, parseErr := strconv.Atoi(strings.TrimSpace(s))
		if parseErr == nil && n > 0 {
			return n, nil
		}
		fmt.Fprintln(out, "  (invalid number, try again)")
	}
	return def, nil
}

// promptInstances reads an instance count and defaults to one.
func promptInstances(rdr *bufio.Reader, out io.Writer) (int, error) {
	return promptPositiveInt(rdr, out, "Instances", process.DefaultInstances)
}

// promptEnvVars loops reading KEY=VAL until blank line.
func promptEnvVars(rdr *bufio.Reader, out io.Writer) (map[string]string, error) {
	env := make(map[string]string)
	fmt.Fprintln(out, "Env vars? (one per line KEY=VAL; blank line to finish)")
	for {
		s, err := promptLine(rdr, out, "  env", "")
		if err != nil {
			return nil, err
		}
		if s == "" {
			break
		}
		parts := strings.SplitN(s, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			fmt.Fprintf(out, "  (ignoring malformed env: %q)\n", s)
			continue
		}
		env[strings.TrimSpace(parts[0])] = parts[1]
	}
	if len(env) == 0 {
		return nil, nil
	}
	return env, nil
}

// resolveCronSchedule maps the wizard's r shortcut to one concrete daily
// schedule in the inclusive 02:00-08:00 window. Other input is returned
// trimmed so callers can supply any custom cron expression.
func resolveCronSchedule(raw string) string {
	schedule := strings.TrimSpace(raw)
	if !strings.EqualFold(schedule, "r") {
		return schedule
	}

	minuteOfDay := randomCronStartMinute +
		rand.IntN(randomCronEndMinute-randomCronStartMinute+1)
	return fmt.Sprintf("%d %d * * *", minuteOfDay%60, minuteOfDay/60)
}
