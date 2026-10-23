package output

import (
	"strings"
	"testing"
)

func TestPodMountTableCollapsesDuplicatePVCIdentity(t *testing.T) {
	for _, columns := range []string{"80", "200"} {
		t.Run(columns, func(t *testing.T) {
			t.Setenv("COLUMNS", columns)
			claim := "training-pvc-with-a-long-complete-identifier"
			text := captureTableOutput(t, func() {
				printPodMountTable([][]string{{"app", claim, "PVC", claim, "/training", "-", "false", "Bound", "csi://volume"}})
			})
			if strings.Count(text, claim) != 1 || strings.Contains(text, "VOLUME") || strings.Contains(text, "SUBPATH") || !strings.Contains(text, "false") {
				t.Fatal(text)
			}
		})
	}
}

func TestPodMountTableRetainsAliasesAndNeverWrapsTypes(t *testing.T) {
	t.Setenv("COLUMNS", "80")
	text := captureTableOutput(t, func() {
		printPodMountTable([][]string{
			{"app", "claim-one", "PVC", "claim-one", "/data", "train", "true", "Bound", "csi://one"},
			{"app", "config-alias", "ConfigMap", "actual-config", "/etc/app", "-", "false", "-", "-"},
		})
	})
	for _, expected := range []string{"claim-one", "config-alias", "ConfigMap", "actual-config", "train", "true", "false"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %q: %s", expected, text)
		}
	}
	if strings.Count(text, "claim-one") != 1 {
		t.Fatal(text)
	}
}
