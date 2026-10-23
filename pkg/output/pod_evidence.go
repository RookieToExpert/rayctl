package output

import (
	"fmt"
	"os"
	"strings"
	"time"

	"rayctl/internal/podevidence"
)

func printPodEvidence(result *podevidence.Result, views ...podEvidenceView) {
	if result == nil {
		return
	}
	view := podEvidenceView{IncludeHistory: true}
	if len(views) > 0 {
		view = views[0]
	}
	pvcInfo := map[string]int{}
	for i, pvc := range view.PVCs {
		for _, name := range []string{pvc.ClaimName, pvc.Name, pvc.HostPVCName} {
			if name != "" {
				pvcInfo[name] = i
			}
		}
	}
	seenPVCs := map[string]bool{}
	fmt.Fprintf(os.Stdout, "\nPOD EVIDENCE: %d/%d (omitted %d), current Pod cumulative restarts %d\n", len(result.Pods), result.Total, result.Omitted, result.Restarts)
	if view.IncludeHistory && result.Restarts24h != nil {
		fmt.Fprintf(os.Stdout, "Observed restarts in 24h (%d inspected Pods): %d\n", len(result.HistoryPods), *result.Restarts24h)
	}
	if last := result.LastRestart24h; view.IncludeHistory && last != nil {
		fmt.Fprintf(os.Stdout, "Last observed restart in 24h: %s (UTC+8), %s/%s, reason %s, Pod UID %s\n", last.ObservedAt.In(time.FixedZone("UTC+8", 8*3600)).Format("2006-01-02 15:04:05"), last.Pod, last.Container, emptyDash(last.Reason), emptyDash(last.UID))
	}
	for _, pod := range result.Pods {
		rows := [][]string{}
		images := [][]string{}
		for _, container := range pod.Containers {
			last, exit, instant := "-", "-", "-"
			if t := container.LastState; t != nil {
				last = t.Reason
				if t.FinishedAt != nil {
					last += " " + t.FinishedAt.In(time.FixedZone("UTC+8", 8*3600)).Format("2006-01-02 15:04:05")
				}
				exit = fmt.Sprint(t.ExitCode)
				if t.InstantExit != nil {
					instant = fmt.Sprint(*t.InstantExit)
				}
			}
			rows = append(rows, []string{container.Name, container.Kind, fmt.Sprint(container.Ready), fmt.Sprint(container.RestartCount), containerStateLabel(container.State), last, exit, instant})
			imageArch, nodeArch := "-", "-"
			if container.Image.Architecture != nil {
				imageArch = *container.Image.Architecture
			}
			if pod.NodeArchitecture != nil {
				nodeArch = *pod.NodeArchitecture
			}
			images = append(images, []string{container.Name, container.Image.Ref, imageArch, pod.Node, nodeArch})
		}
		fmt.Fprintf(os.Stdout, "\nPod %s/%s (%s), phase=%s\n", pod.Namespace, pod.Name, pod.UID, emptyDash(pod.Phase))
		if pod.Reason != "" {
			fmt.Fprintf(os.Stdout, "status.reason: %s\n", pod.Reason)
		}
		if pod.Message != "" {
			fmt.Fprintf(os.Stdout, "status.message: %s\n", redactText(pod.Message))
		}
		if len(rows) > 0 {
			printBoxTable([]string{"CONTAINER", "KIND", "READY", "RESTARTS", "STATE", "LAST TERMINATION", "EXIT", "INSTANT EXIT"}, rows)
		}
		imageHeaders := []string{"CONTAINER", "IMAGE", "IMAGE ARCH", "NODE", "NODE ARCH"}
		sameImage := view.BaselineImage != "" && len(images) > 0
		for _, row := range images {
			sameImage = sameImage && row[1] == view.BaselineImage
		}
		if sameImage {
			imageHeaders = []string{"CONTAINER", "IMAGE ARCH", "NODE", "NODE ARCH"}
			for i, row := range images {
				images[i] = []string{row[0], row[2], row[3], row[4]}
			}
		}
		if len(images) > 0 {
			printBoxTable(imageHeaders, images)
		}
		mountRows := [][]string{}
		hiddenMounts := 0
		for _, container := range pod.Containers {
			for _, mount := range container.Mounts {
				if injectedMount(mount) {
					hiddenMounts++
					continue
				}
				status, endpoint := "", ""
				if index, ok := pvcInfo[mount.Source]; ok && mount.Type == "PVC" {
					pvc := view.PVCs[index]
					status, endpoint = pvc.Status, pvc.FrontendVolume
					seenPVCs[firstNonEmptyOutput(pvc.ClaimName, pvc.Name)] = true
				}
				mountRows = append(mountRows, []string{container.Name, mount.Name, mount.Type, emptyDash(mount.Source), mount.Path, emptyDash(mount.SubPath), fmt.Sprint(mount.ReadOnly), emptyDash(status), emptyDash(endpoint)})
			}
		}
		if len(mountRows) > 0 {
			printPodMountTable(mountRows)
		}
		if hiddenMounts > 0 {
			fmt.Fprintf(os.Stdout, "平台注入挂载：已折叠 %d 项\n", hiddenMounts)
		}
		for _, container := range pod.Containers {
			for _, log := range []struct {
				name      string
				lines     []string
				truncated bool
			}{{"current", container.Logs.Current, container.Logs.CurrentTruncated}, {"previous", container.Logs.Previous, container.Logs.PreviousTruncated}} {
				if strings.TrimSpace(strings.Join(log.lines, "\n")) == "" {
					continue
				}
				fmt.Fprintf(os.Stdout, "\n%s / %s / %s (tail 30):\n%s\n", pod.Name, container.Name, log.name, redactText(strings.Join(log.lines, "\n")))
				if log.truncated {
					fmt.Fprintln(os.Stdout, "Log limited to 1 MiB.")
				}
			}
		}
		for _, warning := range pod.Warnings {
			fmt.Fprintln(os.Stdout, "Warning:", redactText(warning))
		}
	}
	for _, warning := range result.Warnings {
		if !view.IncludeHistory && strings.HasPrefix(warning, "采样间隔约") {
			continue
		}
		fmt.Fprintln(os.Stdout, "Warning:", redactText(warning))
	}
	printIncrementPVCs(view.PVCs, seenPVCs)
}

func containerStateLabel(state any) string {
	values, ok := state.(map[string]any)
	if !ok {
		return "-"
	}
	if value, ok := values["terminated"].(*podevidence.Termination); ok {
		return fmt.Sprintf("%s exit=%d", value.Reason, value.ExitCode)
	}
	if value, ok := values["waiting"].(map[string]any); ok {
		return fmt.Sprint(value["reason"])
	}
	if _, ok := values["running"]; ok {
		return "Running"
	}
	return "-"
}
