package output

import (
	"fmt"
	"rayctl/internal/service"
)

func printQueueIncrement(r *service.SSPQueueItem) {
	printDetailAnchor(r.Name, r.State, r.Namespace, "", r.Region)
	printResourceDetail([][]string{{"WORKSPACE UID", r.WorkspaceUID}, {"CLUSTER UID", r.ClusterUID}, {"NODE COUNT", fmt.Sprint(r.NodeCount)}, {"MACHINE TYPES", r.MachineTypes}, {"MAX NODES", r.MaxNodes}, {"CAPABILITY", r.Capability}})
}
