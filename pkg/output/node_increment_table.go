package output

import (
	"rayctl/internal/service"
	"sort"
	"strings"
)

func printNodeIncrement(r *service.NodeDescribe) {
	printDetailAnchor(r.Hostname, r.Ready, "", r.Hostname, "")
	if r.Workloads == nil {
		return
	}
	printResourceDetail([][]string{{"VC", r.VClusterName}})
	rows := [][]string{}
	for _, item := range r.Workloads {
		accelerators := []string{}
		for key, value := range item.AcceleratorRequests {
			accelerators = append(accelerators, key+"="+value)
		}
		sort.Strings(accelerators)
		rows = append(rows, []string{item.Namespace + "/" + item.Pod, emptyDash(item.VC), emptyDash(item.Workspace), emptyDash(item.Creator), emptyDash(strings.Join(accelerators, ", ")), emptyDash(item.Source)})
	}
	if len(rows) > 0 {
		printBoxTable([]string{"POD", "VC", "WORKSPACE", "CREATOR", "ACCEL REQ", "SOURCE"}, rows)
	}
}
