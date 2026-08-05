package task

import (
	"io"
	"strings"
	"testing"
)

func TestValidateDeleteFlagsRejectsSelectionModes(t *testing.T) {
	tests := []struct {
		name   string
		single bool
		all    bool
		with   []string
	}{
		{name: "all", all: true},
		{name: "with", with: []string{"worker"}},
		{name: "single", single: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDeleteFlags(true, tt.single, tt.all, tt.with)
			if err == nil {
				t.Fatalf("expected --delete with --%s to fail", tt.name)
			}
			if !strings.Contains(err.Error(), "--delete cannot be used") {
				t.Errorf("error = %q, want conflicting flag explanation", err)
			}
		})
	}

	if err := validateDeleteFlags(true, false, false, nil); err != nil {
		t.Errorf("--delete alone = %v, want nil", err)
	}
	if err := validateDeleteFlags(false, true, false, nil); err != nil {
		t.Errorf("--single without --delete = %v, want nil", err)
	}
}

func TestDeleteEcosystemAppsRejectsEmptyEcosystem(t *testing.T) {
	err := deleteEcosystemApps(nil, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "no apps to delete") {
		t.Fatalf("error = %v, want empty-ecosystem explanation", err)
	}
}

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
