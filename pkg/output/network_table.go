package output

import (
	"rayctl/internal/service"
	"strconv"
)

func PrintVPCList(result *service.VPCListResult) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}
	rows := make([][]string, 0, maxInt(1, len(result.Items)))
	for _, item := range result.Items {
		rows = append(rows, []string{
			emptyDash(item.Name), emptyDash(item.State), emptyDash(item.CIDR), emptyDash(item.RDMA),
			strconv.Itoa(item.SubnetCount), emptyDash(item.NATGateways), emptyDash(item.Region),
		})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-", "-", "-", "-"})
	}
	printBoxTableWithOptions(
		[]string{"VPC", "STATE", "CIDR", "RDMA", "SUBNETS", "NATGW", "REGION"},
		rows,
		[]int{32, 10, 20, 6, 8, 28, 12},
		tableOptions{minWidths: []int{16, 8, 12, 4, 7, 10, 8}},
	)
}

func PrintVPCDetail(result *service.VPCListItem) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}
	printResourceDetail([][]string{
		{"VPC", emptyDash(result.Name)},
		{"UID", emptyDash(result.UID)},
		{"STATE", emptyDash(result.State)},
		{"CIDR", emptyDash(result.CIDR)},
		{"RDMA", emptyDash(result.RDMA)},
		{"DEFAULT", emptyDash(result.Default)},
		{"SUBNET COUNT", strconv.Itoa(result.SubnetCount)},
		{"NAT GATEWAY", emptyDash(result.NATGateways)},
		{"REGION", emptyDash(result.Region)},
		{"CREATED", emptyDash(result.CreatedAt)},
		{"UPDATED", emptyDash(result.UpdatedAt)},
	})
}

func PrintSubnetList(result *service.SubnetListResult) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}
	rows := make([][]string, 0, maxInt(1, len(result.Items)))
	for _, item := range result.Items {
		rows = append(rows, []string{
			emptyDash(item.Name), emptyDash(item.State), emptyDash(item.VPC), emptyDash(item.CIDR),
			emptyDash(item.AvailableIPs), emptyDash(item.Zone),
		})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-", "-", "-"})
	}
	printBoxTableWithOptions(
		[]string{"SUBNET", "STATE", "VPC", "CIDR", "AVAILABLE", "ZONE"},
		rows,
		[]int{32, 10, 28, 20, 10, 12},
		tableOptions{minWidths: []int{18, 8, 14, 12, 9, 8}},
	)
}

func PrintSubnetDetail(result *service.SubnetListItem) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}
	printResourceDetail([][]string{
		{"SUBNET", emptyDash(result.Name)},
		{"UID", emptyDash(result.UID)},
		{"STATE", emptyDash(result.State)},
		{"VPC", emptyDash(result.VPC)},
		{"CIDR", emptyDash(result.CIDR)},
		{"GATEWAY", emptyDash(result.Gateway)},
		{"AVAILABLE IPS", emptyDash(result.AvailableIPs)},
		{"SCOPE", emptyDash(result.Scope)},
		{"PROVIDER", emptyDash(result.Provider)},
		{"NETWORK TYPE", emptyDash(result.NetworkType)},
		{"ZONE", emptyDash(result.Zone)},
		{"CREATED", emptyDash(result.CreatedAt)},
		{"UPDATED", emptyDash(result.UpdatedAt)},
	})
}

func PrintNATGatewayList(result *service.NATGatewayListResult) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}
	rows := make([][]string, 0, maxInt(1, len(result.Items)))
	for _, item := range result.Items {
		rows = append(rows, []string{
			emptyDash(item.Name), emptyDash(item.State), emptyDash(item.VPC), emptyDash(item.Subnet),
			emptyDash(item.GatewayType), strconv.Itoa(item.EIPCount), strconv.Itoa(item.SNATCount), strconv.Itoa(item.DNATCount),
		})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-", "-", "-", "-", "-"})
	}
	printBoxTableWithOptions(
		[]string{"NATGW", "STATE", "VPC", "SUBNET", "TYPE", "EIPS", "SNAT", "DNAT"},
		rows,
		[]int{30, 10, 26, 26, 10, 6, 6, 6},
		tableOptions{minWidths: []int{16, 8, 12, 12, 6, 5, 5, 5}},
	)
}

func PrintNATGatewayDetail(result *service.NATGatewayListItem) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}
	printResourceDetail([][]string{
		{"NAT GATEWAY", emptyDash(result.Name)},
		{"UID", emptyDash(result.UID)},
		{"STATE", emptyDash(result.State)},
		{"VPC", emptyDash(result.VPC)},
		{"SUBNET", emptyDash(result.Subnet)},
		{"TYPE", emptyDash(result.GatewayType)},
		{"EIP", emptyDash(result.EIPs)},
		{"SNAT RULES", strconv.Itoa(result.SNATCount)},
		{"DNAT RULES", strconv.Itoa(result.DNATCount)},
		{"ZONE", emptyDash(result.Zone)},
		{"CREATED", emptyDash(result.CreatedAt)},
		{"UPDATED", emptyDash(result.UpdatedAt)},
	})
}
