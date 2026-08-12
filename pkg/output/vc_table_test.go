package output

import (
	"io"
	"os"
	"strings"
	"testing"

	"rayctl/internal/service"
)

func TestPrintVCOverviewSummaryOmitsResourceNamespaceTable(t *testing.T) {
	detail := &service.VCDetailResult{Name: "vc-test", UID: "uid-test", State: "RUNNING"}
	mapping := &service.ClusterGetResult{
		ControlPlaneNamespace:  "vc-uid-test",
		ResourceNamespaceCount: 1,
		ResourceNamespaces: []service.ClusterNamespaceMapping{
			{ResourceNamespace: "vcluster-default", VirtualNamespace: "default"},
		},
	}

	text := captureTableOutput(t, func() {
		PrintVCOverviewSummary(detail, mapping)
	})
	for _, expected := range []string{"vc-test", "vc-uid-test", "RESOURCE NS COUNT"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("summary output does not contain %q:\n%s", expected, text)
		}
	}
	for _, omitted := range []string{"RESOURCE NAMESPACE", "vcluster-default"} {
		if strings.Contains(text, omitted) {
			t.Fatalf("summary output unexpectedly contains %q:\n%s", omitted, text)
		}
	}
}

func TestPrintVCOverviewKeepsResourceNamespaceTable(t *testing.T) {
	detail := &service.VCDetailResult{Name: "vc-test", UID: "uid-test"}
	mapping := &service.ClusterGetResult{
		ControlPlaneNamespace:  "vc-uid-test",
		ResourceNamespaceCount: 1,
		ResourceNamespaces: []service.ClusterNamespaceMapping{
			{ResourceNamespace: "vcluster-default", VirtualNamespace: "default"},
		},
	}

	text := captureTableOutput(t, func() {
		PrintVCOverview(detail, mapping)
	})
	for _, expected := range []string{"RESOURCE NAMESPACE", "vcluster-default"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("full output does not contain %q:\n%s", expected, text)
		}
	}
}

func captureTableOutput(t *testing.T, render func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(): %v", err)
	}
	original := os.Stdout
	os.Stdout = writer
	defer func() {
		os.Stdout = original
	}()

	render()
	if err := writer.Close(); err != nil {
		t.Fatalf("close output writer: %v", err)
	}
	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close output reader: %v", err)
	}
	return string(content)
}
