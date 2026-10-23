package output

import (
	"fmt"
	"os"
	"strconv"

	"rayctl/internal/service"
)

func PrintVCConfig(result *service.VCControlPlaneConfig) {
	if captureJSON(result) || result == nil {
		return
	}
	rows := [][]string{{"VC", result.Name}, {"UID", result.UID}, {"ENVIRONMENT", result.Environment}, {"HOST NAMESPACE", result.HostNamespace}}
	rows = append(rows, vcConfigRows(result)...)
	printBoxTableWithOptions([]string{"FIELD", "VALUE"}, rows, []int{24, 72}, tableOptions{noWrapCells: makeNoWrapCells(noWrapCellsForSingleColumn(len(rows), 1)...)})
	printVCConfigWarnings(result)
}

func vcConfigRows(result *service.VCControlPlaneConfig) [][]string {
	if result == nil {
		return nil
	}
	desired := "未知"
	if result.Desired != nil {
		desired = result.Desired.Name
	}
	rows := [][]string{{"FLAVOR (HC)", emptyDash(result.AppliedFlavor)}, {"DESIRED FLAVOR", desired}, {"FLAVOR SOURCE", emptyDash(result.FlavorSource)}}
	if result.Desired != nil {
		rows = append(rows, []string{"MAX NODES", strconv.Itoa(int(result.Desired.MaxNodeCount))})
	}
	if result.Deployment != nil {
		d := result.Deployment
		rows = append(rows,
			[]string{"CONTROL PLANE READY", fmt.Sprintf("%d/%d (updated %d)", d.Ready, d.Replicas, d.Updated)},
			[]string{"CPU / REPLICA", resourcePair(d.Requests, d.Limits, "cpu")},
			[]string{"MEMORY / REPLICA", resourcePair(d.Requests, d.Limits, "memory")},
		)
	}
	status := "未收敛或信息不完整"
	if result.Converged {
		status = "已收敛"
	}
	return append(rows, []string{"CONFIG STATUS", status})
}

func resourcePair(requests, limits map[string]string, key string) string {
	limit := limits[key]
	if limit == "" {
		limit = "无限制"
	}
	return fmt.Sprintf("%s / %s (request / limit)", emptyDash(requests[key]), limit)
}

func printVCConfigWarnings(result *service.VCControlPlaneConfig) {
	if result != nil {
		for _, warning := range result.Warnings {
			fmt.Fprintln(os.Stdout, "Warning:", redactText(warning))
		}
	}
}

func PrintVCConfigUpdate(result *service.VCConfigUpdateResult) {
	if captureJSON(result) || result == nil || result.Current == nil {
		return
	}
	current := "未知"
	if result.Current.Desired != nil {
		current = result.Current.Desired.Name
	}
	rows := [][]string{
		{"VC", result.Current.Name}, {"UID", result.Current.UID}, {"ENVIRONMENT", result.Current.Environment},
		{"FLAVOR", current + " → " + result.Target.Name}, {"TARGET REPLICAS", strconv.Itoa(int(result.Target.Replicas))},
		{"TARGET CPU / REPLICA", resourcePair(result.Target.ResourceRequests, result.Target.ResourceLimits, "cpu")},
		{"TARGET MEMORY / REPLICA", resourcePair(result.Target.ResourceRequests, result.Target.ResourceLimits, "memory")},
		{"TARGET MAX NODES", strconv.Itoa(int(result.Target.MaxNodeCount))}, {"RESULT", result.Status},
	}
	if result.Observed != nil {
		rows = append(rows, vcConfigRows(result.Observed)...)
	}
	printBoxTableWithOptions([]string{"FIELD", "VALUE"}, rows, []int{28, 72}, tableOptions{noWrapCells: makeNoWrapCells(noWrapCellsForSingleColumn(len(rows), 1)...)})
	printVCConfigWarnings(result.Observed)
}
