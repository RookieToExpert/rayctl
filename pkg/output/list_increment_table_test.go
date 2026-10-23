package output

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"rayctl/internal/service"
)

type incrementalListCase struct {
	command string
	base    []string
	added   []string
	render  func(bool)
}

func incrementalListCases(empty ...bool) []incrementalListCase {
	names := []string{"z-" + strings.Repeat("resource-", 14), "a-" + strings.Repeat("resource-", 14)}
	if len(empty) > 0 && empty[0] {
		names = nil
	}
	const instant = "2026-10-23 10:11:12"
	air := &service.SSPAIRJobListResult{}
	aid := &service.SSPCatalogListResult{AID: true}
	users := []service.UserListItem{}
	groups := []service.GroupListItem{}
	afs := &service.AFSListResult{}
	vpcs := &service.VPCListResult{}
	subnets := &service.SubnetListResult{}
	nat := &service.NATGatewayListResult{}
	ecs := &service.ECSListResult{Total: 2}
	nodes := []service.NodeListItem{}
	vc := &service.VCNodeListResult{ClusterName: "vc-test"}
	queue := &service.SSPQueueNodeListResult{Queue: service.SSPQueueItem{Name: "queue-test"}}
	for _, name := range names {
		uid := "uid-" + name
		air.Items = append(air.Items, service.SSPAIRJobItem{Name: name, UID: uid, State: "RUNNING", Workspace: "ws-test", Queue: "queue-test", Creator: "creator", CreatedAt: instant})
		aid.Items = append(aid.Items, service.SSPCatalogListItem{Name: name, UID: uid, State: "RUNNING", Workspace: "ws-test", Queue: "queue-test", Creator: "creator", Node: "node-test", Resource: "cpu=4 mem=16Gi", CreatedAt: instant})
		users = append(users, service.UserListItem{Username: name, Name: "display", ID: uid, TenantCode: "tenant", CreateTime: instant})
		groups = append(groups, service.GroupListItem{Name: name, DisplayName: "display", ID: uid, PosixGroupName: "posix", TenantID: "tenant-id", CreateTime: instant})
		afs.Items = append(afs.Items, service.AFSListItem{Name: name, State: "AVAILABLE", Tenant: "tenant", CreatedAt: instant})
		vpcs.Items = append(vpcs.Items, service.VPCListItem{Name: name, State: "AVAILABLE", Tenant: "tenant"})
		subnets.Items = append(subnets.Items, service.SubnetListItem{Name: name, State: "AVAILABLE", Tenant: "tenant"})
		nat.Items = append(nat.Items, service.NATGatewayListItem{Name: name, State: "AVAILABLE", Tenant: "tenant"})
		ecs.Items = append(ecs.Items, service.ECSListItem{Name: name, UID: uid, ResourceType: "ECS", State: "RUNNING", Creator: "creator", InternalIP: "10.0.0.1", Tenant: "tenant", MachineType: "machine", VPC: "vpc-test"})
		nodes = append(nodes, service.NodeListItem{Name: name, Ready: "True", Schedulable: "Yes", Usage: "SSP", ProdRole: "ecp-private", SSPClusterName: "cluster", Model: "model", QueueName: "queue-test", Tenant: "tenant"})
		item := service.VCNodeListItem{HostName: name, HostIP: "10.0.0.1", State: "ACTIVE", Model: "model", Name: "acn-" + name, UID: uid}
		vc.Items = append(vc.Items, item)
		queue.Items = append(queue.Items, item)
	}
	return []incrementalListCase{
		{"air job list", []string{"NAME", "STATE", "READY", "WORKSPACE"}, []string{"QUEUE", "CREATOR", "CREATED"}, func(long bool) { PrintSSPAIRJobList(air, long) }},
		{"aid list", []string{"NAME", "STATE", "WORKSPACE", "QUEUE", "CREATOR", "RESTARTS"}, []string{"NODE", "RESOURCE", "CREATED"}, func(long bool) { PrintSSPAIDList(aid, long) }},
		{"user list", []string{"USERNAME", "NAME", "STATUS", "SOURCE"}, []string{"ID", "TENANT CODE", "CREATED"}, func(long bool) { PrintUserList(users, long) }},
		{"group list", []string{"GROUP", "DISPLAY NAME"}, []string{"ID", "POSIX", "TENANT ID", "CREATED"}, func(long bool) { PrintGroupList(groups, long) }},
		{"afs list", []string{"AFS", "STATE", "CAPACITY", "STORAGE CLASS", "ZONE", "CREATED"}, []string{"TENANT"}, func(long bool) { PrintAFSList(afs, long) }},
		{"vpc list", []string{"VPC", "STATE", "CIDR", "RDMA", "SUBNETS", "NATGW", "REGION"}, []string{"TENANT"}, func(long bool) { PrintVPCList(vpcs, long) }},
		{"subnet list", []string{"SUBNET", "STATE", "VPC", "CIDR", "AVAILABLE", "ZONE"}, []string{"TENANT"}, func(long bool) { PrintSubnetList(subnets, long) }},
		{"natgw list", []string{"NATGW", "STATE", "VPC", "SUBNET", "TYPE", "EIPS", "SNAT", "DNAT"}, []string{"TENANT"}, func(long bool) { PrintNATGatewayList(nat, long) }},
		{"ecs list", []string{"NAME", "TYPE", "STATE", "INTERNAL IP", "CREATOR"}, []string{"TENANT", "UID", "MACHINE", "VPC"}, func(long bool) { PrintECSList(ecs, long) }},
		{"node list", []string{"HOSTNAME", "RDY", "SCH", "RPR", "USE", "IP", "CLUSTER"}, []string{"MODEL", "PROD", "QUEUE", "TENANT"}, func(long bool) { PrintNodeList(nodes, "", 2, 1, 2, 100, true, long) }},
		{"vc node list", []string{"HOST", "IP", "STATE", "MACHINE TYPE"}, []string{"MODEL", "ACN", "ACN UID"}, func(long bool) { PrintVCNodeList(vc, long) }},
		{"queue node list", []string{"HOST", "IP", "STATE", "MACHINE TYPE", "MODEL"}, []string{"ACN UID"}, func(long bool) { PrintSSPQueueNodeList(queue, long) }},
		{"vc node list many", []string{"HOST", "VC", "IP", "STATE", "MACHINE TYPE"}, []string{"MODEL", "ACN", "ACN UID"}, func(long bool) { PrintVCNodeListMany([]*service.VCNodeListResult{vc}, long) }},
	}
}

