package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"

	"rayctl/internal/platform"
)

const defaultRBACLabelSelector = "resource.compute.sensecore.cn/control"

type RBACService struct {
	vcClient *platform.VirtualClusterClient
}

type RBACGetResult struct {
	ClusterName   string
	ClusterUID    string
	ClusterRef    string
	ProfileName   string
	LabelSelector string
	Items         []RBACBindingItem
}

type RBACBindingItem struct {
	Name      string
	Role      string
	Subjects  string
	CreatedAt string
}

func NewRBACService(vcClient *platform.VirtualClusterClient) *RBACService {
	return &RBACService{vcClient: vcClient}
}

func (s *RBACService) Get(ctx context.Context, clusterIdentifier string, labelSelector string, bearerToken string) (*RBACGetResult, error) {
	if s == nil || s.vcClient == nil {
		return nil, fmt.Errorf("platform client is required for rbac lookup")
	}
	clusterIdentifier = strings.TrimSpace(clusterIdentifier)
	if clusterIdentifier == "" {
		return nil, fmt.Errorf("cluster identifier is required")
	}
	labelSelector = strings.TrimSpace(labelSelector)
	if labelSelector == "" {
		labelSelector = defaultRBACLabelSelector
	}
	if strings.TrimSpace(bearerToken) == "" {
		return nil, fmt.Errorf("rbac get requires console bearer token, please set RAYCTL_BEARER_TOKEN or BEARER_TOKEN")
	}

	clusterName, clusterUID, profileName, err := s.resolveCluster(ctx, clusterIdentifier)
	if err != nil {
		return nil, err
	}
	clusterRef := "vc-" + clusterUID

	bindings, err := s.vcClient.ListClusterRoleBindingsForProfileToken(ctx, profileName, clusterRef, labelSelector, bearerToken)
	if err != nil {
		return nil, fmt.Errorf("list clusterrolebindings: %w", err)
	}

	items := make([]RBACBindingItem, 0, len(bindings))
	for _, binding := range bindings {
		items = append(items, RBACBindingItem{
			Name:      binding.Name,
			Role:      roleRefText(binding.RoleRef),
			Subjects:  subjectsText(binding.Subjects),
			CreatedAt: formatRBACLocalTime(binding),
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Role != items[j].Role {
			return items[i].Role < items[j].Role
		}
		return items[i].Name < items[j].Name
	})

	return &RBACGetResult{
		ClusterName:   clusterName,
		ClusterUID:    clusterUID,
		ClusterRef:    clusterRef,
		ProfileName:   profileName,
		LabelSelector: labelSelector,
		Items:         items,
	}, nil
}

func (s *RBACService) resolveCluster(ctx context.Context, identifier string) (string, string, string, error) {
	clusters, err := s.vcClient.ListVirtualClusters(ctx)
	if err != nil {
		return "", "", "", fmt.Errorf("list virtual clusters: %w", err)
	}

	normalized := strings.ToLower(strings.TrimSpace(identifier))
	exact := make([]platform.VirtualCluster, 0)
	fuzzy := make([]platform.VirtualCluster, 0)
	for _, cluster := range clusters {
		fields := []string{
			strings.TrimSpace(cluster.Name),
			strings.TrimSpace(cluster.DisplayName),
			strings.TrimSpace(cluster.UID),
			"vc-" + strings.TrimSpace(cluster.UID),
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
		return firstNonEmpty(exact[0].Name, exact[0].DisplayName, "vc-"+exact[0].UID), exact[0].UID, exact[0].ProfileName, nil
	case len(exact) > 1:
		return "", "", "", fmt.Errorf("cluster %q matched multiple virtual clusters: %s", identifier, rbacClusterCandidates(exact))
	case len(fuzzy) == 1:
		return firstNonEmpty(fuzzy[0].Name, fuzzy[0].DisplayName, "vc-"+fuzzy[0].UID), fuzzy[0].UID, fuzzy[0].ProfileName, nil
	case len(fuzzy) > 1:
		return "", "", "", fmt.Errorf("cluster %q matched multiple virtual clusters: %s", identifier, rbacClusterCandidates(fuzzy))
	default:
		if strings.HasPrefix(identifier, "vc-") {
			uid := strings.TrimPrefix(identifier, "vc-")
			if uid != "" {
				return identifier, uid, "", nil
			}
		}
		return "", "", "", fmt.Errorf("virtual cluster %q not found", identifier)
	}
}

func rbacClusterCandidates(items []platform.VirtualCluster) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, firstNonEmpty(item.Name, item.DisplayName, "vc-"+item.UID))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func roleRefText(role rbacv1.RoleRef) string {
	return strings.TrimSpace(role.Kind + "/" + role.Name)
}

func formatRBACLocalTime(binding rbacv1.ClusterRoleBinding) string {
	if binding.CreationTimestamp.IsZero() {
		return ""
	}
	return formatLocalTime(binding.CreationTimestamp.Time.Format("2006-01-02T15:04:05.999999999Z07:00"))
}

func subjectsText(subjects []rbacv1.Subject) string {
	if len(subjects) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(subjects))
	for _, subject := range subjects {
		value := strings.TrimSpace(subject.Kind + "/" + subject.Name)
		if strings.TrimSpace(subject.Namespace) != "" {
			value += "@" + strings.TrimSpace(subject.Namespace)
		}
		parts = append(parts, value)
	}
	sort.Strings(parts)
	return strings.Join(parts, "\n")
}
