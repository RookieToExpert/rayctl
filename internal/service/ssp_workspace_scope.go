package service

import (
	"strings"

	"rayctl/internal/platform"
)

// A platform workspace is identified by its subscription/profile as well as
// its name. HC namespace labels never add scopes to this inventory.
func platformWorkspaceCandidates(workspaces []platform.SSPWorkspace, requested string) []sspWorkspaceRef {
	requested = strings.TrimSpace(requested)
	result := make([]sspWorkspaceRef, 0, len(workspaces))
	for _, workspace := range workspaces {
		if requested != "" && !strings.EqualFold(workspace.Name, requested) {
			continue
		}
		result = append(result, sspWorkspaceRef{
			Name: workspace.Name, Subscription: workspace.Subscription, ProfileName: workspace.ProfileName,
		})
	}
	if requested != "" && len(result) == 0 {
		result = append(result, sspWorkspaceRef{Name: requested})
	}
	return result
}
