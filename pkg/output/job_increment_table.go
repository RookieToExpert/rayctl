package output

import (
	"fmt"
	"os"
	"rayctl/internal/podevidence"
	"rayctl/internal/service"
)

func printDetailAnchor(name, status, namespace, node, environment string) {
	fmt.Fprintf(os.Stdout, "%s | status=%s | host namespace=%s | nodes=%s", emptyDash(name), emptyDash(status), emptyDash(namespace), emptyDash(node))
	if environment != "" {
		fmt.Fprintf(os.Stdout, " | environment=%s", environment)
	}
	fmt.Fprintln(os.Stdout)
}

func printATEIncrement(r *service.JobGetResult, debug bool) {
	printDetailAnchor(r.Name, r.Status, r.HostNamespace, joinOrDash(r.Nodes), "")
	printResourceDetail([][]string{{"UID", emptyDash(r.UID)}})
	for _, pod := range r.Pods {
		if pod.Message != "" {
			covered := false
			if r.PodEvidence != nil {
				for _, evidence := range r.PodEvidence.Pods {
					if evidence.Name == pod.Name && evidence.Message == pod.Message {
						covered = true
					}
				}
			}
			if !covered {
				fmt.Fprintf(os.Stdout, "Pod %s: %s\n", pod.Name, redactText(pod.Message))
			}
		}
	}
	printPodEvidence(r.PodEvidence, podEvidenceView{PVCs: r.PersistentVolumeClaims})
	if r.PodEvidence == nil {
		printIncrementPVCs(r.PersistentVolumeClaims, nil)
		printIncrementLogs(r.RecentLogLines)
	}
	if debug {
		printBoxTable([]string{"STEP", "DURATION"}, [][]string{{"locate", r.Timings.Locate.String()}, {"platform job", r.Timings.PlatformJob.String()}, {"platform pods", r.Timings.PlatformPods.String()}, {"platform events", r.Timings.PlatformEvents.String()}, {"platform logs", r.Timings.PlatformLogs.String()}, {"diagnosis", r.Timings.Diagnosis.String()}, {"enrichment", r.Timings.Enrichment.String()}, {"kube job", r.Timings.KubeJob.String()}, {"kube pods", r.Timings.KubePods.String()}, {"kube events", r.Timings.KubeEvents.String()}, {"kube logs", r.Timings.KubeLogs.String()}, {"total", r.Timings.Total.String()}})
	}
}

func printAITIncrement(r *service.SSPJobGetResult) {
	printDetailAnchor(r.Name, r.Status, r.HostNamespace, joinOrDash(r.Nodes), "")
	printResourceDetail([][]string{{"UID", emptyDash(r.UID)}, {"IMAGE PULL SECRET", joinOrDash(r.ImagePullSecrets)}})
	if len(r.PodResources) > 0 {
		rows := [][]string{}
		for _, pod := range r.PodResources {
			rows = append(rows, []string{pod.Pod, pod.CPU, pod.Memory, pod.MachineType, pod.Model, pod.Accelerator})
		}
		printBoxTable([]string{"POD", "CPU", "MEMORY", "MACHINE TYPE", "MODEL", "ACCELERATOR"}, rows)
	}
	printPodEvidence(r.PodEvidence, podEvidenceView{PVCs: r.PersistentVolumeClaims})
	if r.PodEvidence == nil {
		printIncrementPVCs(r.PersistentVolumeClaims, nil)
		printIncrementLogs(r.RecentLogLines)
	}
}

func printAIDIncrement(r *service.SSPAIDGetResult, debug bool) {
	printDetailAnchor(r.Name, r.State, r.HostNamespace, joinOrDash(r.Nodes), r.Environment)
	printResourceDetail([][]string{{"UID", emptyDash(r.UID)}, {"IMAGE PULL SECRET", joinOrDash(r.ImagePullSecrets)}})
	printPodEvidence(r.PodEvidence, podEvidenceView{PVCs: r.PersistentVolumeClaims, BaselineImage: r.Image, IncludeHistory: true})
	if r.PodEvidence == nil {
		printIncrementPVCs(r.PersistentVolumeClaims, nil)
		printIncrementLogs(r.RecentLogLines)
	}
	if debug {
		printBoxTable([]string{"STEP", "DURATION"}, [][]string{{"locate pods", r.Timings.LocatePods.String()}, {"resolve region", r.Timings.ResolveRegion.String()}, {"workspace", r.Timings.Workspace.String()}, {"subscription", r.Timings.Subscription.String()}, {"platform aid", r.Timings.PlatformAID.String()}, {"refine pods", r.Timings.RefinePods.String()}, {"build detail", r.Timings.BuildDetail.String()}, {"dnat", r.Timings.DNAT.String()}, {"total", r.Timings.Total.String()}})
	}
}

func printIncrementLogs(lines []string) {
	if len(lines) == 0 {
		return
	}
	rows := [][]string{}
	for _, line := range lines {
		if line != "" {
			rows = append(rows, []string{redactText(line)})
		}
	}
	if len(rows) > 0 {
		printBoxTableWithMaxWidths([]string{"LATEST LOGS"}, rows, []int{110})
	}
}

func printIncrementPVCs(pvcs []service.VolumeClaimRef, seen map[string]bool) {
	rows := [][]string{}
	for _, pvc := range pvcs {
		name := firstNonEmptyOutput(pvc.ClaimName, pvc.Name)
		if seen[name] {
			continue
		}
		rows = append(rows, []string{name, pvc.MountPath, pvc.VolumeType, pvc.Status, pvc.FrontendVolume})
	}
	if len(rows) > 0 {
		printBoxTable([]string{"PVC", "MOUNT PATH", "TYPE", "STATUS", "AFS/AOSS ENDPOINT"}, rows)
	}
}

type podEvidenceView struct {
	PVCs           []service.VolumeClaimRef
	BaselineImage  string
	IncludeHistory bool
}

func injectedMount(mount podevidence.Mount) bool {
	return mount.Path == "/etc/hosts" || mount.Name == "vcluster-rewrite-hosts" || len(mount.Name) >= 16 && mount.Name[:16] == "kube-api-access-" || mount.Path == "/var/run/secrets/kubernetes.io/serviceaccount"
}
