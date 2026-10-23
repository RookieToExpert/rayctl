package platform

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// VCKubeconfigRef retains the original tenant and resource scope, not just a name.
type VCKubeconfigRef struct {
	Name          string `json:"name"`
	UID           string `json:"uid"`
	Profile       string `json:"profile"`
	Subscription  string `json:"subscription"`
	ResourceGroup string `json:"resource_group"`
	Region        string `json:"region"`
	Environment   string `json:"environment"`
}

func (c *VirtualClusterClient) VCKubeconfigInventory(ctx context.Context, environment string) ([]VCKubeconfigRef, []error) {
	if c == nil || len(c.profiles) == 0 {
		return nil, []error{fmt.Errorf("VC inventory unavailable: no configured profiles")}
	}
	if environment == "" || environment == "auto" {
		environment = c.ProfileEnvironment(c.CurrentProfileName())
	}
	if environment != "d" && environment != "pt" && environment != "dcloud" && environment != "all" {
		return nil, []error{fmt.Errorf("VC inventory unavailable: unknown environment %q", environment)}
	}
	refs := []VCKubeconfigRef{}
	failures := []error{}
	seen := map[string]bool{}
	selectedProfiles := 0
	for _, p := range c.orderedProfiles() {
		// D/PT share tenants; resources are selected by their own region below.
		if environment != "all" && (profileEnvironment(p) == "dcloud") != (environment == "dcloud") {
			continue
		}
		selectedProfiles++
		items, err := c.listVCKubeconfigInventoryForProfile(ctx, p)
		if err != nil {
			failures = append(failures, fmt.Errorf("profile %s inventory unavailable", p.Name))
			continue
		}
		for _, vc := range items {
			if strings.EqualFold(vc.State, "DELETED") {
				continue
			}
			subscription := firstNonEmpty(ridSegment(vc.ID, "subscriptions"), vc.TenantID)
			if vc.UID == "" || vc.Name == "" || subscription == "" || vc.Region == "" {
				failures = append(failures, fmt.Errorf("profile %s: incomplete VC identity for %s", p.Name, vc.Name))
				continue
			}
			env := "d"
			if profileEnvironment(p) == "dcloud" {
				env = "dcloud"
			} else if vc.Region == "cn-pj-03" {
				env = "pt"
			}
			if environment != "all" && environment != env {
				continue
			}
			ref := VCKubeconfigRef{Name: vc.Name, UID: vc.UID, Profile: p.Name, Subscription: subscription, ResourceGroup: firstNonEmpty(ridSegment(vc.ID, "resourceGroups"), p.ResourceGroup, "default"), Region: vc.Region, Environment: env}
			key := env + "|" + ref.Subscription + "|" + ref.UID
			if !seen[key] {
				refs = append(refs, ref)
				seen[key] = true
			}
		}
	}
	if selectedProfiles == 0 {
		failures = append(failures, fmt.Errorf("VC inventory unavailable: no profiles for environment %s", environment))
	}
	return refs, failures
}

// Inventory completeness controls local cleanup, so malformed or truncated
// responses must not be interpreted as an authoritative empty resource list.
func (c *VirtualClusterClient) listVCKubeconfigInventoryForProfile(ctx context.Context, profile clientProfile) ([]VirtualCluster, error) {
	endpoint, err := url.Parse(profile.BaseURL)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" {
		return nil, fmt.Errorf("invalid HTTPS platform endpoint")
	}
	clusters := make([]VirtualCluster, 0)
	seenTokens := map[string]bool{}
	seenPages := map[string]bool{}
	seenUIDs := map[string]bool{}
	skip, token := 0, ""
	var total *int
	for {
		reqURL, _ := url.Parse(c.virtualClusterListURL(profile, skip))
		query := reqURL.Query()
		if token != "" {
			query.Set("page_token", token)
		}
		reqURL.RawQuery = query.Encode()
		var payload struct {
			VirtualClusters []VirtualCluster `json:"virtual_clusters"`
			Items           []VirtualCluster `json:"items"`
			NextPageToken   string           `json:"next_page_token"`
			TotalSize       *int             `json:"total_size"`
		}
		if err := c.getJSONWithProfile(ctx, profile, reqURL.String(), &payload); err != nil {
			return nil, fmt.Errorf("list VC inventory: %w", err)
		}
		items := payload.VirtualClusters
		if items == nil {
			items = payload.Items
		}
		if items == nil {
			return nil, fmt.Errorf("VC inventory response has no non-null resource array")
		}
		if payload.TotalSize != nil {
			if *payload.TotalSize < 0 || (total != nil && *total != *payload.TotalSize) {
				return nil, fmt.Errorf("VC inventory total_size is inconsistent")
			}
			total = payload.TotalSize
		}
		count := skip + len(items)
		if total != nil && count > *total {
			return nil, fmt.Errorf("VC inventory exceeds total_size")
		}
		more := payload.NextPageToken != "" || (total != nil && count < *total)
		if len(items) == 0 {
			if more {
				return nil, fmt.Errorf("VC inventory is incomplete: empty page before pagination finished")
			}
			return clusters, nil
		}
		page, _ := json.Marshal(items)
		if seenPages[string(page)] {
			return nil, fmt.Errorf("VC inventory is incomplete: repeated page")
		}
		seenPages[string(page)] = true
		for _, item := range items {
			uid := strings.TrimSpace(item.UID)
			if uid != "" && seenUIDs[uid] {
				return nil, fmt.Errorf("VC inventory is incomplete: resource repeated across pages")
			}
			seenUIDs[uid] = true
		}
		clusters = append(clusters, items...)
		if !more && (total != nil || len(items) < defaultPageLimit) {
			return clusters, nil
		}
		if payload.NextPageToken != "" {
			if seenTokens[payload.NextPageToken] {
				return nil, fmt.Errorf("VC inventory is incomplete: repeated page token")
			}
			seenTokens[payload.NextPageToken] = true
		}
		skip = count
		token = payload.NextPageToken
	}
}

