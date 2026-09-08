package output

import (
	"fmt"
	"os"
	"strings"
	"time"

	"rayctl/internal/podevidence"
)

func printPodEvidence(result *podevidence.Result) {
	if result == nil {
		return
	}
	fmt.Fprintf(os.Stdout, "\nPOD EVIDENCE: %d/%d (omitted %d), cumulative restarts %d\n", len(result.Pods), result.Total, result.Omitted, result.Restarts)
	if result.Restarts24h != nil {
		fmt.Fprintf(os.Stdout, "Observed restarts in 24h (%d inspected Pods): %d\n", len(result.HistoryPods), *result.Restarts24h)
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
		fmt.Fprintf(os.Stdout, "\nPod %s/%s (%s)\n", pod.Namespace, pod.Name, pod.UID)
		printBoxTable([]string{"CONTAINER", "KIND", "READY", "RESTARTS", "STATE", "LAST TERMINATION", "EXIT", "INSTANT EXIT"}, rows)
		printBoxTable([]string{"CONTAINER", "IMAGE", "IMAGE ARCH", "NODE", "NODE ARCH"}, images)
		for _, container := range pod.Containers {
			for _, log := range []struct {
				name      string
				lines     []string
				truncated bool
			}{{"current", container.Logs.Current, container.Logs.CurrentTruncated}, {"previous", container.Logs.Previous, container.Logs.PreviousTruncated}} {
				if log.lines == nil {
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
		fmt.Fprintln(os.Stdout, "Warning:", redactText(warning))
	}
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
