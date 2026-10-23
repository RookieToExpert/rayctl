package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"rayctl/internal/service"
)

func TestPrintWorkspaceWorkloadsShowsQueueAndWorkspaceSources(t *testing.T) {
	results := []*service.SSPWorkspaceWorkloadResult{nil, {
		Workspace: service.SSPWorkspaceItem{Name: "ws-one"},
		Items: []service.SSPQueueWorkloadItem{
			{Queue: "queue-one", Type: "trainingJob", Name: "same-job", State: "Running", Creator: "tester", Resources: "worker x 1 32C"},
			{Queue: "queue-two", Type: "aid", Name: "same-job", Workspace: "ws-two"},
		},
		Warnings: []string{"skipped queue-offline"},
	}}
	body := captureTableOutput(t, func() { PrintSSPWorkspaceWorkloads(results) })
	for _, expected := range []string{"QUEUE", "WORKSPACE", "queue-one", "queue-two", "ws-one", "ws-two", "JOB", "AID", "Running", "tester"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q in workspace workloads:\n%s", expected, body)
		}
	}
	if strings.Count(body, "same-job") != 2 {
		t.Fatalf("workloads with the same name in different queues were lost:\n%s", body)
	}
	if strings.Contains(body, "RESOURCES") || strings.Contains(body, "worker x 1 32C") {
		t.Fatalf("workload summary includes resource detail:\n%s", body)
	}
}

func TestPrintVCWorkloadsShowsVCForMultipleTargets(t *testing.T) {
	results := []*service.VCWorkloadResult{{
		ClusterName: "vc-one",
		Items:       []service.SSPQueueWorkloadItem{{Queue: "queue-one", Type: "vcJob", Name: "same-job", Workspace: "ws-one", Resources: "32C"}},
	}, nil, {
		ClusterName: "vc-two",
		Items:       []service.SSPQueueWorkloadItem{{Queue: "queue-two", Type: "trainingJob", Name: "same-job", Workspace: "ws-two"}},
	}}
	body := captureTableOutput(t, func() { PrintVCWorkloads(results) })
	for _, expected := range []string{"VC", "QUEUE", "vc-one", "vc-two", "queue-one", "queue-two", "ws-one", "ws-two", "VCJOB", "JOB"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q in VC workloads:\n%s", expected, body)
		}
	}
	if strings.Contains(body, "RESOURCES") || strings.Contains(body, "32C") {
		t.Fatalf("VC workload summary includes resource detail:\n%s", body)
	}
	single := captureTableOutput(t, func() { PrintVCWorkloads(results[:1]) })
	if strings.Contains(single, "│ VC ") || !strings.Contains(single, "QUEUE") {
		t.Fatalf("single VC table should have a queue source without a VC column:\n%s", single)
	}
}

func TestWorkloadTablesPreserveEmptyScopesAndHandleNil(t *testing.T) {
	workspace := captureTableOutput(t, func() {
		PrintSSPWorkspaceWorkloads([]*service.SSPWorkspaceWorkloadResult{nil, {Workspace: service.SSPWorkspaceItem{Name: "ws-empty"}}})
	})
	if !strings.Contains(workspace, "ws-empty") || !strings.Contains(workspace, "QUEUE") {
		t.Fatalf("empty workspace scope missing:\n%s", workspace)
	}
	vc := captureTableOutput(t, func() {
		PrintVCWorkloads([]*service.VCWorkloadResult{nil, {ClusterName: "vc-empty-one"}, {ClusterName: "vc-empty-two"}})
	})
	if !strings.Contains(vc, "vc-empty-one") || !strings.Contains(vc, "vc-empty-two") {
		t.Fatalf("empty VC scopes missing:\n%s", vc)
	}
	for name, render := range map[string]func(){
		"workspace nil": func() { PrintSSPWorkspaceWorkloads(nil) },
		"VC nil":        func() { PrintVCWorkloads(nil) },
		"workspace null entries": func() {
			PrintSSPWorkspaceWorkloads([]*service.SSPWorkspaceWorkloadResult{nil})
		},
		"VC null entries": func() { PrintVCWorkloads([]*service.VCWorkloadResult{nil}) },
	} {
		t.Run(name, func(t *testing.T) {
			body := captureTableOutput(t, render)
			if !strings.Contains(body, "QUEUE") || !strings.Contains(body, "NAME") {
				t.Fatalf("empty table missing headers:\n%s", body)
			}
		})
	}
}

