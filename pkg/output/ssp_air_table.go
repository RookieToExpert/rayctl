package output

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"rayctl/internal/service"
)

func PrintSSPAIRJobList(result *service.SSPAIRJobListResult, long ...bool) {
	if captureJSON(result) {
		return
	}
	if result != nil {
		defer func() {
			for _, warning := range result.Warnings {
				PrintWarning(os.Stderr, warning)
			}
		}()
	}
	rows := make([][]string, 0)
	wide := len(long) > 0 && long[0]
	if result != nil {
		for _, item := range result.Items {
			row := []string{emptyDash(item.Name), emptyDash(item.State), fmt.Sprintf("%d/%d ×%s", item.ReadyReplicas, item.Replicas, formatAIRGroupSize(item.GroupSize)), emptyDash(item.Workspace)}
			if wide {
				row = append(row, emptyDash(item.Queue), emptyDash(item.Creator), emptyDash(item.CreatedAt))
			}
			rows = append(rows, row)
		}
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-"})
	}
	headers := []string{"NAME", "STATE", "READY", "WORKSPACE"}
	widths := []int{48, 9, 12, 28}
	if wide {
		headers = append(headers, "QUEUE", "CREATOR", "CREATED")
		widths = append(widths, 48, 22, 20)
	}
	printBoxTableWithMaxWidths(headers, rows, widths)
}

func PrintSSPAIRJobDetail(result *service.SSPAIRJobItem, longOutput bool) {
	if captureJSON(result) {
		return
	}
	if longOutput && result != nil {
		printAIRIncrement(result)
		return
	}
	if result != nil && longOutput {
		defer func() { printPodEvidence(result.PodEvidence) }()
	}
	if result == nil {
		return
	}
	rows := [][]string{
		{"TYPE", "AIR JOB"}, {"NAME", emptyDash(result.Name)}, {"UID", emptyDash(result.UID)},
		{"STATE", emptyDash(result.State)}, {"WORKSPACE", emptyDash(result.Workspace)}, {"QUEUE", emptyDash(result.Queue)},
		{"QUEUE TYPE", emptyDash(result.QueueType)}, {"CLUSTER", emptyDash(result.Cluster)}, {"VC", emptyDash(result.VCluster)},
		{"VC NAMESPACE", emptyDash(result.Namespace)}, {"HOST NAMESPACE", emptyDash(result.HostNamespace)}, {"PRIORITY", emptyDash(result.Priority)},
		{"GROUPS READY/TOTAL", fmt.Sprintf("%d / %d", result.ReadyReplicas, result.Replicas)},
		{"DESIRED GROUPS", strconv.Itoa(result.DesiredReplicas)},
		{"INSTANCES/GROUP", formatAIRGroupSize(result.GroupSize)},
		{"TOTAL INSTANCES", formatAIRTotalInstances(*result)}, {"CREATOR", emptyDash(result.Creator)},
		{"LEADER SERVICE IP", formatAIRLeaderServiceIP(result.InternalIP)}, {"REGION", emptyDash(result.Region)}, {"PROFILE", emptyDash(result.Profile)},
		{"CREATED", emptyDash(result.CreatedAt)}, {"UPDATED", emptyDash(result.UpdatedAt)},
	}
	if longOutput {
		rows = append(rows,
			[]string{"RESOURCE", formatAIRResource(result.Resource)},
			[]string{"IMAGE", emptyDash(result.Resource.Image)},
		)
	}
	printBoxTableWithOptions([]string{"FIELD", "VALUE"}, rows, []int{22, 100}, tableOptions{minWidths: []int{22, 30}})
	if longOutput {
		printAIRDNATRules(result.DNATRules)
	}
	if longOutput && len(result.Volumes) > 0 {
		fmt.Fprintln(os.Stdout)
		volumeRows := make([][]string, 0, len(result.Volumes))
		for _, volume := range result.Volumes {
			volumeRows = append(volumeRows, []string{emptyDash(volume.Type), emptyDash(volume.Name), emptyDash(volume.MountPath), emptyDash(volume.Endpoint)})
		}
		printBoxTableWithMaxWidths([]string{"TYPE", "VOLUME", "MOUNT PATH", "ENDPOINT"}, volumeRows, []int{18, 34, 36, 44})
	}
	if result.WorkerTotal > 0 || len(result.Workers) > 0 {
		fmt.Fprintln(os.Stdout)
		fmt.Fprintf(os.Stdout, "INSTANCES: showing %d (configured total %s)\n", len(result.Workers), formatAIRTotalInstances(*result))
		printAIRWorkers(result.Workers, "INSTANCE", longOutput)
	}
	if len(result.AuxiliaryWorkers) > 0 {
		fmt.Fprintf(os.Stdout, "\nAUXILIARY PODS: showing %d\n", len(result.AuxiliaryWorkers))
		printAIRWorkers(result.AuxiliaryWorkers, "POD", longOutput)
	}
}

