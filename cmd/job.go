package cmd

import (
	"context"

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
		Use:   "job <job-name-or-pod-name-or-uid>",
		Short: "根据任务名、Pod 名或 UID 查询 Job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientset, dynamicClient, err := newJobClients()
			if err != nil {
				return err
			}

			vcClient, _ := platform.NewVirtualClusterClientFromEnv()
			jobService := service.NewJobService(clientset, dynamicClient, vcClient)
			result, err := jobService.GetJob(context.Background(), args[0])
			if err != nil {
				return err
			}

			output.PrintJobDetail(result, debugTiming)
			return nil
		},
	}

	cmd.Flags().BoolVar(&debugTiming, "debug-timing", false, "Print timing diagnostics for job get")
	return cmd
}

func newJobGetPodGroupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "pg <podgroup-name-or-uid>",
		Aliases: []string{"podgroup"},
		Short:   "根据名称或 UID 查询 PodGroup",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientset, dynamicClient, err := newJobClients()
			if err != nil {
				return err
			}

			vcClient, _ := platform.NewVirtualClusterClientFromEnv()
			jobService := service.NewJobService(clientset, dynamicClient, vcClient)
			result, err := jobService.GetPodGroup(context.Background(), args[0])
			if err != nil {
				return err
			}

			output.PrintPodGroupDetail(result)
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
		Use:   "check <job-name>",
		Short: "检查 Job 为什么起不来",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientset, dynamicClient, err := newJobClients()
			if err != nil {
				return err
			}

			vcClient, _ := platform.NewVirtualClusterClientFromEnv()
			jobService := service.NewJobService(clientset, dynamicClient, vcClient)
			result, err := jobService.CheckJob(context.Background(), args[0])
			if err != nil {
				return err
			}

			output.PrintJobCheckDetail(result)
			return nil
		},
	}

	return cmd
}
