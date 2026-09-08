package cmd

import (
	"testing"
	"time"

	"github.com/spf13/cobra"

	metricsquery "rayctl/internal/metrics"
)

func TestParseMetricsRangeUsesBeijingTime(t *testing.T) {
	start, end, err := parseMetricsRange("2h", "2026-09-04 10:00:00", "2026-09-04 12:00:00")
	if err != nil {
		t.Fatalf("parseMetricsRange() error = %v", err)
	}
	if got := start.In(metricsquery.BeijingLocation).Format("15:04"); got != "10:00" {
		t.Fatalf("start = %s, want 10:00 UTC+8", got)
	}
	if end.Sub(start) != 2*time.Hour {
		t.Fatalf("duration = %s, want 2h", end.Sub(start))
	}
}

func TestValidateMetricsTimeFlags(t *testing.T) {
	command := &cobra.Command{Use: "test"}
	command.Flags().String("since", "2h", "")
	if err := validateMetricsTimeFlags(command, "2026-09-04 10:00:00", ""); err == nil {
		t.Fatal("expected incomplete start/end to fail")
	}
	if err := validateMetricsTimeFlags(command, "2026-09-04 10:00:00", "2026-09-04 12:00:00"); err != nil {
		t.Fatalf("default --since should not conflict: %v", err)
	}
	if err := command.Flags().Set("since", "1h"); err != nil {
		t.Fatal(err)
	}
	if err := validateMetricsTimeFlags(command, "2026-09-04 10:00:00", "2026-09-04 12:00:00"); err == nil {
		t.Fatal("expected explicit --since and start/end to conflict")
	}
}
