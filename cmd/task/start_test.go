package task

import (
	"strings"
	"testing"
)

func TestValidateSelectionFlagsRejectsSingleWithOtherModes(t *testing.T) {
	tests := []struct {
		name string
		all  bool
		with []string
	}{
		{name: "all", all: true},
		{name: "with", with: []string{"worker"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSelectionFlags(true, tt.all, tt.with)
			if err == nil {
				t.Fatalf("expected --single with --%s to fail", tt.name)
			}
			if !strings.Contains(err.Error(), "--single cannot be used") {
				t.Errorf("error = %q, want conflicting flag explanation", err)
			}
		})
	}
}
