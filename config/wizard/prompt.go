package wizard

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
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

// promptInstances reads an int with a soft retry loop; falls back to 1.
func promptInstances(rdr *bufio.Reader, out io.Writer) (int, error) {
	for i := 0; i < 3; i++ {
		s, err := promptLine(rdr, out, "Instances", "1")
		if err != nil {
			return 0, err
		}
		if s == "" {
			return 1, nil
		}
		n, perr := strconv.Atoi(strings.TrimSpace(s))
		if perr == nil && n > 0 {
			return n, nil
		}
		fmt.Fprintln(out, "  (invalid number, try again)")
	}
	return 1, nil
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