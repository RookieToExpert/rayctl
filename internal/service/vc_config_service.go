package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"rayctl/internal/platform"
)

var vcControlPlaneResource = schema.GroupVersionResource{Group: "controlplane.cluster.x-k8s.io", Version: "v1alpha2", Resource: "vcluster2controlplanes"}

const vcFlavorLabel = "resource.compute.sensecore.cn/vc-flavor"

type VCConfigReader interface {
	Get(context.Context, string) (*platform.VCOperationResource, error)
	Flavors(context.Context) ([]platform.VCControlPlaneFlavor, error)
}

type VCControlPlaneSpec struct {
	Replicas int32             `json:"replicas"`
	Requests map[string]string `json:"requests"`
	Limits   map[string]string `json:"limits"`
}

type VCControlPlaneDeployment struct {
	VCControlPlaneSpec
	Generation         int64 `json:"generation"`
	ObservedGeneration int64 `json:"observed_generation"`
	Total              int32 `json:"total"`
	Updated            int32 `json:"updated"`
	Ready              int32 `json:"ready"`
	Available          int32 `json:"available"`
}

type VCControlPlaneConfig struct {
	Name          string                         `json:"name"`
	UID           string                         `json:"uid"`
	Environment   string                         `json:"environment"`
	HostNamespace string                         `json:"host_namespace"`
	Desired       *platform.VCControlPlaneFlavor `json:"desired,omitempty"`
	AppliedFlavor string                         `json:"applied_flavor"`
	FlavorSource  string                         `json:"flavor_source"`
	ControlPlane  *VCControlPlaneSpec            `json:"control_plane,omitempty"`
	Deployment    *VCControlPlaneDeployment      `json:"deployment,omitempty"`
	Converged     bool                           `json:"converged"`
	Warnings      []string                       `json:"warnings,omitempty"`
}

type VCConfigService struct {
	kube    kubernetes.Interface
	dynamic dynamic.Interface
	api     VCConfigReader
}

func NewVCConfigService(client kubernetes.Interface, dynamicClient dynamic.Interface, api VCConfigReader) *VCConfigService {
	return &VCConfigService{kube: client, dynamic: dynamicClient, api: api}
}

func (s *VCConfigService) Get(ctx context.Context, name, uid, environment string) (*VCControlPlaneConfig, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	ns := "vc-" + uid
	result := &VCControlPlaneConfig{Name: name, UID: uid, Environment: environment, HostNamespace: ns}
	var failures []error
	if s.api != nil {
		object, err := s.api.Get(ctx, uid)
		if err != nil {
			failures = append(failures, fmt.Errorf("平台期望规格: %w", err))
		} else if object == nil || object.ControlPlaneFlavor == nil || object.ControlPlaneFlavor.Name == "" {
			failures = append(failures, fmt.Errorf("平台未返回控制面规格"))
		} else {
			result.Desired = object.ControlPlaneFlavor
		}
	}
	var cp *unstructured.Unstructured
	if s.dynamic != nil {
		var err error
		cp, err = s.dynamic.Resource(vcControlPlaneResource).Namespace(ns).Get(ctx, ns, metav1.GetOptions{})
		if err != nil {
			failures = append(failures, fmt.Errorf("HC 控制面: %w", err))
		} else {
			result.AppliedFlavor = cp.GetLabels()[vcFlavorLabel]
			if result.AppliedFlavor != "" {
				result.FlavorSource = "VCluster2ControlPlane label"
			} else {
				failures = append(failures, fmt.Errorf("HC 控制面没有 flavor 标签，无法确认已下发规格"))
			}
			replicas, _, _ := unstructured.NestedInt64(cp.Object, "spec", "replicas")
			requests, _, _ := unstructured.NestedStringMap(cp.Object, "spec", "controlPlane", "statefulSet", "resources", "requests")
			limits, _, _ := unstructured.NestedStringMap(cp.Object, "spec", "controlPlane", "statefulSet", "resources", "limits")
			result.ControlPlane = &VCControlPlaneSpec{Replicas: int32(replicas), Requests: requests, Limits: limits}
		}
	}
	if s.kube != nil {
		deployment, err := s.kube.AppsV1().Deployments(ns).Get(ctx, ns, metav1.GetOptions{})
		if err != nil {
			failures = append(failures, fmt.Errorf("控制面 Deployment: %w", err))
		} else if cp == nil || !ownedByControlPlane(deployment, cp) {
			failures = append(failures, fmt.Errorf("无法确认 Deployment 属于该 VC 控制面"))
		} else if container := vcSyncer(deployment); container == nil {
			failures = append(failures, fmt.Errorf("无法唯一定位控制面 syncer 容器"))
		} else {
			replicas := int32(1)
			if deployment.Spec.Replicas != nil {
				replicas = *deployment.Spec.Replicas
			}
			result.Deployment = &VCControlPlaneDeployment{
				VCControlPlaneSpec: VCControlPlaneSpec{Replicas: replicas, Requests: quantityMap(container.Resources.Requests), Limits: quantityMap(container.Resources.Limits)},
				Generation:         deployment.Generation, ObservedGeneration: deployment.Status.ObservedGeneration,
				Total: deployment.Status.Replicas, Updated: deployment.Status.UpdatedReplicas,
				Ready: deployment.Status.ReadyReplicas, Available: deployment.Status.AvailableReplicas,
			}
		}
	}
	result.Converged = result.ConvergedFor(result.Desired)
	for _, err := range failures {
		result.Warnings = append(result.Warnings, err.Error())
	}
	if err := ctx.Err(); err != nil {
		return result, errors.Join(append(failures, err)...)
	}
	return result, errors.Join(failures...)
}

