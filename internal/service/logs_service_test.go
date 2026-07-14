package service

import (
	"testing"
	"time"
)

func TestNormalizeCloudAuditResourceType(t *testing.T) {
	tests := map[string]string{
		"vcluster":        "compute.ecp.v1.virtualCluster",
		"vc":              "compute.ecp.v1.virtualCluster",
		"node":            "compute.ecp.v1.aiComputeNode",
		"vcjob":           "vcjob",
		"custom.resource": "custom.resource",
	}
	for input, want := range tests {
		if got := normalizeCloudAuditResourceType("ECP", input); got != want {
			t.Fatalf("normalizeCloudAuditResourceType(%q) = %q, want %q", input, got, want)
		}
	}
	if got := normalizeCloudAuditResourceType("IAM", "vcluster"); got != "vcluster" {
		t.Fatalf("non-ECP resource type changed to %q", got)
	}
}

func TestNormalizeCloudAuditOperationType(t *testing.T) {
	if got := normalizeCloudAuditOperationType("vcjob", "deletevcjobs"); got != "deleteVCJobs" {
		t.Fatalf("operation type = %q", got)
	}
	if got := normalizeCloudAuditOperationType("compute.ecp.v1.virtualCluster", "customOperation"); got != "customOperation" {
		t.Fatalf("custom operation type = %q", got)
	}
}

func TestCloudAuditTimeRangeExplicitUTC8(t *testing.T) {
	start, end, err := cloudAuditTimeRange("24h", "2026-07-12 00:00:00", "2026-07-13 00:00:00")
	if err != nil {
		t.Fatal(err)
	}
	_, offset := start.Zone()
	if offset != 8*60*60 {
		t.Fatalf("start offset = %d, want UTC+8", offset)
	}
	if end.Sub(start) != 24*time.Hour {
		t.Fatalf("range = %s, want 24h", end.Sub(start))
	}
}

func TestCloudAuditTimeRangeRejectsReversedRange(t *testing.T) {
	if _, _, err := cloudAuditTimeRange("24h", "2026-07-13 00:00:00", "2026-07-12 00:00:00"); err == nil {
		t.Fatal("expected reversed range error")
	}
}

func TestCloudAuditUserIdentityResolvesUUIDUsername(t *testing.T) {
	userID := "0198ef7e-b430-7977-905f-50fb6a83cde1"
	userName, gotID := cloudAuditUserIdentity(userID, "", map[string]string{userID: "linzhouhan"})
	if userName != "linzhouhan" || gotID != userID {
		t.Fatalf("identity = %q/%q, want linzhouhan/%s", userName, gotID, userID)
	}
}

func TestCloudAuditUserIdentityKeepsNamedActor(t *testing.T) {
	userName, userID := cloudAuditUserIdentity("system:serviceaccount:kube-system:test", "", nil)
	if userName != "system:serviceaccount:kube-system:test" || userID != "" {
		t.Fatalf("identity = %q/%q", userName, userID)
	}
}

func TestCloudAuditUserIdentityFallsBackToUUID(t *testing.T) {
	id := "019d78b9-05db-7712-8da7-66cfab9d5b5c"
	userName, userID := cloudAuditUserIdentity("", id, nil)
	if userName != id || userID != id {
		t.Fatalf("identity = %q/%q, want UUID fallback", userName, userID)
	}
}
