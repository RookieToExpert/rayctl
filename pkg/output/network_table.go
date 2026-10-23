package output

import (
	"rayctl/internal/service"
	"strconv"
)

func printInventoryWithTenants(headers []string, rows [][]string, widths []int, options tableOptions, tenants []string, longOutput []bool) {
	if len(longOutput) > 0 && longOutput[0] {
		headers = append(headers, "TENANT")
		widths = append(widths, 36)
		options.minWidths = append(options.minWidths, 8)
		for i := range rows {
			tenant := "-"
			if i < len(tenants) {
				tenant = emptyDash(tenants[i])
			}
			rows[i] = append(rows[i], tenant)
		}
	}
	printBoxTableWithOptions(headers, rows, widths, options)
}

func PrintVPCList(result *service.VPCListResult, longOutput ...bool) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}
	tenants := make([]string, 0, len(result.Items))
	rows := make([][]string, 0, maxInt(1, len(result.Items)))
	for _, item := range result.Items {
		tenants = append(tenants, item.Tenant)
		rows = append(rows, []string{
			emptyDash(item.Name), emptyDash(item.State), emptyDash(item.CIDR), emptyDash(item.RDMA),
			strconv.Itoa(item.SubnetCount), emptyDash(item.NATGateways), emptyDash(item.Region),
		})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-", "-", "-", "-"})
	}
	printInventoryWithTenants(
		[]string{"VPC", "STATE", "CIDR", "RDMA", "SUBNETS", "NATGW", "REGION"},
		rows,
		[]int{32, 10, 20, 6, 8, 28, 12},
		tableOptions{minWidths: []int{16, 8, 12, 4, 7, 10, 8}},
		tenants, longOutput,
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
		{"TENANT", emptyDash(result.Tenant)},
		{"TENANT ID", emptyDash(result.TenantID)},
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

func PrintSubnetList(result *service.SubnetListResult, longOutput ...bool) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}
	tenants := make([]string, 0, len(result.Items))
	rows := make([][]string, 0, maxInt(1, len(result.Items)))
	for _, item := range result.Items {
		tenants = append(tenants, item.Tenant)
		rows = append(rows, []string{
			emptyDash(item.Name), emptyDash(item.State), emptyDash(item.VPC), emptyDash(item.CIDR),
			emptyDash(item.AvailableIPs), emptyDash(item.Zone),
		})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-", "-", "-"})
	}
	printInventoryWithTenants(
		[]string{"SUBNET", "STATE", "VPC", "CIDR", "AVAILABLE", "ZONE"},
		rows,
		[]int{32, 10, 28, 20, 10, 12},
		tableOptions{minWidths: []int{18, 8, 14, 12, 9, 8}},
		tenants, longOutput,
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
		{"TENANT", emptyDash(result.Tenant)},
		{"TENANT ID", emptyDash(result.TenantID)},
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

func PrintNATGatewayList(result *service.NATGatewayListResult, longOutput ...bool) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}
	tenants := make([]string, 0, len(result.Items))
	rows := make([][]string, 0, maxInt(1, len(result.Items)))
	for _, item := range result.Items {
		tenants = append(tenants, item.Tenant)
		rows = append(rows, []string{
			emptyDash(item.Name), emptyDash(item.State), emptyDash(item.VPC), emptyDash(item.Subnet),
			emptyDash(item.GatewayType), strconv.Itoa(item.EIPCount), strconv.Itoa(item.SNATCount), strconv.Itoa(item.DNATCount),
		})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-", "-", "-", "-", "-"})
	}
	printInventoryWithTenants(
		[]string{"NATGW", "STATE", "VPC", "SUBNET", "TYPE", "EIPS", "SNAT", "DNAT"},
		rows,
		[]int{30, 10, 26, 26, 10, 6, 6, 6},
		tableOptions{minWidths: []int{16, 8, 12, 12, 6, 5, 5, 5}},
		tenants, longOutput,
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
		{"TENANT", emptyDash(result.Tenant)},
		{"TENANT ID", emptyDash(result.TenantID)},
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
