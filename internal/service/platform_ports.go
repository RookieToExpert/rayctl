package service

import (
	"context"

	"rayctl/internal/platform"
)

// ClusterPlatform contains only the platform operations needed by ClusterService.
// Keeping this boundary narrow makes resolution behavior independently testable.
type ClusterPlatform interface {
	FindExactVirtualCluster(ctx context.Context, identifier string) (*platform.VirtualCluster, error)
	ListCurrentProfileVirtualClusters(ctx context.Context) ([]platform.VirtualCluster, error)
	ListVirtualClusters(ctx context.Context) ([]platform.VirtualCluster, error)
	ResolveDisplayNamesWithProfiles(ctx context.Context, uids []string) (map[string]string, map[string]string, error)
}

// ECSPlatform contains only the ECS/AIS discovery operations used by ECSService.
type ECSPlatform interface {
	FindCurrentProfileECSVirtualMachines(ctx context.Context, identifier string) ([]platform.ECSVirtualMachine, error)
	FindCurrentProfileAISpaces(ctx context.Context, identifier string) ([]platform.AISpace, error)
	ListECSVirtualMachines(ctx context.Context) ([]platform.ECSVirtualMachine, error)
	ListAISpaces(ctx context.Context) ([]platform.AISpace, error)
	ResolveUsernames(ctx context.Context, ids []string) (map[string]string, error)
}
