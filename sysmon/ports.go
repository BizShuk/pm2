package sysmon

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// readListeningPorts returns every listening TCP socket the current user
// can see, keyed by owning PID.
//
// Linux prefers `ss` (part of iproute2, present on far more images than
// lsof) and falls back to lsof; macOS has lsof in the base system. Both
// tools only report sockets the caller owns unless run as root, which is
// exactly the scope pm2 needs — it manages the invoking user's processes.
func readListeningPorts(goos string) (map[int][]Port, error) {
	if goos == "linux" {
		if ports, err := readSSPorts(); err == nil {
			return ports, nil
		}
	}
	return readLsofPorts()
}

func readLsofPorts() (map[int][]Port, error) {
	output, err := exec.Command("lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-F", "pcPnT").Output()
	if err != nil {
		// lsof exits non-zero when it finds nothing at all, which is a
		// legitimate state for a machine with no listeners. Only treat a
		// missing binary as an error worth surfacing.
		if len(output) == 0 && isCommandMissing(err) {
			return nil, fmt.Errorf("read listening ports: %w", err)
		}
	}
	return parseLsofPorts(string(output)), nil
}

// parseLsofPorts consumes lsof's field output (-F). The format is a flat
// line stream where the first byte selects the field: `p` opens a process
// block, `f` opens a file (descriptor) block inside it, and P/n/T carry
// the protocol, address and TCP state of the current descriptor.
//
// One listening socket routinely appears on several descriptors, so
// identical (pid, protocol, address, port) rows are collapsed.
func parseLsofPorts(output string) map[int][]Port {
	ports := make(map[int][]Port)
	seen := make(map[string]bool)

	var (
		pid     int
		current Port
	)
	flush := func() {
		if pid == 0 || current.Address == "" || current.State != "LISTEN" {
			return
		}
		current.PID = pid
		key := fmt.Sprintf("%d/%s/%s/%d", pid, current.Protocol, current.Address, current.Port)
		if !seen[key] {
			seen[key] = true
			ports[pid] = append(ports[pid], current)
		}
	}

	for line := range strings.SplitSeq(output, "\n") {
		if line == "" {
			continue
		}
		field, value := line[0], line[1:]
		switch field {
		case 'p':
			flush()
			current = Port{}
			parsed, err := strconv.Atoi(value)
			if err != nil {
				pid = 0
				continue
			}
			pid = parsed
		case 'f':
			flush()
			current = Port{}
		case 'P':
			current.Protocol = strings.ToLower(value)
		case 'n':
			current.Address, current.Port = splitHostPort(value)
		case 'T':
			if state, ok := strings.CutPrefix(value, "ST="); ok {
				current.State = state
			}
		}
	}
	flush()
	return ports
}

func readSSPorts() (map[int][]Port, error) {
	output, err := exec.Command("ss", "-lntpH").Output()
	if err != nil {
		return nil, fmt.Errorf("read listening ports: %w", err)
	}
	return parseSSPorts(string(output)), nil
}

// parseSSPorts consumes `ss -lntpH` rows, e.g.
//
//	LISTEN 0 4096 0.0.0.0:22 0.0.0.0:* users:(("sshd",pid=812,fd=3))
//
// A socket shared by several forked workers lists every PID in the same
// users:(...) group, so one row can produce several Port entries.
func parseSSPorts(output string) map[int][]Port {
	ports := make(map[int][]Port)
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		address, port := splitHostPort(fields[3])
		if address == "" {
			continue
		}
		for _, pid := range parseSSPIDs(line) {
			ports[pid] = append(ports[pid], Port{
				PID:      pid,
				Protocol: "tcp",
				Address:  address,
				Port:     port,
				State:    "LISTEN",
			})
		}
	}
	return ports
}

// parseSSPIDs pulls every `pid=NNN` out of an ss row.
func parseSSPIDs(line string) []int {
	var pids []int
	rest := line
	for {
		_, after, found := strings.Cut(rest, "pid=")
		if !found {
			return pids
		}
		rest = after
		end := strings.IndexFunc(rest, func(r rune) bool { return r < '0' || r > '9' })
		digits := rest
		if end >= 0 {
			digits = rest[:end]
		}
		if pid, err := strconv.Atoi(digits); err == nil {
			pids = append(pids, pid)
		}
	}
}

// splitHostPort splits an "address:port" pair as printed by lsof and ss.
// IPv6 literals carry their brackets ("[::1]:8080") and a wildcard port
// ("*:*") yields port 0.
func splitHostPort(value string) (string, int) {
	index := strings.LastIndex(value, ":")
	if index < 0 {
		return "", 0
	}
	address := value[:index]
	port, err := strconv.Atoi(value[index+1:])
	if err != nil {
		port = 0
	}
	if address == "" {
		address = "*"
	}
	return address, port
}

// isCommandMissing reports whether err came from the binary not existing
// rather than from the command running and returning non-zero.
func isCommandMissing(err error) bool {
	var execErr *exec.Error
	return errors.As(err, &execErr)
}
