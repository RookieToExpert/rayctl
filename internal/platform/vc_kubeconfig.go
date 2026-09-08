package platform

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
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
	if environment == "" || environment == "auto" {
		environment = c.ProfileEnvironment(c.CurrentProfileName())
	}
	refs := []VCKubeconfigRef{}
	failures := []error{}
	seen := map[string]bool{}
	for _, p := range c.orderedProfiles() {
		// D/PT share tenants; resources are selected by their own region below.
		if environment != "all" && (profileEnvironment(p) == "dcloud") != (environment == "dcloud") {
			continue
		}
		items, err := c.listVirtualClustersForProfile(ctx, p)
		if err != nil {
			failures = append(failures, fmt.Errorf("profile %s inventory unavailable", p.Name))
			continue
		}
		for _, vc := range items {
			if strings.EqualFold(vc.State, "DELETED") {
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
			ref := VCKubeconfigRef{Name: vc.Name, UID: vc.UID, Profile: p.Name, Subscription: firstNonEmpty(ridSegment(vc.ID, "subscriptions"), vc.TenantID), ResourceGroup: firstNonEmpty(ridSegment(vc.ID, "resourceGroups"), p.ResourceGroup, "default"), Region: vc.Region, Environment: env}
			key := env + "|" + ref.Subscription + "|" + ref.UID
			if ref.UID == "" || ref.Name == "" || ref.Subscription == "" || ref.Region == "" {
				failures = append(failures, fmt.Errorf("profile %s: incomplete VC identity for %s", p.Name, vc.Name))
				continue
			}
			if !seen[key] {
				refs = append(refs, ref)
				seen[key] = true
			}
		}
	}
	return refs, failures
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