func (c *VirtualClusterClient) vcCredentialBase(ref VCKubeconfigRef) (clientProfile, string, error) {
	p, ok := c.clientProfileByName(ref.Profile)
	if !ok {
		return p, "", fmt.Errorf("profile %s unavailable", ref.Profile)
	}
	u, err := url.Parse(p.BaseURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return p, "", fmt.Errorf("invalid HTTPS platform endpoint")
	}
	u.Path = "/compute/ecp/v1/subscriptions/" + url.PathEscape(ref.Subscription) + "/resourceGroups/" + url.PathEscape(ref.ResourceGroup) + "/regions/" + url.PathEscape(ref.Region) + "/virtualClusters/" + url.PathEscape(ref.Name)
	u.RawQuery = ""
	return p, u.String(), nil
}

// Only a matching UID with an explicit deleted state is affirmative deletion evidence.
// HTTP errors (including 403/404) and missing inventory are never deletion evidence.
func (c *VirtualClusterClient) VCKubeconfigDeleted(ctx context.Context, ref VCKubeconfigRef) (bool, error) {
	p, base, err := c.vcCredentialBase(ref)
	if err != nil {
		return false, err
	}
	var d struct {
		UID        string `json:"uid"`
		State      string `json:"state"`
		Properties struct {
			State string `json:"state"`
		} `json:"properties"`
	}
	if err = c.getJSONWithProfile(ctx, p, base, &d); err != nil {
		return false, fmt.Errorf("VC detail unavailable; preserving local config")
	}
	return d.UID == ref.UID && strings.EqualFold(firstNonEmpty(d.State, d.Properties.State), "DELETED"), nil
}

func (c *VirtualClusterClient) DownloadVCKubeconfig(ctx context.Context, ref VCKubeconfigRef) ([]byte, error) {
	p, base, err := c.vcCredentialBase(ref)
	if err != nil {
		return nil, err
	}
	var detail struct {
		UID        string `json:"uid"`
		Properties struct {
			Endpoints struct {
				Public []string `json:"public_endpoints"`
			} `json:"endpoints_config"`
		} `json:"properties"`
	}
	if err = c.getJSONWithProfile(ctx, p, base, &detail); err != nil {
		return nil, fmt.Errorf("VC detail unavailable (check profile permissions)")
	}
	if detail.UID != ref.UID {
		return nil, fmt.Errorf("VC UID changed; refusing credential download")
	}
	if len(detail.Properties.Endpoints.Public) == 0 {
		return nil, fmt.Errorf("VC has no public endpoint")
	}
	server := detail.Properties.Endpoints.Public[0]
	if !strings.Contains(server, "://") {
		server = "https://" + server
	}
	u, err := url.Parse(server)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return nil, fmt.Errorf("invalid HTTPS VC endpoint")
	}
	var list struct {
		Credentials []struct {
			UID string `json:"uid"`
		} `json:"credentials"`
	}
	if err = c.getJSONWithProfile(ctx, p, base+"/credentials", &list); err != nil {
		return nil, fmt.Errorf("credential list unavailable")
	}
	for _, item := range list.Credentials {
		if item.UID == "" {
			continue
		}
		var cert struct {
			CA   string `json:"certificate_authority"`
			Cert string `json:"client_certificate"`
			Key  string `json:"client_key"`
		}
		if c.getJSONWithProfile(ctx, p, base+"/credentials/"+url.PathEscape(item.UID), &cert) != nil {
			continue
		}
		ca, e1 := base64.StdEncoding.DecodeString(cert.CA)
		crt, e2 := base64.StdEncoding.DecodeString(cert.Cert)
		key, e3 := base64.StdEncoding.DecodeString(cert.Key)
		if e1 != nil || e2 != nil || e3 != nil || len(ca) == 0 || len(crt) == 0 || len(key) == 0 {
			continue
		}
		pair, err := tls.X509KeyPair(crt, key)
		if err != nil {
			continue
		}
		leaf, err := x509.ParseCertificate(pair.Certificate[0])
		if err != nil || time.Now().Before(leaf.NotBefore) || time.Now().After(leaf.NotAfter) {
			continue
		}
		config := clientcmdapi.NewConfig()
		config.Clusters[ref.Name] = &clientcmdapi.Cluster{Server: server, CertificateAuthorityData: ca}
		config.AuthInfos[ref.Name] = &clientcmdapi.AuthInfo{ClientCertificateData: crt, ClientKeyData: key}
		config.Contexts[ref.Name] = &clientcmdapi.Context{Cluster: ref.Name, AuthInfo: ref.Name}
		config.CurrentContext = ref.Name
		return clientcmd.Write(*config)
	}
	return nil, fmt.Errorf("no complete certificate credential available")
}
