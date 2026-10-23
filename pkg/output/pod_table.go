package output

import (
	"fmt"
	"rayctl/internal/service"
)

func PrintPodLocateResult(r *service.PodLocateResult) {
	if captureJSON(r) || r == nil {
		return
	}
	rows := [][]string{
		{"ENVIRONMENT", r.Environment},
		{"HC NAMESPACE", r.HostNamespace}, {"HC POD", r.HostPod},
		{"VC NAMESPACE", r.VCNamespace}, {"VC POD", r.VCPod},
		{"TYPE", r.WorkloadType}, {"WORKLOAD", r.WorkloadName}, {"WORKLOAD UID", r.WorkloadUID},
		{"WORKSPACE", r.Workspace}, {"QUEUE", r.Queue}, {"CLUSTER", r.Cluster}, {"VC", r.VC},
		{"CREATOR", r.Creator}, {"TENANT", r.Tenant},
		{"NODE", r.Node}, {"NODE IP", r.NodeIP}, {"POD IP", r.PodIP},
		{"STATUS", r.Status}, {"PHASE", r.Phase}, {"READY", fmt.Sprint(r.Ready)}, {"RESTARTS", fmt.Sprint(r.Restarts)},
		{"DELETING FOR", r.DeletingFor},
	}
	for _, pvc := range r.PVCs {
		rows = append(rows, []string{"PVC " + pvc.Namespace + "/" + pvc.Name, emptyDash(pvc.AFS) + " " + pvc.Warning})
	}
	for _, suggestion := range r.Suggestions {
		rows = append(rows, []string{"NEXT", suggestion})
	}
	for _, warning := range r.Warnings {
		rows = append(rows, []string{"WARNING", warning})
	}
	for i := range rows {
		rows[i][1] = emptyDash(rows[i][1])
	}
	printBoxTableWithOptions([]string{"FIELD", "VALUE"}, rows, []int{36, 100}, tableOptions{})
}
