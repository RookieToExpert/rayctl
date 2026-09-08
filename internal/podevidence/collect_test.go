package podevidence

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestTerminationRequiresValidTimes(t *testing.T) {
	if termination(&corev1.ContainerStateTerminated{}).InstantExit != nil {
		t.Fatal("missing timestamps are not instant exits")
	}
	now := metav1.NewTime(time.Now())
	result := termination(&corev1.ContainerStateTerminated{StartedAt: now, FinishedAt: now, ExitCode: 255})
	if result.InstantExit == nil || !*result.InstantExit {
		t.Fatal(result)
	}
	result = termination(&corev1.ContainerStateTerminated{StartedAt: now, FinishedAt: metav1.NewTime(now.Add(-time.Second))})
	if result.InstantExit != nil {
		t.Fatal("negative duration accepted")
	}
}

func TestSelectFindsRestartingPodBeyondFirstPageAndKeepsInput(t *testing.T) {
	pods := make([]corev1.Pod, 21)
	for i := range pods {
		pods[i].Name = "healthy"
		pods[i].Status.Phase = corev1.PodRunning
		pods[i].Status.ContainerStatuses = []corev1.ContainerStatus{{Ready: true}}
	}
	pods[20].Name = "crashing"
	pods[20].Status.ContainerStatuses[0].RestartCount = 82
	selected := Select(pods, 3)
	if len(selected) != 3 || selected[0].Name != "crashing" || pods[0].Name != "healthy" {
		t.Fatal(selected)
	}
}

func TestCollectIncludesCompletedInitAndAllContainers(t *testing.T) {
	pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "ns", UID: types.UID("uid")}, Spec: corev1.PodSpec{InitContainers: []corev1.Container{{Name: "init", Image: "registry.test/a/b:v1"}}, Containers: []corev1.Container{{Name: "app", Image: "registry.test/a/c:v1"}}}}
	pod.Status.InitContainerStatuses = []corev1.ContainerStatus{{Name: "init", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "Completed", ExitCode: 0}}}}
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "app", RestartCount: 6}}
	result := Collect(context.Background(), fake.NewSimpleClientset(&pod), []corev1.Pod{pod}, Options{NoImage: true})
	if len(result.Pods) != 1 || len(result.Pods[0].Containers) != 2 || result.Restarts != 6 {
		t.Fatal(result)
	}
	init := result.Pods[0].Containers[0]
	if init.Kind != "init" || init.State.(map[string]any)["terminated"].(*Termination).Reason != "Completed" {
		t.Fatal(init)
	}
	if init.Comparison.Match != nil {
		t.Fatal("unknown architecture must not compare false")
	}
}

func TestArtifactURLUsesDigestAndDoubleEscapesRepository(t *testing.T) {
	url, host, err := artifactURL("registry.test/project/team/image:v1", "docker-pullable://registry.test/project/team/image@sha256:abc")
	if err != nil || host != "registry.test" || url != "https://registry.test/api/v2.0/projects/project/repositories/team%252Fimage/artifacts/sha256:abc?with_tag=true" {
		t.Fatal(url, host, err)
	}
}
