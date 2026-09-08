package nodehealth

import (
	"strconv"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestMindXNPUDetectsGhostPreSeparate(t *testing.T) {
	now := time.Unix(1788504988, 0)
	nodeName := "host-10-140-70-107"
	client := fake.NewSimpleClientset(
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "mindx-dl-nodeinfo-" + nodeName, Namespace: "mindx-dl"},
			Data:       map[string]string{"NodeInfo": `{"NodeInfo":{"FaultDevList":[],"NodeStatus":"PreSeparate"}}`},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "mindx-dl-deviceinfo-" + nodeName, Namespace: "kube-system"},
			Data:       map[string]string{"DeviceInfoCfg": `{"DeviceInfo":{"DeviceList":{"huawei.com/Ascend910-Fault":"[]","huawei.com/Ascend910-NetworkUnhealthy":""},"UpdateTime":` + strconv.FormatInt(now.Unix(), 10) + `}}`},
		},
	)
	node := healthyNode(nodeName)
	service := NewService(client, nil, nil, "d")
	result, err := service.checkMindXNPU(t.Context(), node, now)
	if err != nil {
		t.Fatalf("checkMindXNPU() error = %v", err)
	}
	if result.Status != StatusFail {
		t.Fatalf("checkMindXNPU() status = %s, want FAIL; evidence=%#v", result.Status, result.Evidence)
	}
	if result.Evidence["nodeinfo_status"] != "PreSeparate" || result.Evidence["device_healthy"] != true || result.Evidence["node_ready"] != true {
		t.Fatalf("ghost-state evidence is incomplete: %#v", result.Evidence)
	}
}

func TestMindXNPUWithoutNodeInfoIsOK(t *testing.T) {
	service := NewService(fake.NewSimpleClientset(), nil, nil, "d")
	result, err := service.checkMindXNPU(t.Context(), healthyNode("host-1"), time.Now())
	if err != nil {
		t.Fatalf("checkMindXNPU() error = %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("checkMindXNPU() status = %s, want OK", result.Status)
	}
}

func TestMindXNPUSkipsOutsideD(t *testing.T) {
	service := NewService(fake.NewSimpleClientset(), nil, nil, "pt")
	result, err := service.checkMindXNPU(t.Context(), healthyNode("host-1"), time.Now())
	if err != nil || result.Status != StatusSkip {
		t.Fatalf("checkMindXNPU() = %#v, %v, want SKIP", result, err)
	}
}

func healthyNode(name string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{
			Type: corev1.NodeReady, Status: corev1.ConditionTrue,
		}}},
	}
}