func printAIRWorkers(workers []service.SSPAIRWorkerItem, nameHeader string, longOutput bool) {
	headers := []string{nameHeader, "GROUP", "ROLE", "PHASE", "NODE", "HOST IP", "POD IP", "RESTARTS"}
	widths := []int{96, 7, 12, 14, 32, 18, 18, 10}
	minWidths := []int{28, 7, 9, 10, 20, 14, 14, 9}
	noWrapColumns := []int{0, 1, 2, 4, 5, 6}
	if longOutput {
		headers = append(headers, "STARTED", "LAST STARTED")
		widths = append(widths, 20, 20)
		minWidths = append(minWidths, 19, 19)
		noWrapColumns = append(noWrapColumns, 8, 9)
	}
	rows := make([][]string, 0, maxInt(1, len(workers)))
	for _, worker := range workers {
		row := []string{
			emptyDash(worker.Name), emptyDash(worker.Group), emptyDash(worker.Role), emptyDash(worker.Phase),
			emptyDash(worker.Node), emptyDash(worker.HostIP), emptyDash(worker.PodIP), strconv.Itoa(worker.Restarts),
		}
		for _, column := range []int{0, 4, 5, 6} {
			minWidths[column] = max(minWidths[column], min(widths[column], textWidth(row[column])))
		}
		if longOutput {
			row = append(row, emptyDash(worker.StartedAt), emptyDash(worker.LastStarted))
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		row := make([]string, len(headers))
		for index := range row {
			row[index] = "-"
		}
		rows = append(rows, row)
	}
	printBoxTableWithOptions(headers, rows, widths, tableOptions{
		noWrapCells: makeNoWrapCells(noWrapCellsForColumns(len(rows), noWrapColumns...)...),
		minWidths:   minWidths,
	})
}

func formatAIRGroupSize(groupSize int) string {
	if groupSize <= 0 {
		return "-"
	}
	return strconv.Itoa(groupSize)
}

func formatAIRTotalInstances(item service.SSPAIRJobItem) string {
	if item.GroupSize <= 0 && item.TotalInstances <= 0 {
		return "-"
	}
	return strconv.Itoa(item.TotalInstances)
}

func formatAIRLeaderServiceIP(ip string) string {
	if strings.TrimSpace(ip) == "" {
		return "- (not assigned)"
	}
	return ip
}

func PrintSSPAIRGatewayList(result *service.SSPAIRGatewayListResult) {
	if captureJSON(result) {
		return
	}
	rows := make([][]string, 0)
	if result != nil {
		for _, item := range result.Items {
			rows = append(rows, []string{
				emptyDash(item.Name), emptyDash(item.State), emptyDash(item.Workspace), emptyDash(item.Queue),
				strconv.Itoa(item.Replicas), emptyDash(item.Creator), emptyDash(item.CreatedAt),
			})
		}
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-", "-", "-", "-"})
	}
	printBoxTableWithOptions(
		[]string{"NAME", "STATE", "WORKSPACE", "QUEUE", "REPLICAS", "CREATOR", "CREATED"}, rows,
		[]int{48, 12, 38, 48, 10, 22, 20},
		tableOptions{noWrapCells: makeNoWrapCells(noWrapCellsForColumns(len(rows), 0, 2, 3, 5)...), minWidths: []int{28, 8, 22, 26, 9, 14, 19}},
	)
}

func PrintSSPAIRGatewayDetail(result *service.SSPAIRGatewayItem, longOutput bool) {
	if captureJSON(result) {
		return
	}
	if longOutput && result != nil {
		printGatewayIncrement(result)
		return
	}
	if result == nil {
		return
	}
	rows := [][]string{
		{"TYPE", "AIR GATEWAY"}, {"NAME", emptyDash(result.Name)}, {"UID", emptyDash(result.UID)},
		{"STATE", emptyDash(result.State)}, {"WORKSPACE", emptyDash(result.Workspace)}, {"QUEUE", emptyDash(result.Queue)},
		{"QUEUE TYPE", emptyDash(result.QueueType)}, {"CLUSTER", emptyDash(result.Cluster)},
		{"VC NAMESPACE", emptyDash(result.Namespace)}, {"HOST NAMESPACE", emptyDash(result.HostNamespace)}, {"PRIORITY", emptyDash(result.Priority)},
		{"REPLICAS", strconv.Itoa(result.Replicas)}, {"CREATOR", emptyDash(result.Creator)}, {"REGION", emptyDash(result.Region)},
		{"PROFILE", emptyDash(result.Profile)}, {"CREATED", emptyDash(result.CreatedAt)}, {"UPDATED", emptyDash(result.UpdatedAt)},
	}
	if longOutput {
		rows = append(rows, []string{"RESOURCE", formatAIRResource(result.Resource)})
	}
	printBoxTableWithOptions([]string{"FIELD", "VALUE"}, rows, []int{18, 100}, tableOptions{minWidths: []int{14, 30}})
	if longOutput {
		printAIRDNATRules(result.DNATRules)
	}
}

func printAIRDNATRules(rules []service.SSPAIRDNATItem) {
	if len(rules) == 0 {
		return
	}
	fmt.Fprintln(os.Stdout)
	rows := make([][]string, 0, len(rules))
	for _, rule := range rules {
		rows = append(rows, []string{emptyDash(rule.External), emptyDash(rule.Internal), emptyDash(rule.Protocol), emptyDash(rule.Gateway)})
	}
	printBoxTableWithMaxWidths([]string{"EXTERNAL", "INTERNAL", "PROTOCOL", "NAT GATEWAY"}, rows, []int{28, 28, 12, 34})
}

func formatAIRResource(resource service.SSPAIRResourceItem) string {
	parts := make([]string, 0, 5)
	for _, item := range []struct{ label, value string }{
		{"CPU", resource.CPU}, {"MEM", resource.Memory}, {"ACCEL", resource.Accelerator},
		{"MACHINE", resource.MachineType}, {"RDMA", resource.RDMA},
	} {
		if item.value != "" {
			parts = append(parts, item.label+"="+item.value)
		}
	}
	return emptyDash(strings.Join(parts, " "))
}
