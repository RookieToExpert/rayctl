package output

import (
	"strings"
	"testing"

	"rayctl/internal/platform"
	"rayctl/internal/service"
)

func TestPrintVCResourceUsageHidesAcceleratorForCPUOnlyNodes(t *testing.T) {
	cpuUsage := platform.VirtualClusterNodeResourceUsage{}
	cpuUsage.Usage.Total.CPU = "255.9"
	cpuUsage.Usage.Total.Memory = "1005GiB"
	result := &service.VCResourceUsageResult{Items: []service.VCNodeResourceUsageItem{{HostName: "cpu-node", Usage: cpuUsage}}}

	text := captureTableOutput(t, func() { PrintVCResourceUsage([]*service.VCResourceUsageResult{result}) })

	if strings.Contains(text, "ACCEL") {
		t.Fatalf("CPU-only output contains ACCEL column:\n%s", text)
	}
	for _, expected := range []string{"CPU ALLOC/TOTAL", "MEMORY ALLOC/TOTAL"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %q:\n%s", expected, text)
		}
	}
}

func TestPrintVCResourceUsageKeepsAcceleratorForMixedNodes(t *testing.T) {
	gpuUsage := platform.VirtualClusterNodeResourceUsage{}
	gpuUsage.Usage.Total.Device = "8"
	result := &service.VCResourceUsageResult{Items: []service.VCNodeResourceUsageItem{{HostName: "gpu-node", Usage: gpuUsage}}}

	text := captureTableOutput(t, func() { PrintVCResourceUsage([]*service.VCResourceUsageResult{result}) })

	if !strings.Contains(text, "ACCEL ALLOC/TOTAL") {
		t.Fatalf("accelerator output is missing ACCEL column:\n%s", text)
	}
}
