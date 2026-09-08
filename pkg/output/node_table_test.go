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

	text := captureTableOutput(t, func() { PrintNodeDescribe(details, false, nil) })
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

	text := captureTableOutput(t, func() { PrintNodeDescribe(details, false, nil) })
	if strings.Contains(text, "RDMA") {
		t.Fatalf("RDMA column should not be rendered:\n%s", text)
	}
}
