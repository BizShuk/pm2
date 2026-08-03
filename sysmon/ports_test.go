package sysmon

import "testing"

// Captured from `lsof -nP -iTCP -sTCP:LISTEN -F pcPnT`. Port 49159 is
// bound on two descriptors, which is the common case and must not be
// reported twice.
const lsofOutput = `p455
crapportd
f10
PTCP
n*:49159
TST=LISTEN
TQR=0
TQS=0
f11
PTCP
n*:49159
TST=LISTEN
f12
PTCP
n127.0.0.1:8080
TST=LISTEN
p1978
node
f23
PTCP
n[::1]:9229
TST=LISTEN
f24
PTCP
n127.0.0.1:5432
TST=ESTABLISHED
`

func TestParseLsofPorts(t *testing.T) {
	ports := parseLsofPorts(lsofOutput)

	if len(ports[455]) != 2 {
		t.Fatalf("pid 455 has %d ports %+v, want the duplicate descriptor collapsed to 2", len(ports[455]), ports[455])
	}
	if ports[455][0].Port != 49159 || ports[455][0].Address != "*" {
		t.Errorf("first port = %+v, want *:49159", ports[455][0])
	}
	if ports[455][1].Port != 8080 || ports[455][1].Address != "127.0.0.1" {
		t.Errorf("second port = %+v, want 127.0.0.1:8080", ports[455][1])
	}

	if len(ports[1978]) != 1 {
		t.Fatalf("pid 1978 has %+v, want only the LISTEN row — an established connection is not a listener", ports[1978])
	}
	if ports[1978][0].Address != "[::1]" || ports[1978][0].Port != 9229 {
		t.Errorf("ipv6 listener = %+v, want [::1]:9229", ports[1978][0])
	}
	if ports[1978][0].Protocol != "tcp" {
		t.Errorf("protocol = %q, want lower-case tcp", ports[1978][0].Protocol)
	}
}

const ssOutput = `LISTEN 0      4096         0.0.0.0:22        0.0.0.0:*    users:(("sshd",pid=812,fd=3))
LISTEN 0      511          0.0.0.0:80        0.0.0.0:*    users:(("nginx",pid=901,fd=6),("nginx",pid=902,fd=6))
LISTEN 0      128             [::]:443          [::]:*
`

func TestParseSSPorts(t *testing.T) {
	ports := parseSSPorts(ssOutput)

	if len(ports[812]) != 1 || ports[812][0].Port != 22 {
		t.Errorf("sshd ports = %+v, want a single listener on 22", ports[812])
	}
	// Forked workers share one socket; every owning PID must be able to
	// find it, because pm2 may be managing any one of them.
	if len(ports[901]) != 1 || len(ports[902]) != 1 {
		t.Errorf("nginx workers = %+v / %+v, want the shared socket attributed to both", ports[901], ports[902])
	}
	if ports[901][0].Address != "0.0.0.0" {
		t.Errorf("address = %q, want 0.0.0.0", ports[901][0].Address)
	}
	// A row with no users:(...) group has no owner to attribute it to.
	for pid, owned := range ports {
		for _, port := range owned {
			if port.Port == 443 {
				t.Errorf("pid %d claimed the unowned :443 row %+v", pid, port)
			}
		}
	}
}

func TestSplitHostPort(t *testing.T) {
	cases := []struct {
		input       string
		wantAddress string
		wantPort    int
	}{
		{"*:49159", "*", 49159},
		{"127.0.0.1:8080", "127.0.0.1", 8080},
		{"[::1]:9229", "[::1]", 9229},
		{"*:*", "*", 0},
		{"noport", "", 0},
	}
	for _, tc := range cases {
		address, port := splitHostPort(tc.input)
		if address != tc.wantAddress || port != tc.wantPort {
			t.Errorf("splitHostPort(%q) = %q,%d want %q,%d", tc.input, address, port, tc.wantAddress, tc.wantPort)
		}
	}
}
