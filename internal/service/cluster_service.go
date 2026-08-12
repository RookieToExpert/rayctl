package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"rayctl/internal/platform"
)

const (
	clusterVirtualNamespaceLabelKey = "vcluster.loft.sh/vcluster-namespace"
	clusterLogicalNamespaceLabelKey = "vcluster.loft.sh/custom-namespace-name"
	clusterLogicalNamespaceAnnotKey = "vcluster.loft.sh/object-name"
)

type ClusterService struct {
	clientset kubernetes.Interface
	vcClient  ClusterPlatform
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
	if vcClient == nil {
		return NewClusterServiceWithPlatform(clientset, nil)
	}
	return NewClusterServiceWithPlatform(clientset, vcClient)
}

func NewClusterServiceWithPlatform(clientset kubernetes.Interface, vcClient ClusterPlatform) *ClusterService {
	return &ClusterService{
		clientset: clientset,
		vcClient:  vcClient,
	}
}

func (s *ClusterService) ResolveDisplayNamesWithProfiles(ctx context.Context, uids []string) (map[string]string, map[string]string, error) {
	if s == nil || s.vcClient == nil {
		return map[string]string{}, map[string]string{}, nil
	}
	return s.vcClient.ResolveDisplayNamesWithProfiles(ctx, uids)
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
	return s.GetResolved(ctx, clusterName, clusterUID)
}

func (s *ClusterService) GetResolved(ctx context.Context, clusterName string, clusterUID string) (*ClusterGetResult, error) {
	clusterName = strings.TrimSpace(clusterName)
	clusterUID = strings.TrimPrefix(strings.TrimSpace(clusterUID), "vc-")
	if clusterUID == "" {
		return nil, fmt.Errorf("cluster uid is required")
	}
	controlPlaneNamespace := "vc-" + clusterUID
	controlPlaneCall := asyncCall(ctx, func(ctx context.Context) (*corev1.Namespace, error) {
		return s.clientset.CoreV1().Namespaces().Get(ctx, controlPlaneNamespace, metav1.GetOptions{})
	})
	resourceListCall := asyncCall(ctx, func(ctx context.Context) (*corev1.NamespaceList, error) {
		return s.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
			LabelSelector: clusterVirtualNamespaceLabelKey + "=" + controlPlaneNamespace,
		})
	})
	controlPlaneResult := <-controlPlaneCall
	resourceListResult := <-resourceListCall

	if controlPlaneResult.Err != nil && !apierrors.IsNotFound(controlPlaneResult.Err) {
		return nil, fmt.Errorf("get control plane namespace %q: %w", controlPlaneNamespace, controlPlaneResult.Err)
	}
	if resourceListResult.Err != nil {
		return nil, fmt.Errorf("list resource namespaces: %w", resourceListResult.Err)
	}

	resourceNamespaces := make([]ClusterNamespaceMapping, 0)
	for _, ns := range resourceListResult.Value.Items {
		logicalNamespace := firstNonEmpty(
			strings.TrimSpace(ns.Labels[clusterLogicalNamespaceLabelKey]),
			strings.TrimSpace(ns.Annotations[clusterLogicalNamespaceAnnotKey]),
			ns.Name,
		)
		resourceNamespaces = append(resourceNamespaces, ClusterNamespaceMapping{
			ResourceNamespace: ns.Name,
			VirtualNamespace:  logicalNamespace,
		})
	}

	if controlPlaneResult.Err != nil && len(resourceNamespaces) == 0 {
		return nil, fmt.Errorf("cluster %q 在当前 kubeconfig 下没有找到控制面 namespace 或资源 namespace，当前大概率不是 HC kubeconfig", firstNonEmpty(clusterName, "vc-"+clusterUID))
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

	exactCtx, cancelExact := context.WithTimeout(ctx, 2*time.Second)
	cluster, exactErr := s.vcClient.FindExactVirtualCluster(exactCtx, identifier)
	cancelExact()
	if exactErr == nil {
		return firstNonEmpty(cluster.Name, cluster.DisplayName, "vc-"+cluster.UID), cluster.UID, nil
	}

	if clusters, currentErr := s.vcClient.ListCurrentProfileVirtualClusters(ctx); currentErr == nil {
		clusterName, clusterUID, matched, matchErr := matchVirtualClusterIdentifier(identifier, clusters)
		if matchErr != nil {
			return "", "", matchErr
		}
		if matched {
			return clusterName, clusterUID, nil
		}
	}

	clusters, err := s.vcClient.ListVirtualClusters(ctx)
	if err != nil {
		return "", "", fmt.Errorf("list virtual clusters: %w", err)
	}
	clusterName, clusterUID, matched, matchErr := matchVirtualClusterIdentifier(identifier, clusters)
	if matchErr != nil {
		return "", "", matchErr
	}
	if matched {
		return clusterName, clusterUID, nil
	}
	return "", "", fmt.Errorf("virtual cluster %q not found", identifier)
}

func matchVirtualClusterIdentifier(identifier string, clusters []platform.VirtualCluster) (string, string, bool, error) {
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
		return firstNonEmpty(exact[0].Name, exact[0].DisplayName, "vc-"+exact[0].UID), exact[0].UID, true, nil
	case len(exact) > 1:
		return "", "", false, fmt.Errorf("cluster %q matched multiple virtual clusters: %s", identifier, joinVirtualClusterCandidates(exact))
	case len(fuzzy) == 1:
		return firstNonEmpty(fuzzy[0].Name, fuzzy[0].DisplayName, "vc-"+fuzzy[0].UID), fuzzy[0].UID, true, nil
	case len(fuzzy) > 1:
		return "", "", false, fmt.Errorf("cluster %q matched multiple virtual clusters: %s", identifier, joinVirtualClusterCandidates(fuzzy))
	default:
		return "", "", false, nil
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
