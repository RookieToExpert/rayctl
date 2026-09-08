package cmd

import (
	"reflect"
	"testing"

	"rayctl/internal/nodehealth"
)

func TestParseNodeHealthChecks(t *testing.T) {
	checks, err := parseNodeHealthChecks("disk,node-basic,disk")
	if err != nil {
		t.Fatalf("parseNodeHealthChecks() error = %v", err)
	}
	want := []string{nodehealth.CheckDisk, nodehealth.CheckNodeBasic}
	if !reflect.DeepEqual(checks, want) {
		t.Fatalf("checks = %#v, want %#v", checks, want)
	}
}

func TestValidateNodeHealthTarget(t *testing.T) {
	if err := validateNodeHealthTarget(nil, "", ""); err == nil {
		t.Fatal("expected missing target error")
	}
	if err := validateNodeHealthTarget([]string{"host-1"}, "queue-1", ""); err == nil {
		t.Fatal("expected mutually exclusive target error")
	}
	if err := validateNodeHealthTarget(nil, "queue-1", ""); err != nil {
		t.Fatalf("queue target error = %v", err)
	}
}