func ownedByControlPlane(deployment *appsv1.Deployment, cp *unstructured.Unstructured) bool {
	for _, owner := range deployment.OwnerReferences {
		if owner.Kind == "VCluster2ControlPlane" && owner.Name == cp.GetName() && owner.UID == cp.GetUID() && owner.UID != "" {
			return true
		}
	}
	return false
}

func vcSyncer(deployment *appsv1.Deployment) *corev1.Container {
	for index := range deployment.Spec.Template.Spec.Containers {
		if deployment.Spec.Template.Spec.Containers[index].Name == "syncer" {
			return &deployment.Spec.Template.Spec.Containers[index]
		}
	}
	if len(deployment.Spec.Template.Spec.Containers) == 1 {
		return &deployment.Spec.Template.Spec.Containers[0]
	}
	return nil
}

func quantityMap(values corev1.ResourceList) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[string(key)] = value.String()
	}
	return result
}

func sameVCResources(actual, expected map[string]string) bool {
	if len(actual) != len(expected) {
		return false
	}
	for key, value := range expected {
		left, leftErr := resource.ParseQuantity(actual[key])
		right, rightErr := resource.ParseQuantity(value)
		if leftErr != nil || rightErr != nil || left.Cmp(right) != 0 {
			return false
		}
	}
	return true
}

func (c *VCControlPlaneConfig) ConvergedFor(target *platform.VCControlPlaneFlavor) bool {
	if c == nil || target == nil || target.Name == "" || target.Replicas <= 0 || c.Desired == nil || c.Desired.Name != target.Name || c.AppliedFlavor != target.Name || c.ControlPlane == nil || c.Deployment == nil {
		return false
	}
	for _, spec := range []*VCControlPlaneSpec{c.ControlPlane, &c.Deployment.VCControlPlaneSpec} {
		if spec.Replicas != target.Replicas || !sameVCResources(spec.Requests, target.ResourceRequests) || !sameVCResources(spec.Limits, target.ResourceLimits) {
			return false
		}
	}
	d := c.Deployment
	return d.ObservedGeneration >= d.Generation && d.Total == target.Replicas && d.Updated == target.Replicas && d.Ready == target.Replicas && d.Available >= target.Replicas
}