func TestWorkspaceWorkloadsJSONPreservesUIDContextWarningsAndEmptyScope(t *testing.T) {
	session := BeginJSON()
	defer EndJSON()
	body := captureTableOutput(t, func() {
		PrintSSPWorkspaceWorkloads([]*service.SSPWorkspaceWorkloadResult{nil, {
			Workspace: service.SSPWorkspaceItem{Name: "ws-one", UID: "workspace-uid"},
			Items: []service.SSPQueueWorkloadItem{{
				Queue: "queue-one", Name: "job-one", UID: "workload-uid", Resources: "worker x 1 32C", Creator: "password: hidden",
			}},
			Warnings: []string{"skipped queue-offline"},
		}, {Workspace: service.SSPWorkspaceItem{Name: "ws-empty", UID: "empty-workspace-uid"}}})
	})
	if body != "" {
		t.Fatalf("JSON capture printed a table: %s", body)
	}
	var buffer bytes.Buffer
	if err := session.Write(&buffer, true, 2, nil); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Items []struct {
			UID       string `json:"uid"`
			Queue     string `json:"queue"`
			Resources string `json:"resources"`
			Context   struct {
				Workspace struct {
					Name string `json:"name"`
					UID  string `json:"uid"`
				} `json:"workspace"`
			} `json:"context"`
		} `json:"items"`
		Metadata []struct {
			Workspace struct {
				Name string `json:"name"`
				UID  string `json:"uid"`
			} `json:"workspace"`
		} `json:"metadata"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(buffer.Bytes(), &payload); err != nil {
		t.Fatal(err, buffer.String())
	}
	if len(payload.Items) != 1 || payload.Items[0].UID != "workload-uid" || payload.Items[0].Queue != "queue-one" || payload.Items[0].Resources != "worker x 1 32C" || payload.Items[0].Context.Workspace.UID != "workspace-uid" {
		t.Fatalf("workload identity or context missing: %s", buffer.String())
	}
	if len(payload.Metadata) != 2 || payload.Metadata[1].Workspace.Name != "ws-empty" || payload.Metadata[1].Workspace.UID != "empty-workspace-uid" {
		t.Fatalf("empty workspace scope missing: %s", buffer.String())
	}
	if len(payload.Warnings) != 1 || payload.Warnings[0] != "skipped queue-offline" || strings.Contains(buffer.String(), "hidden") {
		t.Fatalf("warnings or safe capture failed: %s", buffer.String())
	}
}

func TestVCWorkloadsJSONPreservesScopeIdentityAndEmptyScope(t *testing.T) {
	session := BeginJSON()
	defer EndJSON()
	PrintVCWorkloads([]*service.VCWorkloadResult{nil, {
		ClusterName: "vc-one", ClusterUID: "vc-uid", ProfileName: "ssp-one",
		Items:    []service.SSPQueueWorkloadItem{{Queue: "queue-one", UID: "workload-uid"}},
		Warnings: []string{"skipped queue-offline"},
	}, {ClusterName: "vc-empty", ClusterUID: "empty-vc-uid", ProfileName: "ssp-two"}})
	var buffer bytes.Buffer
	if err := session.Write(&buffer, true, 2, nil); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Items []struct {
			UID     string `json:"uid"`
			Context struct {
				ClusterName string `json:"cluster_name"`
				ClusterUID  string `json:"cluster_uid"`
				ProfileName string `json:"profile_name"`
			} `json:"context"`
		} `json:"items"`
		Metadata []struct {
			ClusterName string `json:"cluster_name"`
			ClusterUID  string `json:"cluster_uid"`
			ProfileName string `json:"profile_name"`
		} `json:"metadata"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(buffer.Bytes(), &payload); err != nil {
		t.Fatal(err, buffer.String())
	}
	if len(payload.Items) != 1 || payload.Items[0].UID != "workload-uid" || payload.Items[0].Context.ClusterName != "vc-one" || payload.Items[0].Context.ClusterUID != "vc-uid" || payload.Items[0].Context.ProfileName != "ssp-one" {
		t.Fatalf("VC workload identity or context missing: %s", buffer.String())
	}
	if len(payload.Metadata) != 2 || payload.Metadata[1].ClusterUID != "empty-vc-uid" || payload.Metadata[1].ProfileName != "ssp-two" {
		t.Fatalf("empty VC scope metadata missing: %s", buffer.String())
	}
	if len(payload.Warnings) != 1 || payload.Warnings[0] != "skipped queue-offline" {
		t.Fatalf("VC warnings missing: %s", buffer.String())
	}
}
