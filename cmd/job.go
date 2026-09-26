package cmd

import (
	"github.com/spf13/cobra"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"rayctl/internal/kube"
	"time"
)

const defaultJobGetTimeout = 10 * time.Second

func newATECmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ate",
		Short: "查询或创建 ATE VCJob",
	}
	cmd.AddCommand(newECPJobListCmd())
	cmd.AddCommand(newECPJobGetCmd())
	cmd.AddCommand(newJobCreateCmd())
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
