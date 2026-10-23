package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"rayctl/internal/platform"
)

const nodeSSPClusterUIDLabelKey = "resource.compute.sensecore.cn/cluster-uid"

type NodeUsagePlatform interface {
	ListSSPClustersForNodeUsage(context.Context, string) ([]platform.SSPCluster, error)
}

func nodeUsageFromLabels(labels map[string]string) string {
	role := nodeDisplayRole(labels)
	if strings.EqualFold(role, "ssp") {
		return "SSP"
	}
	for _, key := range []string{nodeQueueNameLabelKey, nodeQueueUIDLabelKey, sspClusterNameLabel, nodeSSPClusterUIDLabelKey} {
		if value := strings.TrimSpace(labels[key]); value != "" && value != "-" {
			return "SSP"
		}
	}
	return nodeUsageFromRole(role)
}

func nodeUsageFromRole(role string) string {
	role = strings.TrimSpace(role)
	lower := strings.ToLower(role)
	switch {
	case lower == "ecp" || strings.HasPrefix(lower, "ecp-"):
		return "ECP"
	case lower == "ecs" || strings.HasPrefix(lower, "ecs-"):
		return "ECS"
	case lower == "ssp":
		return "SSP"
	default:
		return role
	}
}

// EnrichNodeListUsage joins SSP clusters to their source VC. A source VC UID
// takes precedence over a name so a recreated VC cannot inherit stale usage.
func EnrichNodeListUsage(ctx context.Context, nodes []NodeListItem, client NodeUsagePlatform, region string) error {
	if client == nil || len(nodes) == 0 {
		return nil
	}
	clusters, err := client.ListSSPClustersForNodeUsage(ctx, region)
	if err != nil {
		return fmt.Errorf("list SSP clusters for node usage: %w", err)
	}
	ambiguousNames := nodeUsageAmbiguousSourceNames(clusters)
	for index := range nodes {
		names := splitNodeUsageValues(nodes[index].SSPClusterName)
		uids := splitNodeUsageValues(nodes[index].SSPClusterUID)
		for _, cluster := range clusters {
			if !nodeUsageClusterActive(cluster) {
				continue
			}
			allowName := !ambiguousNames[strings.ToLower(nodeUsageClusterSourceName(cluster))]
			if !nodeMatchesSSPClusterSourceWithName(nodes[index], cluster, allowName) {
				continue
			}
			nodes[index].Usage = "SSP"
			names = append(names, firstNonEmpty(strings.TrimSpace(cluster.Name), strings.TrimSpace(cluster.DisplayName)))
			uids = append(uids, strings.TrimSpace(cluster.UID))
		}
		nodes[index].SSPClusterName = joinNodeUsageValues(names)
		nodes[index].SSPClusterUID = joinNodeUsageValues(uids)
	}
	return nil
}

func nodeMatchesSSPClusterSource(node NodeListItem, cluster platform.SSPCluster) bool {
	return nodeMatchesSSPClusterSourceWithName(node, cluster, true)
}

func nodeMatchesSSPClusterSourceWithName(node NodeListItem, cluster platform.SSPCluster, allowName bool) bool {
	nodeUID := strings.TrimPrefix(strings.TrimSpace(node.ClusterUID), "vc-")
	sourceUID := nodeUsageClusterSourceUID(cluster)
	if nodeUID != "" && sourceUID != "" {
		if strings.EqualFold(nodeUID, sourceUID) {
			return true
		}
		// Legacy node labels also populate ClusterUID by stripping "vc-"
		// from a display name. Only a UUID is authoritative on mismatch.
		if looksLikeUUID(nodeUID) {
			return false
		}
	}
	if !allowName {
		return false
	}
	nodeName := strings.TrimSpace(node.ClusterName)
	sourceName := nodeUsageClusterSourceName(cluster)
	return nodeName != "" && sourceName != "" && strings.EqualFold(nodeName, sourceName)
}

func nodeUsageClusterSourceUID(cluster platform.SSPCluster) string {
	if uid := strings.TrimPrefix(strings.TrimSpace(cluster.Properties.Source.UID), "vc-"); uid != "" {
		return uid
	}
	uid := strings.TrimPrefix(lastResourceSegment(cluster.Properties.Source.ID), "vc-")
	if looksLikeUUID(uid) {
		return uid
	}
	return ""
}

func nodeUsageClusterSourceName(cluster platform.SSPCluster) string {
	return firstNonEmpty(strings.TrimSpace(cluster.Properties.Source.Name), lastResourceSegment(cluster.Properties.Source.ID))
}

func nodeUsageClusterActive(cluster platform.SSPCluster) bool {
	switch strings.ToUpper(strings.TrimSpace(cluster.State)) {
	case "DELETED", "DELETING":
		return false
	default:
		return true
	}
}

func nodeUsageAmbiguousSourceNames(clusters []platform.SSPCluster) map[string]bool {
	identities := make(map[string]string)
	ambiguous := make(map[string]bool)
	for _, cluster := range clusters {
		if !nodeUsageClusterActive(cluster) {
			continue
		}
		name := strings.ToLower(nodeUsageClusterSourceName(cluster))
		if name == "" {
			continue
		}
		identity := "uid:" + strings.ToLower(nodeUsageClusterSourceUID(cluster))
		if identity == "uid:" {
			identity = "profile:" + strings.ToLower(strings.TrimSpace(cluster.ProfileName))
		}
		if previous, ok := identities[name]; ok && previous != identity {
			ambiguous[name] = true
		}
		identities[name] = identity
	}
	return ambiguous
}

func splitNodeUsageValues(value string) []string {
	return strings.Split(value, ",")
}

func joinNodeUsageValues(values []string) string {
	unique := make(map[string]string, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || value == "-" {
			continue
		}
		key := strings.ToLower(value)
		if _, exists := unique[key]; !exists {
			unique[key] = value
		}
	}
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, unique[key])
	}
	return strings.Join(result, ", ")
}

func nodeListUsageTarget(target string) string {
	switch strings.ToUpper(strings.TrimSpace(target)) {
	case "ECP":
		return "ECP"
	case "ECS":
		return "ECS"
	case "SSP":
		return "SSP"
	default:
		return ""
	}
}

// FilterNodeListByUsage applies role aliases after platform usage enrichment.
// Raw Kubernetes selectors are already applied by NodeService.List.
func FilterNodeListByUsage(nodes []NodeListItem, target string) []NodeListItem {
	usage := nodeListUsageTarget(target)
	if usage == "" {
		return nodes
	}
	result := make([]NodeListItem, 0, len(nodes))
	for _, node := range nodes {
		if strings.EqualFold(strings.TrimSpace(node.Usage), usage) {
			result = append(result, node)
		}
	}
	return result
}
