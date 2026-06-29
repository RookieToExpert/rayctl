package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"rayctl/internal/platform"
)

const (
	clusterNamespaceUIDLabelKey     = "resource.compute.sensecore.cn/vc-uid"
	clusterNamespaceNameLabelKey    = "cluster.x-k8s.io/cluster-name"
	clusterVirtualNamespaceLabelKey = "vcluster.loft.sh/vcluster-namespace"
	clusterLogicalNamespaceLabelKey = "vcluster.loft.sh/custom-namespace-name"
	clusterLogicalNamespaceAnnotKey = "vcluster.loft.sh/object-name"
)

type ClusterService struct {
	clientset kubernetes.Interface
	vcClient  *platform.VirtualClusterClient
}

type ClusterNamespaceMapping struct {
	ResourceNamespace string
	VirtualNamespace  string
}

type ClusterGetResult struct {
	ClusterName            string
	ClusterUID             string
	ControlPlaneNamespace  string
	ResourceNamespaceCount int
	ResourceNamespaces     []ClusterNamespaceMapping
}

func NewClusterService(clientset kubernetes.Interface, vcClient *platform.VirtualClusterClient) *ClusterService {
	return &ClusterService{
		clientset: clientset,
		vcClient:  vcClient,
	}
}

func (s *ClusterService) Get(ctx context.Context, identifier string) (*ClusterGetResult, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, fmt.Errorf("cluster identifier is required")
	}

	clusterName, clusterUID, err := s.resolveClusterIdentifier(ctx, identifier)
	if err != nil {
		return nil, err
	}

	controlPlaneNamespace := "vc-" + clusterUID
	namespaces, err := s.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}

	controlPlaneFound := false
	resourceNamespaces := make([]ClusterNamespaceMapping, 0)
	seenMappings := make(map[string]struct{})
	for _, ns := range namespaces.Items {
		switch {
		case ns.Name == controlPlaneNamespace:
			controlPlaneFound = true
		case strings.TrimSpace(ns.Labels[clusterNamespaceUIDLabelKey]) == clusterUID:
			controlPlaneFound = true
			if controlPlaneNamespace == "" {
				controlPlaneNamespace = ns.Name
			}
		case strings.TrimSpace(ns.Labels[clusterNamespaceNameLabelKey]) == controlPlaneNamespace:
			controlPlaneFound = true
		}

		if strings.TrimSpace(ns.Labels[clusterVirtualNamespaceLabelKey]) != controlPlaneNamespace {
			continue
		}

		logicalNamespace := firstNonEmpty(
			strings.TrimSpace(ns.Labels[clusterLogicalNamespaceLabelKey]),
			strings.TrimSpace(ns.Annotations[clusterLogicalNamespaceAnnotKey]),
			ns.Name,
		)
		key := ns.Name + "|" + logicalNamespace
		if _, ok := seenMappings[key]; ok {
			continue
		}
		seenMappings[key] = struct{}{}
		resourceNamespaces = append(resourceNamespaces, ClusterNamespaceMapping{
			ResourceNamespace: ns.Name,
			VirtualNamespace:  logicalNamespace,
		})
	}

	if !controlPlaneFound && len(resourceNamespaces) == 0 {
		return nil, fmt.Errorf("cluster %q 在当前 kubeconfig 下没有找到控制面 namespace 或资源 namespace，当前大概率不是 HC kubeconfig", identifier)
	}

	sort.Slice(resourceNamespaces, func(i, j int) bool {
		if resourceNamespaces[i].VirtualNamespace == resourceNamespaces[j].VirtualNamespace {
			return resourceNamespaces[i].ResourceNamespace < resourceNamespaces[j].ResourceNamespace
		}
		if resourceNamespaces[i].VirtualNamespace == "default" {
			return true
		}
		if resourceNamespaces[j].VirtualNamespace == "default" {
			return false
		}
		if resourceNamespaces[i].VirtualNamespace == "kube-system" {
			return true
		}
		if resourceNamespaces[j].VirtualNamespace == "kube-system" {
			return false
		}
		return resourceNamespaces[i].VirtualNamespace < resourceNamespaces[j].VirtualNamespace
	})

	return &ClusterGetResult{
		ClusterName:            firstNonEmpty(clusterName, "vc-"+clusterUID),
		ClusterUID:             clusterUID,
		ControlPlaneNamespace:  controlPlaneNamespace,
		ResourceNamespaceCount: len(resourceNamespaces),
		ResourceNamespaces:     resourceNamespaces,
	}, nil
}

func (s *ClusterService) resolveClusterIdentifier(ctx context.Context, identifier string) (string, string, error) {
	if s.vcClient == nil {
		if strings.HasPrefix(identifier, "vc-") {
			trimmed := strings.TrimPrefix(identifier, "vc-")
			if trimmed != "" {
				return identifier, trimmed, nil
			}
		}
		return "", "", fmt.Errorf("cluster get requires platform configuration to resolve %q", identifier)
	}

	clusters, err := s.vcClient.ListVirtualClusters(ctx)
	if err != nil {
		return "", "", fmt.Errorf("list virtual clusters: %w", err)
	}

	normalized := strings.ToLower(strings.TrimSpace(identifier))
	var exact []platform.VirtualCluster
	var fuzzy []platform.VirtualCluster
	for _, cluster := range clusters {
		fields := []string{
			strings.TrimSpace(cluster.Name),
			strings.TrimSpace(cluster.DisplayName),
			"vc-" + strings.TrimSpace(cluster.UID),
			strings.TrimSpace(cluster.UID),
		}
		matchedExact := false
		matchedFuzzy := false
		for _, field := range fields {
			field = strings.ToLower(strings.TrimSpace(field))
			if field == "" {
				continue
			}
			if field == normalized {
				matchedExact = true
				break
			}
			if strings.Contains(field, normalized) {
				matchedFuzzy = true
			}
		}
		if matchedExact {
			exact = append(exact, cluster)
			continue
		}
		if matchedFuzzy {
			fuzzy = append(fuzzy, cluster)
		}
	}

	switch {
	case len(exact) == 1:
		return firstNonEmpty(exact[0].Name, exact[0].DisplayName, "vc-"+exact[0].UID), exact[0].UID, nil
	case len(exact) > 1:
		return "", "", fmt.Errorf("cluster %q matched multiple virtual clusters: %s", identifier, joinVirtualClusterCandidates(exact))
	case len(fuzzy) == 1:
		return firstNonEmpty(fuzzy[0].Name, fuzzy[0].DisplayName, "vc-"+fuzzy[0].UID), fuzzy[0].UID, nil
	case len(fuzzy) > 1:
		return "", "", fmt.Errorf("cluster %q matched multiple virtual clusters: %s", identifier, joinVirtualClusterCandidates(fuzzy))
	default:
		return "", "", fmt.Errorf("virtual cluster %q not found", identifier)
	}
}

func joinVirtualClusterCandidates(items []platform.VirtualCluster) string {
	if len(items) == 0 {
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, firstNonEmpty(item.Name, item.DisplayName, "vc-"+item.UID))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}
