package nodehealth

import "testing"

func TestDevicePluginFailureFixture(t *testing.T) {
	logs := `2026-09-04 discovering host network devices
2026-09-04 no changes to devices for "rdma/hca"
2026-09-04 exposing "1024" devices
2026-09-04 Updating "rdma-training/roce" devices
2026-09-04 exposing "0" devices`
	exposing := parseLatestPluginRound(logs)
	if exposing[resourceHCA] != 1024 || exposing[resourceRoCE] != 0 {
		t.Fatalf("parseLatestPluginRound() = %#v", exposing)
	}

	checkpoint := `{"Data":{"RegisteredDevices":{"nvidia.com/gpu":["0","1","2","3","4","5","6","7"],"rdma-training/roce":[]}}}`
	registered, err := parseRegisteredDevices(checkpoint)
	if err != nil {
		t.Fatalf("parseRegisteredDevices() error = %v", err)
	}
	if _, exists := registered[resourceHCA]; exists {
		t.Fatalf("fixture unexpectedly contains %s: %#v", resourceHCA, registered)
	}
	if registered["nvidia.com/gpu"] != 8 {
		t.Fatalf("nvidia checkpoint count = %d, want 8", registered["nvidia.com/gpu"])
	}

	result := evaluateDeviceResource(resourceHCA, exposing[resourceHCA], 0, false, 0, true, 2)
	if result.Verdict != StatusFail {
		t.Fatalf("evaluateDeviceResource() verdict = %s, want FAIL; evidence=%#v", result.Verdict, result)
	}
	if result.Exposing == nil || *result.Exposing != 1024 || result.Checkpoint != nil || result.Capacity != 0 || result.IBPorts == nil || *result.IBPorts != 2 {
		t.Fatalf("failure evidence is incomplete: %#v", result)
	}
}

func TestEvaluateDeviceResourceNormalRoCE(t *testing.T) {
	result := evaluateDeviceResource(resourceRoCE, 1, 1, true, 1, true, 0)
	if result.Verdict != StatusOK {
		t.Fatalf("evaluateDeviceResource() = %#v, want OK", result)
	}
	if result.IBPorts != nil {
		t.Fatalf("RoCE evidence must not use IB interface count: %#v", result)
	}
}

func TestParseLatestPluginRoundUsesLastRound(t *testing.T) {
	logs := `discovering host network devices
no changes to devices for "rdma/hca"
exposing "1" devices
discovering host network devices
Updating "rdma/hca" devices
device detail
exposing "2" devices`
	result := parseLatestPluginRound(logs)
	if result[resourceHCA] != 2 {
		t.Fatalf("latest exposing = %d, want 2", result[resourceHCA])
	}
}
