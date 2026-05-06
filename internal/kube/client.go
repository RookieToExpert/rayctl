package kube

import (
	"fmt"
	"os"
	"strings"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const defaultKubeconfigPath = "/root/kubeconfig"
const (
	defaultClientQPS   = 50
	defaultClientBurst = 100
)

func NewRestConfig(kubeconfig string) (*rest.Config, error) {
	configPath := resolveKubeconfigPath(kubeconfig)

	restConfig, err := clientcmd.BuildConfigFromFlags("", configPath)
	if err != nil {
		return nil, fmt.Errorf("build kubeconfig from %q: %w", configPath, err)
	}

	// Raise client-side throttling limits for read-heavy CLI workflows such as
	// node/job inspection to avoid noisy throttling logs on large clusters.
	restConfig.QPS = defaultClientQPS
	restConfig.Burst = defaultClientBurst

	return restConfig, nil
}

func NewClientset(kubeconfig string) (*kubernetes.Clientset, error) {
	restConfig, err := NewRestConfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes clientset: %w", err)
	}

	return clientset, nil
}

func NewDynamicClient(kubeconfig string) (dynamic.Interface, error) {
	restConfig, err := NewRestConfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	client, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}

	return client, nil
}

func resolveKubeconfigPath(kubeconfig string) string {
	if path := strings.TrimSpace(kubeconfig); path != "" {
		return path
	}
	if path := strings.TrimSpace(os.Getenv("KUBECONFIG")); path != "" {
		return path
	}
	return defaultKubeconfigPath
}
