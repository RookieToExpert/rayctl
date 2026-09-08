package nodeexec

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

type Operation string

const (
	OperationCheckpoint   Operation = "checkpoint"
	OperationClock        Operation = "clock"
	OperationKernelLog    Operation = "kernel-log"
	OperationIBInterfaces Operation = "ib-interfaces"
)

const (
	execNamespace = "kube-system"
	execPodPrefix = "huawei-csi-node"
	execContainer = "huawei-csi-driver"
)

var operationCommands = map[Operation][]string{
	OperationCheckpoint: {
		"cat", "/var/lib/kubelet/device-plugins/kubelet_internal_checkpoint",
	},
	OperationClock: {
		"sh", "-c", "grep '^btime ' /proc/stat; head -n 1 /proc/uptime; date +%s",
	},
	OperationKernelLog: {
		"sh", "-c", "grep '^btime ' /proc/stat; date +%s; dmesg",
	},
	OperationIBInterfaces: {
		"ls", "/sys/class/net",
	},
}

type Runner interface {
	Run(context.Context, string, Operation) (string, error)
}

type KubernetesExecutor struct {
	config    *rest.Config
	client    kubernetes.Interface
	cacheMu   sync.Mutex
	podByNode map[string]string
	lookups   map[string]*podLookup
}

type podLookup struct {
	done chan struct{}
	pod  string
	err  error
}

func NewKubernetesExecutor(config *rest.Config, client kubernetes.Interface) *KubernetesExecutor {
	return &KubernetesExecutor{
		config:    rest.CopyConfig(config),
		client:    client,
		podByNode: make(map[string]string),
		lookups:   make(map[string]*podLookup),
	}
}

func (e *KubernetesExecutor) Run(ctx context.Context, node string, operation Operation) (string, error) {
	command, ok := operationCommands[operation]
	if !ok {
		return "", fmt.Errorf("unsupported node exec operation %q", operation)
	}
	if e == nil || e.config == nil || e.client == nil {
		return "", fmt.Errorf("node exec is unavailable")
	}
	pod, err := e.podForNode(ctx, node)
	if err != nil {
		return "", err
	}

	request := e.client.CoreV1().RESTClient().Post().
		Namespace(execNamespace).
		Resource("pods").
		Name(pod).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: execContainer,
			Command:   append([]string(nil), command...),
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)
	executor, err := remotecommand.NewSPDYExecutor(e.config, "POST", request.URL())
	if err != nil {
		return "", fmt.Errorf("prepare node exec on %s: %w", node, err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := executor.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: &stdout, Stderr: &stderr}); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return "", fmt.Errorf("exec %s on %s: %w: %s", operation, node, err, detail)
		}
		return "", fmt.Errorf("exec %s on %s: %w", operation, node, err)
	}
	return stdout.String(), nil
}

func (e *KubernetesExecutor) podForNode(ctx context.Context, node string) (string, error) {
	e.cacheMu.Lock()
	if pod := e.podByNode[node]; pod != "" {
		e.cacheMu.Unlock()
		return pod, nil
	}
	if lookup := e.lookups[node]; lookup != nil {
		e.cacheMu.Unlock()
		select {
		case <-lookup.done:
			return lookup.pod, lookup.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	lookup := &podLookup{done: make(chan struct{})}
	e.lookups[node] = lookup
	e.cacheMu.Unlock()

	pod, err := e.discoverPodForNode(ctx, node)
	e.cacheMu.Lock()
	lookup.pod = pod
	lookup.err = err
	if err == nil {
		e.podByNode[node] = pod
	}
	delete(e.lookups, node)
	close(lookup.done)
	e.cacheMu.Unlock()
	return pod, err
}

func (e *KubernetesExecutor) discoverPodForNode(ctx context.Context, node string) (string, error) {
	pods, err := e.client.CoreV1().Pods(execNamespace).List(ctx, metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("spec.nodeName", node).String(),
	})
	if err != nil {
		return "", fmt.Errorf("list node exec pods on %s: %w", node, err)
	}
	var nonRunning string
	for _, pod := range pods.Items {
		if !strings.HasPrefix(pod.Name, execPodPrefix) || !hasContainer(pod, execContainer) {
			continue
		}
		if pod.Status.Phase == corev1.PodRunning {
			return pod.Name, nil
		}
		nonRunning = fmt.Sprintf("%s(%s)", pod.Name, pod.Status.Phase)
	}
	if nonRunning != "" {
		return "", fmt.Errorf("node exec pod on %s is not Running: %s", node, nonRunning)
	}
	return "", fmt.Errorf("node exec pod %s* was not found on %s", execPodPrefix, node)
}

func hasContainer(pod corev1.Pod, name string) bool {
	for _, container := range pod.Spec.Containers {
		if container.Name == name {
			return true
		}
	}
	return false
}
