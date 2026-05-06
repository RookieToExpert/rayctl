package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"rayctl/internal/kube"
	"rayctl/internal/platform"
	"rayctl/internal/service"
	"rayctl/pkg/output"
)

func newJobCmd() *cobra.Command {
	jobCmd := &cobra.Command{
		Use:   "job",
		Short: "查询 Volcano Job 和 PodGroup",
	}

	jobCmd.AddCommand(newJobGetCmd())
	jobCmd.AddCommand(newJobCheckCmd())
	return jobCmd
}

func newJobGetCmd() *cobra.Command {
	getCmd := &cobra.Command{
		Use:   "get",
		Short: "查询 job 或 podgroup",
	}

	getCmd.AddCommand(newJobGetJobCmd())
	getCmd.AddCommand(newJobGetPodGroupCmd())
	return getCmd
}

func newJobGetJobCmd() *cobra.Command {
	var debugTiming bool

	cmd := &cobra.Command{
		Use:   "job <job-name-or-pod-name-or-uid> [job-name-or-pod-name-or-uid...]",
		Short: "根据任务名、Pod 名或 UID 查询 Job",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientset, dynamicClient, err := newJobClients()
			if err != nil {
				return err
			}

			vcClient, _ := platform.NewVirtualClusterClientFromEnv()
			jobService := service.NewJobService(clientset, dynamicClient, vcClient)
			for i, identifier := range args {
				result, err := jobService.GetJob(context.Background(), identifier)
				if err != nil {
					return fmt.Errorf("job %q: %w", identifier, err)
				}
				if vcClient != nil {
					if vcUID := virtualClusterUIDFromName(result.VClusterName); vcUID != "" {
						displayNames, err := vcClient.ResolveDisplayNames(context.Background(), []string{vcUID})
						if err == nil {
							if displayName, ok := displayNames[vcUID]; ok {
								result.VClusterName = displayName
							}
						}
					}
				}
				if i > 0 {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				output.PrintJobDetail(result, debugTiming)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&debugTiming, "debug-timing", false, "Print timing diagnostics for job get")
	return cmd
}

func virtualClusterUIDFromName(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "vc-") {
		candidate := strings.TrimPrefix(value, "vc-")
		if looksLikeUUID(candidate) {
			return candidate
		}
	}
	if looksLikeUUID(value) {
		return value
	}
	return ""
}

func looksLikeUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, ch := range value {
		switch i {
		case 8, 13, 18, 23:
			if ch != '-' {
				return false
			}
		default:
			if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') && (ch < 'A' || ch > 'F') {
				return false
			}
		}
	}
	return true
}

func newJobGetPodGroupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "pg <podgroup-name-or-uid> [podgroup-name-or-uid...]",
		Aliases: []string{"podgroup"},
		Short:   "根据名称或 UID 查询 PodGroup",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientset, dynamicClient, err := newJobClients()
			if err != nil {
				return err
			}

			vcClient, _ := platform.NewVirtualClusterClientFromEnv()
			jobService := service.NewJobService(clientset, dynamicClient, vcClient)
			for i, identifier := range args {
				result, err := jobService.GetPodGroup(context.Background(), identifier)
				if err != nil {
					return fmt.Errorf("podgroup %q: %w", identifier, err)
				}
				if i > 0 {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				output.PrintPodGroupDetail(result)
			}
			return nil
		},
	}

	return cmd
}

func newJobClients() (kubernetes.Interface, dynamic.Interface, error) {
	clientset, err := kube.NewClientset(kubeconfig)
	if err != nil {
		return nil, nil, err
	}

	dynamicClient, err := kube.NewDynamicClient(kubeconfig)
	if err != nil {
		return nil, nil, err
	}

	return clientset, dynamicClient, nil
}

func newJobCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check <job-name> [job-name...]",
		Short: "检查 Job 为什么起不来",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientset, dynamicClient, err := newJobClients()
			if err != nil {
				return err
			}

			vcClient, _ := platform.NewVirtualClusterClientFromEnv()
			jobService := service.NewJobService(clientset, dynamicClient, vcClient)
			for i, identifier := range args {
				result, err := jobService.CheckJob(context.Background(), identifier)
				if err != nil {
					return fmt.Errorf("job %q: %w", identifier, err)
				}
				if i > 0 {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				output.PrintJobCheckDetail(result)
			}
			return nil
		},
	}

	return cmd
}
