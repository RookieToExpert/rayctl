package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestPodEnvironmentLookupUsesMatchingHostClient(t *testing.T) {
	d := fake.NewClientset()
	pt := fake.NewClientset(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pt-pod", Namespace: "pt-ns"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}})
	targets := []HCEnvironment{{Name: "d", Region: "cn-pj-01", Client: d}, {Name: "pt", Region: "cn-pj-03", Client: pt}}
	query := func(ctx context.Context, env HCEnvironment) (*PodLocateResult, error) {
		r, err := NewPodLocateService(env.Client, nil).Get(ctx, "pt-pod", "", false)
		if r != nil {
			r.Environment = env.Name
		}
		return r, err
	}
	missing := func(err error) bool { var e *PodNotFoundError; return errors.As(err, &e) }
	got, err := QueryHCEnvironments(context.Background(), targets, "pt-pod", query, missing)
	if err != nil || got == nil || got.Environment != "pt" || got.HostNamespace != "pt-ns" {
		t.Fatalf("%#v %v", got, err)
	}
	if len(d.Actions()) != 1 || len(pt.Actions()) != 1 {
		t.Fatalf("wrong HC scope: D=%v PT=%v", d.Actions(), pt.Actions())
	}
	_, err = QueryHCEnvironments(context.Background(), targets[:1], "pt-pod", query, missing)
	if err == nil || !strings.Contains(err.Error(), "d/cn-pj-01") || strings.Contains(err.Error(), "pt/cn-pj-03") {
		t.Fatalf("explicit D crossed scope: %v", err)
	}
}
