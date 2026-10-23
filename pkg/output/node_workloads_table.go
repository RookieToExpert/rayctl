package output

import (
	"fmt"
	"rayctl/internal/service"
	"sort"
	"strings"
	"time"
)

func printNodeWorkloads(items []service.NodeWorkload, long bool) {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		row := []string{item.Namespace + "/" + item.Pod, item.Type, emptyDash(item.Workload), emptyDash(item.Status), fmt.Sprint(item.Restarts), (time.Duration(item.RuntimeSeconds) * time.Second).String()}
		if long {
			accelerators := make([]string, 0, len(item.AcceleratorRequests))
			for key, value := range item.AcceleratorRequests {
				accelerators = append(accelerators, key+"="+value)
			}
			sort.Strings(accelerators)
			row = append(row, emptyDash(item.VC), emptyDash(item.Workspace), emptyDash(item.Creator), emptyDash(strings.Join(accelerators, ", ")), emptyDash(item.Source))
		}
		rows = append(rows, row)
	}
	headers := []string{"POD", "TYPE", "WORKLOAD", "STATUS", "RESTARTS", "RUNTIME"}
	widths := []int{42, 6, 30, 20, 8, 14}
	if long {
		headers = append(headers, "VC", "WORKSPACE", "CREATOR", "ACCEL REQ", "SOURCE")
		widths = append(widths, 24, 24, 20, 28, 8)
	}
	printBoxTableWithMaxWidths(headers, rows, widths)
}
