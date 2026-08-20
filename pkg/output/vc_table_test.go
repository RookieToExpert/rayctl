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

func TestPrintVCNodeListDefaultColumns(t *testing.T) {
	result := &service.VCNodeListResult{
		ClusterName: "vc-test",
		Items: []service.VCNodeListItem{{
			HostName:    "host-10-0-0-1",
			HostIP:      "10.0.0.1",
			State:       "ACTIVE",
			MachineType: "h2ls.ru.k10",
			Model:       "module-910c-8",
			Name:        "acn-test",
			UID:         "acn-uid-test",
		}},
	}

	text := captureTableOutput(t, func() { PrintVCNodeList(result, false) })
	for _, expected := range []string{"HOST", "IP", "STATE", "MACHINE TYPE", "host-10-0-0-1"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("default output does not contain %q:\n%s", expected, text)
		}
	}
	for _, omitted := range []string{"MODEL", "ACN UID", "module-910c-8", "acn-uid-test"} {
		if strings.Contains(text, omitted) {
			t.Fatalf("default output unexpectedly contains %q:\n%s", omitted, text)
		}
	}
}

func TestPrintVCNodeListLongOnlyShowsModelAndACN(t *testing.T) {
	result := &service.VCNodeListResult{
		ClusterName: "vc-test",
		Items: []service.VCNodeListItem{{
			HostName:    "host-10-0-0-1",
			HostIP:      "10.0.0.1",
			State:       "ACTIVE",
			MachineType: "h2ls.ru.k10",
			Model:       "module-910c-8",
			Name:        "acn-with-a-complete-name",
			UID:         "acn-uid-test",
		}},
	}

	text := captureTableOutput(t, func() { PrintVCNodeList(result, true) })
	for _, expected := range []string{"HOST", "MODEL", "ACN", "ACN UID", "host-10-0-0-1", "module-910c-8", "acn-with-a-complete-name", "acn-uid-test"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("long output does not contain %q:\n%s", expected, text)
		}
	}
	for _, omitted := range []string{"IP", "STATE", "MACHINE TYPE", "10.0.0.1", "h2ls.ru.k10"} {
		if strings.Contains(text, omitted) {
			t.Fatalf("long output unexpectedly contains %q:\n%s", omitted, text)
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
