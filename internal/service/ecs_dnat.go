package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"rayctl/internal/platform"
)

type ECSDNATItem struct {
	ExternalIP   string
	ExternalPort string
	InternalIP   string
	InternalPort string
	Protocol     string
	State        string
	NATGateway   string
}

func (s *ECSService) enrichDNAT(ctx context.Context, items []ECSCheckItem, ecs []platform.ECSVirtualMachine, ais []platform.AISpace) {
	type lookup struct {
		query   platform.InstanceDNATQuery
		indexes []int
	}
	lookups := make([]lookup, 0)
	add := func(kind, uid, name, profile, tenant, zone, vpc, ip string, direct []platform.DNATRule) {
		indices := make([]int, 0)
		for i := range items {
			if items[i].ResourceType == kind && items[i].Tenant == profile && ((uid != "" && items[i].UID == uid) || (uid == "" && items[i].Name == name)) {
				indices = append(indices, i)
				if ip == "" && items[i].InternalIP != "-" {
					ip = items[i].InternalIP
				}
			}
		}
		if len(indices) == 0 {
			return
		}
		lookups = append(lookups, lookup{query: platform.InstanceDNATQuery{ProfileName: profile, TenantID: tenant, Zone: zone, VPCUID: vpc, InternalIP: ip, DirectRules: direct}, indexes: indices})
	}
	for _, vm := range ecs {
		nics := vm.Properties.NetworkInterfaces
		if len(nics) == 0 {
			add("ECS", vm.UID, vm.Name, vm.ProfileName, vm.TenantID, vm.Zone, "", "", vm.Properties.DNATRules)
		}
		for _, nic := range nics {
			add("ECS", vm.UID, vm.Name, vm.ProfileName, vm.TenantID, vm.Zone, nic.Properties.VPCInfo.UID, nic.Properties.IPv4Addr, vm.Properties.DNATRules)
		}
	}
	for _, vm := range ais {
		direct := append(append([]platform.DNATRule{}, vm.Properties.DNATRules...), vm.Properties.VirtualMachineProperties.DNATRules...)
		nics := vm.Properties.VirtualMachineProperties.NetworkInterfaces
		if len(nics) == 0 {
			add("AIS", vm.UID, firstNonEmpty(vm.DisplayName, vm.Name), vm.ProfileName, vm.TenantID, vm.Zone, "", vm.Properties.HostIP, direct)
		}
		for _, nic := range nics {
			add("AIS", vm.UID, firstNonEmpty(vm.DisplayName, vm.Name), vm.ProfileName, vm.TenantID, vm.Zone, nic.Properties.VPCInfo.UID, firstNonEmpty(nic.Properties.IPv4Addr, vm.Properties.HostIP), direct)
		}
	}
	for i := range items {
		items[i].DNATRules = []ECSDNATItem{}
		items[i].Warnings = []string{}
		items[i].DNATComplete = true
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	type response struct {
		lookup lookup
		rules  []platform.DNATRule
		err    error
	}
	results := boundedMap(ctx, lookups, 4, func(ctx context.Context, item lookup) response {
		rules, err := s.vcClient.FindInstanceDNATRules(ctx, item.query)
		return response{lookup: item, rules: rules, err: err}
	})
	for _, result := range results {
		for _, i := range result.lookup.indexes {
			item := &items[i]
			if result.err != nil {
				item.DNATComplete = false
				item.Warnings = append(item.Warnings, fmt.Sprintf("DNAT: %v", result.err))
			}
			for _, rule := range result.rules {
				entry := ECSDNATItem{ExternalIP: rule.ExternalIP, ExternalPort: rule.ExternalPort, InternalIP: rule.InternalIP, InternalPort: rule.InternalPort, Protocol: strings.ToUpper(rule.Protocol), State: firstNonEmpty(rule.State, "UNKNOWN"), NATGateway: rule.NATGateway}
				duplicate := false
				for _, existing := range item.DNATRules {
					if existing == entry {
						duplicate = true
						break
					}
				}
				if !duplicate {
					item.DNATRules = append(item.DNATRules, entry)
				}
			}
		}
	}
}
