package output

import (
	"io"
	"os"
	"strings"
	"testing"

	"rayctl/internal/platform"
	"rayctl/internal/service"
)

func TestPrintVCConfigShowsDesiredAppliedAndActual(t *testing.T) {
	config := &service.VCControlPlaneConfig{
		Name: "vc-demo", UID: "vc-uid", Environment: "pt", HostNamespace: "vc-vc-uid",
		AppliedFlavor: "medium", FlavorSource: "VCluster2ControlPlane label",
		Desired:    &platform.VCControlPlaneFlavor{Name: "large", MaxNodeCount: 500},
		Deployment: &service.VCControlPlaneDeployment{VCControlPlaneSpec: service.VCControlPlaneSpec{Replicas: 3, Requests: map[string]string{"cpu": "8", "memory": "32Gi"}, Limits: map[string]string{"cpu": "8", "memory": "32Gi"}}, Ready: 3, Updated: 2},
	}
	for _, render := range []func(){func() { PrintVCConfig(config) }, func() { PrintVCOverview(&service.VCDetailResult{Name: "vc-demo", ControlPlane: config}, nil) }} {
		text := captureTableOutput(t, render)
		for _, expected := range []string{"DESIRED FLAVOR", "large", "medium", "500", "3/3 (updated 2)", "32Gi", "未收敛"} {
			if !strings.Contains(text, expected) {
				t.Fatalf("missing %q: %s", expected, text)
			}
		}
	}
}

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
		writer.Close()
		reader.Close()
	}()
	type captured struct {
		content []byte
		err     error
	}
	done := make(chan captured, 1)
	go func() {
		content, err := io.ReadAll(reader)
		done <- captured{content, err}
	}()

	render()
	if err := writer.Close(); err != nil {
		t.Fatalf("close output writer: %v", err)
	}
	result := <-done
	if result.err != nil {
		t.Fatalf("read output: %v", result.err)
	}
	return string(result.content)
}
