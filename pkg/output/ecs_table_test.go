package output

import (
	"strings"
	"testing"

	"rayctl/internal/service"
)

func TestECSListCompactAndLongOutput(t *testing.T) {
	t.Setenv("COLUMNS", "350")
	result := &service.ECSListResult{
		Items: []service.ECSListItem{{ResourceType: "ECS", Name: "ecs-production-long-name", State: "RUNNING", InternalIP: "10.0.0.1", Creator: "alice", UID: "uid-a", MachineType: "flavor-a", VPC: "vpc-production", Tenant: "tenant-a"}},
		Total: 2, Truncated: true,
	}
	compact := captureListTableOutput(t, func() { PrintECSList(result, false) })
	for _, value := range []string{"TYPE", "NAME", "STATE", "INTERNAL IP", "CREATOR", "ecs-production-long-name", "RUNNING", "10.0.0.1", "alice", "匹配共 2 条", "-n 0", "本次输出 1 条。"} {
		if !strings.Contains(compact, value) {
			t.Fatalf("compact table missing %q:\n%s", value, compact)
		}
	}
	for _, value := range []string{"uid-a", "flavor-a", "vpc-production", "tenant-a", "TENANT"} {
		if strings.Contains(compact, value) {
			t.Fatalf("compact table unexpectedly contains long field %q:\n%s", value, compact)
		}
	}
	long := captureTableOutput(t, func() { PrintECSList(result, true) })
	for _, value := range []string{"UID", "MACHINE", "VPC", "uid-a", "flavor-a", "vpc-production", "TENANT", "tenant-a"} {
		if !strings.Contains(long, value) {
			t.Fatalf("long table missing %q:\n%s", value, long)
		}
	}
}

func TestECSListEmptyOutputReportsZeroResources(t *testing.T) {
	text := captureListTableOutput(t, func() { PrintECSList(&service.ECSListResult{}, false) })
	if !strings.Contains(text, "本次输出 0 条。") || strings.Contains(text, "-n 0") {
		t.Fatalf("incorrect empty inventory:\n%s", text)
	}
}

func TestECSListLongKeepsResourceIdentifiersOnOneLine(t *testing.T) {
	t.Setenv("COLUMNS", "100")
	item := service.ECSListItem{
		ResourceType: "ECS", Name: "ecs-production-long-name", State: "RUNNING", InternalIP: "10.0.0.1", Creator: "alice",
		UID: "019c08a2-ec94-7d94-a5c7-9a406d89c40d", MachineType: "c1k.2xlarge", VPC: "019950c9-a304-7855-aed7-4fda613f9f16",
	}
	text := captureTableOutput(t, func() { PrintECSList(&service.ECSListResult{Items: []service.ECSListItem{item}, Total: 1}, true) })
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, item.Name) && strings.Contains(line, item.UID) && strings.Contains(line, item.VPC) {
			return
		}
	}
	t.Fatalf("resource identifiers were split across lines:\n%s", text)
}

func TestECSDetailDisplaysDNATMappingAndLookupState(t *testing.T) {
	item := service.ECSCheckItem{Name: "ecs-demo", DNATComplete: true, DNATRules: []service.ECSDNATItem{{ExternalIP: "198.51.100.1", ExternalPort: "22225", InternalIP: "10.0.0.1", InternalPort: "22", Protocol: "TCP", State: "ACTIVE", NATGateway: "nat-demo"}}}
	text := captureTableOutput(t, func() { PrintECSCheckDetail(&service.ECSCheckResult{Items: []service.ECSCheckItem{item}}) })
	for _, value := range []string{"EXTERNAL", "INTERNAL", "PROTOCOL", "NAT GATEWAY", "198.51.100.1:22225", "10.0.0.1:22", "TCP", "ACTIVE", "nat-demo"} {
		if !strings.Contains(text, value) {
			t.Errorf("DNAT output missing %q:\n%s", value, text)
		}
	}
	item.DNATRules = nil
	text = captureTableOutput(t, func() { printECSDNAT(item) })
	if !strings.Contains(text, "未发现端口映射") {
		t.Fatal(text)
	}
	item.DNATComplete = false
	item.Warnings = []string{"DNAT: lookup unavailable"}
	text = captureTableOutput(t, func() { printECSDNAT(item) })
	if strings.Contains(text, "未发现端口映射") || !strings.Contains(text, "查询未完成") || !strings.Contains(text, "lookup unavailable") {
		t.Fatal(text)
	}
}
