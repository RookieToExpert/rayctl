package kube

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	defaultKubeconfigFileName = "kubeconfig"
	defaultClientQPS          = 50
	defaultClientBurst        = 100
)

type KubeconfigIdentity struct {
	Path           string
	CurrentContext string
	User           string
	Cluster        string
}

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

func ResolvedKubeconfigPath(kubeconfig string) string {
	return resolveKubeconfigPath(kubeconfig)
}

func ResolveKubeconfigIdentity(kubeconfig string) (*KubeconfigIdentity, error) {
	resolvedPath := resolveKubeconfigPath(kubeconfig)

	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	switch {
	case strings.TrimSpace(kubeconfig) != "":
		rules.Precedence = []string{strings.TrimSpace(kubeconfig)}
	case strings.TrimSpace(os.Getenv("KUBECONFIG")) != "":
		rules.Precedence = filepath.SplitList(strings.TrimSpace(os.Getenv("KUBECONFIG")))
	default:
		rules.Precedence = []string{defaultKubeconfigPath()}
	}

	if !hasExistingKubeconfig(rules.Precedence) {
		return nil, fmt.Errorf("当前环境变量找不到相应的 kubeconfig，请检查 -k 参数或 KUBECONFIG 环境变量")
	}

	config, err := rules.Load()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}

	currentContext := strings.TrimSpace(config.CurrentContext)
	if currentContext == "" {
		return nil, fmt.Errorf("当前 kubeconfig 没有 current-context")
	}

	ctx, ok := config.Contexts[currentContext]
	if !ok || ctx == nil {
		return nil, fmt.Errorf("当前 kubeconfig 的 context %q 不存在", currentContext)
	}

	user := strings.TrimSpace(ctx.AuthInfo)
	if user == "" {
		return nil, fmt.Errorf("当前 kubeconfig 的 context %q 没有关联 user", currentContext)
	}

	cluster := strings.TrimSpace(ctx.Cluster)
	if cluster == "" {
		return nil, fmt.Errorf("当前 kubeconfig 的 context %q 没有关联 cluster", currentContext)
	}

	return &KubeconfigIdentity{
		Path:           resolvedPath,
		CurrentContext: currentContext,
		User:           user,
		Cluster:        cluster,
	}, nil
}

func resolveKubeconfigPath(kubeconfig string) string {
	if path := strings.TrimSpace(kubeconfig); path != "" {
		return path
	}
	if path := strings.TrimSpace(os.Getenv("KUBECONFIG")); path != "" {
		return path
	}
	return defaultKubeconfigPath()
}

func defaultKubeconfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return defaultKubeconfigFileName
	}
	return filepath.Join(home, defaultKubeconfigFileName)
}

func hasExistingKubeconfig(paths []string) bool {
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		if _, err := os.Stat(trimmed); err == nil {
			return true
		}
	}
	return false
}
