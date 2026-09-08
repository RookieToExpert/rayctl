package podevidence

import (
	"context"
	"io"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

func readLogs(ctx context.Context, client kubernetes.Interface, pod corev1.Pod, container string, previous bool) ([]string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tail := int64(30)
	stream, err := client.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{Container: container, Previous: previous, TailLines: &tail, Timestamps: true}).Stream(ctx)
	if err != nil {
		return nil, false, err
	}
	defer stream.Close()
	const maxBytes = 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(stream, maxBytes+1))
	if err != nil {
		return nil, false, err
	}
	truncated := len(data) > maxBytes
	if truncated {
		data = data[:maxBytes]
	}
	if len(data) == 0 {
		return []string{}, false, nil
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	for i, line := range lines {
		stamp, message, _ := strings.Cut(line, " ")
		if value, err := time.Parse(time.RFC3339Nano, stamp); err == nil {
			lines[i] = value.In(time.FixedZone("UTC+8", 8*3600)).Format(time.RFC3339Nano) + " " + message
		}
	}
	return lines, truncated, nil
}
