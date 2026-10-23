package service

import (
	"context"
	"fmt"

	"rayctl/internal/platform"
)

// HC namespaces describe Kubernetes placement, not the platform workspaces
// visible to the configured profiles. Use the same RMH inventory as AIR so
// unrelated HC namespace labels do not become tenant-scoped API requests.
func (s *SSPAIDService) resolveAIDWorkspaceCandidates(ctx context.Context, requested, region string) ([]sspWorkspaceRef, error) {
	workspaces, err := s.listAIDWorkspaceInventory(ctx, region)
	result := platformWorkspaceCandidates(workspaces, requested)
	if err != nil {
		return result, fmt.Errorf("list AID workspace inventory: %w", err)
	}
	return result, nil
}

func (s *SSPAIDService) listAIDWorkspaceInventory(ctx context.Context, region string) ([]platform.SSPWorkspace, error) {
	s.workspaceMu.Lock()
	if s.workspaceLoads == nil {
		s.workspaceLoads = map[string]*sspWorkspaceLoad{}
	}
	if load, ok := s.workspaceLoads[region]; ok {
		s.workspaceMu.Unlock()
		select {
		case <-load.done:
			return load.workspaces, load.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	load := &sspWorkspaceLoad{done: make(chan struct{})}
	s.workspaceLoads[region] = load
	s.workspaceMu.Unlock()
	load.workspaces, load.err = s.platform.ListSSPWorkspacesWithStatus(ctx, region)
	close(load.done)
	return load.workspaces, load.err
}
