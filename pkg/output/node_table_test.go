package output

import (
	"strings"
	"testing"

	"rayctl/internal/service"
)

func TestPrintNodeDescribeShowsRDMAInSummaryTable(t *testing.T) {
	details := &service.NodeDescribe{
		Hostname: "host-rdma",
		RDMAResources: []service.NodeExtendedResource{{
			Name: "rdma-training/roce", Allocatable: "1", Capacity: "1", Requested: "0",
		}},
	}

	text := captureTableOutput(t, func() { PrintNodeDescribe(details, false, nil, false) })
	if !strings.Contains(text, "RDMA") || !strings.Contains(text, "0/1") {
		t.Fatalf("RDMA summary is missing:\n%s", text)
	}
	if strings.Contains(text, "roce 0/1") {
		t.Fatalf("RDMA resource name should not be rendered:\n%s", text)
	}
	if strings.Contains(text, "RDMA RESOURCE") {
		t.Fatalf("separate RDMA table should not be rendered:\n%s", text)
	}
}

func TestPrintNodeDescribeHidesRDMAColumnWithoutUsableResource(t *testing.T) {
	details := &service.NodeDescribe{Hostname: "host-no-rdma"}

	text := captureTableOutput(t, func() { PrintNodeDescribe(details, false, nil, false) })
	if strings.Contains(text, "RDMA") {
		t.Fatalf("RDMA column should not be rendered:\n%s", text)
	}
}

func TestPrintNodeDescribeECSBusinessNames(t *testing.T) {
	t.Setenv("COLUMNS", "140")
	pod := "virt-launcher-ecs-b397575f3132f5a8-z9cvj"
	details := &service.NodeDescribe{
		Hostname: "host-10-12-104-6", Pods: []string{"hp-volume-m8qs4", pod},
		PodDetails: []service.NodePodDetail{
			{Pod: "hp-volume-m8qs4"},
			{Pod: pod, Type: "ECS", Workload: "ecs-yunwei"},
		},
	}
	text := captureTableOutput(t, func() { PrintNodeDescribe(details, false, nil, false) })
	for _, value := range []string{pod, "hp-volume-m8qs4", "TYPE", "WORKLOAD", "ecs-yunwei", "ECS"} {
		if !strings.Contains(text, value) {
			t.Fatalf("missing %q in default ECS view:\n%s", value, text)
		}
	}
}

func TestPrintNodeDescribeKeepsPodListWithoutECS(t *testing.T) {
	details := &service.NodeDescribe{Pods: []string{"training-worker"}, PodDetails: []service.NodePodDetail{{Pod: "training-worker", Type: "ATE", Workload: "training"}}}
	text := captureTableOutput(t, func() { PrintNodeDescribe(details, false, nil, false) })
	if !strings.Contains(text, "training-worker") || strings.Contains(text, "WORKLOAD") {
		t.Fatalf("unexpected default non-ECS view:\n%s", text)
	}
}
