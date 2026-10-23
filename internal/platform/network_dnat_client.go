package platform

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

type InstanceDNATQuery struct {
	ProfileName string
	TenantID    string
	Zone        string
	VPCUID      string
	InternalIP  string
	DirectRules []DNATRule
}

// Resource payloads can omit DNAT even when the NAT gateway has mappings.
// Query only the instance's VPC and IP, using its owning profile and tenant.
func (c *VirtualClusterClient) FindInstanceDNATRules(ctx context.Context, query InstanceDNATQuery) ([]DNATRule, error) {
	direct := normalizeDNATRules(append([]DNATRule(nil), query.DirectRules...))
	result := make([]DNATRule, 0)
	for _, rule := range direct {
		if rule.InternalIP == "" {
			rule.InternalIP = query.InternalIP
		}
		if completeDNATRule(rule) && (query.InternalIP == "" || rule.InternalIP == query.InternalIP) {
			result = append(result, rule)
		}
	}
	if len(direct) > 0 && len(result) == len(direct) {
		return result, nil
	}
	if query.VPCUID == "" || query.InternalIP == "" {
		return result, fmt.Errorf("VPC UID or internal IP unavailable for DNAT lookup")
	}
	profile, ok := c.clientProfileByName(query.ProfileName)
	if !ok {
		return result, fmt.Errorf("DNAT profile %q is unavailable", query.ProfileName)
	}
	gateways, err := c.listNATGatewaysWithFilter(ctx, profile, fmt.Sprintf(`vpc_id=%q`, query.VPCUID))
	errs := []error{}
	if err != nil {
		errs = append(errs, err)
	}
	for _, gateway := range gateways {
		if gateway.Properties.VPCInfo.UID != "" && gateway.Properties.VPCInfo.UID != query.VPCUID {
			continue
		}
		if gateway.TenantID != "" && query.TenantID != "" && gateway.TenantID != query.TenantID {
			continue
		}
		tenant := firstNonEmpty(query.TenantID, gateway.TenantID, ridSegment(gateway.ID, "subscriptions"))
		zone := firstNonEmpty(gateway.Zone, query.Zone, ridSegment(gateway.ID, "zones"))
		if tenant == "" || zone == "" || gateway.Name == "" {
			errs = append(errs, fmt.Errorf("NAT gateway %q has incomplete resource scope", gateway.Name))
			continue
		}
		endpoint, parseErr := url.Parse(profile.BaseURL)
		if parseErr != nil {
			return result, parseErr
		}
		endpoint.Host = strings.Replace(endpoint.Host, "management", "network", 1)
		endpoint.Path = fmt.Sprintf("/network/vpc/data/v2/subscriptions/%s/resourceGroups/%s/zones/%s/natGws/%s/dnatRules", url.PathEscape(tenant), url.PathEscape(firstNonEmpty(ridSegment(gateway.ID, "resourceGroups"), profile.ResourceGroup, defaultResourceGroup)), url.PathEscape(zone), url.PathEscape(gateway.Name))
		page := "1"
		seenPages := map[string]bool{}
		for {
			if seenPages[page] {
				errs = append(errs, fmt.Errorf("NAT gateway %q repeated DNAT page token", gateway.Name))
				break
			}
			seenPages[page] = true
			params := endpoint.Query()
			params.Set("rid", gateway.ID)
			params.Set("filter", fmt.Sprintf(`internal_ip=%q`, query.InternalIP))
			params.Set("page_size", "100")
			params.Set("page_token", page)
			endpoint.RawQuery = params.Encode()
			var payload struct {
				Rules         []DNATRule `json:"dnat_rules"`
				NextPageToken string     `json:"next_page_token"`
			}
			if err := c.getJSONWithProfile(ctx, profile, endpoint.String(), &payload); err != nil {
				errs = append(errs, fmt.Errorf("NAT gateway %q: %w", gateway.Name, err))
				break
			}
			for _, rule := range normalizeDNATRules(payload.Rules) {
				if rule.InternalIP != query.InternalIP {
					continue
				}
				if !completeDNATRule(rule) {
					errs = append(errs, fmt.Errorf("NAT gateway %q returned an incomplete DNAT mapping", gateway.Name))
					continue
				}
				rule.NATGateway = gateway.Name
				result = append(result, rule)
			}
			if strings.TrimSpace(payload.NextPageToken) == "" {
				break
			}
			page = strings.TrimSpace(payload.NextPageToken)
		}
	}
	seen := map[string]int{}
	unique := make([]DNATRule, 0, len(result))
	for _, rule := range result {
		key := strings.Join([]string{rule.ExternalIP, rule.ExternalPort, rule.InternalIP, rule.InternalPort, strings.ToUpper(rule.Protocol)}, "|")
		if index, ok := seen[key]; ok {
			unique[index] = rule
			continue
		}
		seen[key] = len(unique)
		unique = append(unique, rule)
	}
	sort.Slice(unique, func(i, j int) bool {
		return unique[i].ExternalIP+":"+unique[i].ExternalPort < unique[j].ExternalIP+":"+unique[j].ExternalPort
	})
	return unique, errors.Join(errs...)
}

func completeDNATRule(rule DNATRule) bool {
	return strings.TrimSpace(rule.ExternalIP) != "" && strings.TrimSpace(rule.ExternalPort) != "" && strings.TrimSpace(rule.InternalIP) != "" && strings.TrimSpace(rule.InternalPort) != ""
}

type DNATRule struct {
	NATGateway   string `json:"nat_gateway,omitempty"`
	Name         string `json:"name"`
	State        string `json:"state"`
	ExternalIP   string `json:"external_ip"`
	ExternalPort string `json:"external_port"`
	InternalIP   string `json:"internal_ip"`
	InternalPort string `json:"internal_port"`
	Protocol     string `json:"protocol"`
	Properties   struct {
		ExternalIP   string `json:"external_ip"`
		ExternalPort string `json:"external_port"`
		InternalIP   string `json:"internal_ip"`
		InternalPort string `json:"internal_port"`
		Protocol     string `json:"protocol"`
	} `json:"properties"`
	DNATRule struct {
		Name       string `json:"name"`
		State      string `json:"state"`
		Properties struct {
			ExternalIP   string `json:"external_ip"`
			ExternalPort string `json:"external_port"`
			InternalIP   string `json:"internal_ip"`
			InternalPort string `json:"internal_port"`
			Protocol     string `json:"protocol"`
		} `json:"properties"`
	} `json:"dnat_rule"`
}

func normalizeDNATRules(items []DNATRule) []DNATRule {
	for i := range items {
		items[i].Name = firstNonEmpty(items[i].Name, items[i].DNATRule.Name)
		items[i].State = firstNonEmpty(items[i].State, items[i].DNATRule.State)
		items[i].ExternalIP = firstNonEmpty(items[i].ExternalIP, items[i].Properties.ExternalIP, items[i].DNATRule.Properties.ExternalIP)
		items[i].ExternalPort = firstNonEmpty(items[i].ExternalPort, items[i].Properties.ExternalPort, items[i].DNATRule.Properties.ExternalPort)
		items[i].InternalIP = firstNonEmpty(items[i].InternalIP, items[i].Properties.InternalIP, items[i].DNATRule.Properties.InternalIP)
		items[i].InternalPort = firstNonEmpty(items[i].InternalPort, items[i].Properties.InternalPort, items[i].DNATRule.Properties.InternalPort)
		items[i].Protocol = firstNonEmpty(items[i].Protocol, items[i].Properties.Protocol, items[i].DNATRule.Properties.Protocol)
	}
	return items
}
