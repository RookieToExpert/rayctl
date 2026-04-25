package cmd

import (
	"context"

	"github.com/spf13/cobra"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"rayctl/internal/kube"
	"rayctl/internal/service"
	"rayctl/pkg/output"
)

func newJobCmd() *cobra.Command {
	jobCmd := &cobra.Command{
		Use:   "job",
		Short: "查询 Volcano Job 和 PodGroup",
	}

	jobCmd.AddCommand(newJobGetCmd())
	return jobCmd
}

func newJobGetCmd() *cobra.Command {
	getCmd := &cobra.Command{
		Use:   "get",
		Short: "查询 job 或 podgroup",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientset, dynamicClient, err := newJobClients()
			if err != nil {
				return err
			}

			jobService := service.NewJobService(clientset, dynamicClient)
			result, err := jobService.GetJob(context.Background(), args[0])
			if err != nil {
				return err
			}

			output.PrintJobDetail(result)
			return nil
		},
	}

	getCmd.AddCommand(newJobGetPodGroupCmd())
	return getCmd
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

			jobService := service.NewJobService(clientset, dynamicClient)
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