// Get the last data table, excluding the default VC/queue overview table.
func incrementalTableRows(text string) [][]string {
	var table, result [][]string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "┌") {
			table = nil
		}
		if strings.HasPrefix(line, "│") {
			cells := strings.Split(line, "│")
			row := []string{}
			for _, cell := range cells[1 : len(cells)-1] {
				row = append(row, strings.TrimSpace(cell))
			}
			table = append(table, row)
		}
		if strings.HasPrefix(line, "└") && len(table) > 0 && table[0][0] != "FIELD" {
			result = table
		}
	}
	return result
}

func TestEveryListLongUsesOnlyPrimaryAndIncrementalColumns(t *testing.T) {
	for _, columns := range []string{"80", "200"} {
		for _, test := range incrementalListCases() {
			t.Run(columns+"/"+test.command, func(t *testing.T) {
				t.Setenv("COLUMNS", columns)
				short := incrementalTableRows(captureTableOutput(t, func() { test.render(false) }))
				long := incrementalTableRows(captureTableOutput(t, func() { test.render(true) }))
				want := append([]string{test.base[0]}, test.added...)
				if len(short) == 0 || len(long) == 0 || !reflect.DeepEqual(short[0], test.base) || !reflect.DeepEqual(long[0], want) {
					t.Fatalf("default=%v long=%v want default=%v long=%v", short, long, test.base, want)
				}
				identities := func(rows [][]string) []string {
					result := []string{}
					for _, row := range rows[1:] {
						if row[0] != "" {
							result = append(result, row[0])
						}
					}
					return result
				}
				wantOrder := []string{"z-" + strings.Repeat("resource-", 14), "a-" + strings.Repeat("resource-", 14)}
				if !reflect.DeepEqual(identities(short), wantOrder) || !reflect.DeepEqual(identities(long), wantOrder) {
					t.Fatalf("identity truncated, wrapped or reordered: default=%v long=%v", identities(short), identities(long))
				}
				for _, rows := range [][][]string{short, long} {
					for c, header := range rows[0] {
						if header == "CREATED" {
							for _, row := range rows[1:] {
								if row[0] != "" && row[c] != "2026-10-23 10:11:12" {
									t.Fatalf("date truncated or wrapped: %v", row)
								}
							}
						}
					}
				}
			})
		}
	}
}

