package output

import (
	"strings"
	"testing"

	"rayctl/internal/service"
)

func TestResourceInventoryTenantColumnsAppearInLongOutput(t *testing.T) {
	for _, tc := range []struct {
		name   string
		render func(bool)
	}{
		{"vpc", func(long bool) {
			PrintVPCList(&service.VPCListResult{Items: []service.VPCListItem{{Name: "vpc-demo", Tenant: "tenant-demo"}}}, long)
		}},
		{"subnet", func(long bool) {
			PrintSubnetList(&service.SubnetListResult{Items: []service.SubnetListItem{{Name: "subnet-demo", Tenant: "tenant-demo"}}}, long)
		}},
		{"nat", func(long bool) {
			PrintNATGatewayList(&service.NATGatewayListResult{Items: []service.NATGatewayListItem{{Name: "nat-demo", Tenant: "tenant-demo"}}}, long)
		}},
		{"afs", func(long bool) {
			PrintAFSList(&service.AFSListResult{Items: []service.AFSListItem{{Name: "afs-demo", Tenant: "tenant-demo"}}}, long)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			short := captureTableOutput(t, func() { tc.render(false) })
			long := captureTableOutput(t, func() { tc.render(true) })
			if strings.Contains(short, "tenant-demo") || !strings.Contains(long, "TENANT") || !strings.Contains(long, "tenant-demo") {
				t.Fatalf("incorrect tenant visibility:\nshort=%s\nlong=%s", short, long)
			}
		})
	}
}