func TestEveryListLongEmptyHeadersRemainIncremental(t *testing.T) {
	for _, test := range incrementalListCases(true) {
		t.Run(test.command, func(t *testing.T) {
			rows := incrementalTableRows(captureTableOutput(t, func() { test.render(true) }))
			want := append([]string{test.base[0]}, test.added...)
			if len(rows) == 0 || !reflect.DeepEqual(rows[0], want) {
				t.Fatalf("empty headers=%v want=%v", rows, want)
			}
		})
	}
}

func TestEveryListLongLeavesJSONUnchanged(t *testing.T) {
	for _, test := range incrementalListCases() {
		t.Run(test.command, func(t *testing.T) {
			project := func(long bool) any {
				session := BeginJSON()
				defer EndJSON()
				test.render(long)
				var buffer bytes.Buffer
				if err := session.Write(&buffer, true, 2, nil); err != nil {
					t.Fatal(err)
				}
				var result any
				if err := json.Unmarshal(buffer.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				normalizeListJSONWarnings(result)
				return result
			}
			short := project(false)
			captureTableOutput(t, func() { test.render(true) })
			long := project(true)
			if !reflect.DeepEqual(short, long) {
				t.Fatalf("table projection changed complete JSON: default=%v long=%v", short, long)
			}
		})
	}
}

// Map traversal already makes missing-field warnings unordered. Normalize
// only warning sets; preserve every item, field and item order for comparison.
func normalizeListJSONWarnings(value any) {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			if warnings, ok := child.([]any); key == "warnings" && ok {
				sort.Slice(warnings, func(i, j int) bool { return warnings[i].(string) < warnings[j].(string) })
			}
			normalizeListJSONWarnings(child)
		}
	case []any:
		for _, child := range value {
			normalizeListJSONWarnings(child)
		}
	}
}

func TestListLongDisambiguatesRepeatedNames(t *testing.T) {
	for _, sameWorkspace := range []bool{false, true} {
		for _, aid := range []bool{false, true} {
			workspaces := []string{"ws-a", "ws-b"}
			want := "WORKSPACE"
			if sameWorkspace {
				workspaces[1], want = "ws-a", "UID"
			}
			render := func() {
				if aid {
					PrintSSPAIDList(&service.SSPCatalogListResult{AID: true, Items: []service.SSPCatalogListItem{{Name: "same", Workspace: workspaces[0], UID: "uid-a"}, {Name: "same", Workspace: workspaces[1], UID: "uid-b"}}}, true)
				} else {
					PrintSSPAIRJobList(&service.SSPAIRJobListResult{Items: []service.SSPAIRJobItem{{Name: "same", Workspace: workspaces[0], UID: "uid-a"}, {Name: "same", Workspace: workspaces[1], UID: "uid-b"}}}, true)
				}
			}
			rows := incrementalTableRows(captureTableOutput(t, render))
			if len(rows) != 3 || rows[0][0] != "NAME" || rows[0][1] != want || rows[1][1] == rows[2][1] {
				t.Fatalf("repeated name is ambiguous: %v", rows)
			}
		}
	}
}

func TestListInventoryReferenceIdentifiersNeverWrap(t *testing.T) {
	t.Setenv("COLUMNS", "80")
	ref := "reference-" + strings.Repeat("resource-", 14)
	for _, render := range []func(){
		func() {
			PrintVPCList(&service.VPCListResult{Items: []service.VPCListItem{{Name: "vpc-main", NATGateways: ref}}}, false)
		},
		func() {
			PrintSubnetList(&service.SubnetListResult{Items: []service.SubnetListItem{{Name: "subnet-main", VPC: ref}}}, false)
		},
		func() {
			PrintNATGatewayList(&service.NATGatewayListResult{Items: []service.NATGatewayListItem{{Name: "nat-main", VPC: ref, Subnet: ref}}}, false)
		},
	} {
		text := captureTableOutput(t, render)
		if !strings.Contains(text, ref) {
			t.Fatalf("resource reference was truncated or wrapped:\n%s", text)
		}
	}
}
