package service

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"rayctl/internal/platform"
	"rayctl/internal/podevidence"
)

type PodNameResolver interface {
	ResolveDisplayNamesWithProfiles(context.Context, []string) (map[string]string, map[string]string, error)
}

type PodLocateService struct {
	client      kubernetes.Interface
	resolver    PodNameResolver
	storage     *StorageService
	queueLookup func(context.Context, string) (*platform.SSPQueueResourceDetails, error)
}

type PodLocateResult struct {
	Environment   string
	HostNamespace string
	HostPod       string
	VCPod         string
	VCNamespace   string
	WorkloadType  string
	WorkloadName  string
	WorkloadUID   string
	Workspace     string
	Queue         string
	Cluster       string
	VC            string
	Creator       string
	Tenant        string
	Node          string
	NodeIP        string
	PodIP         string
	Phase         string
	Status        string
	Ready         bool
	Restarts      int64
	DeletingFor   string
	PVCs          []PodPVCResult
	Suggestions   []string
	Warnings      []string
}

type PodNotFoundError struct{ Identifier string }

func (e *PodNotFoundError) Error() string { return fmt.Sprintf("pod %q not found", e.Identifier) }

type PodPVCResult struct {
	Namespace string
	Name      string
	AFS       string
	Warning   string
}

func NewPodLocateService(client kubernetes.Interface, platformClient *platform.VirtualClusterClient) *PodLocateService {
	s := &PodLocateService{client: client, storage: NewStorageService(client, platformClient)}
	if platformClient != nil {
		s.resolver = platformClient
		s.queueLookup = func(ctx context.Context, identifier string) (*platform.SSPQueueResourceDetails, error) {
			return platformClient.GetSSPQueueResource(ctx, platformClient.CurrentProfileName(), identifier)
		}
	}
	return s
}

func (s *PodLocateService) Get(ctx context.Context, identifier, namespace string, long bool) (*PodLocateResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	identifier, namespace = strings.TrimSpace(identifier), strings.TrimSpace(namespace)
	if strings.Contains(identifier, "/") {
		parts := strings.Split(identifier, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("expected host-namespace/pod-name")
		}
		if namespace != "" && namespace != parts[0] {
			return nil, fmt.Errorf("namespace %q conflicts with %q", namespace, parts[0])
		}
		namespace, identifier = parts[0], parts[1]
	}
	if identifier == "" {
		return nil, fmt.Errorf("pod name or IP is required")
	}
	var pods []corev1.Pod
	isIP := net.ParseIP(identifier) != nil
	if namespace != "" && !isIP {
		pod, err := s.client.CoreV1().Pods(namespace).Get(ctx, identifier, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("get pod %s/%s: %w", namespace, identifier, err)
		}
		pods = append(pods, *pod)
	} else {
		field := "metadata.name"
		if isIP {
			field = "status.podIP"
		}
		options := metav1.ListOptions{FieldSelector: fields.OneTermEqualSelector(field, identifier).String(), Limit: 500}
		seen := map[string]bool{}
		for {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			list, err := s.client.CoreV1().Pods(namespace).List(ctx, options)
			if err != nil {
				return nil, fmt.Errorf("locate pod %q (use -n to restrict namespace if access is denied): %w", identifier, err)
			}
			for _, pod := range list.Items {
				if (!isIP && pod.Name == identifier) || (isIP && pod.Status.PodIP == identifier) {
					pods = append(pods, pod)
				}
			}
			if list.Continue == "" {
				break
			}
			if seen[list.Continue] {
				return nil, fmt.Errorf("pod pagination token repeated")
			}
			seen[list.Continue], options.Continue = true, list.Continue
		}
	}
	if len(pods) == 0 {
		return nil, &PodNotFoundError{Identifier: identifier}
	}
	if len(pods) > 1 {
		matches := make([]string, 0, len(pods))
		for _, pod := range pods {
			matches = append(matches, pod.Namespace+"/"+pod.Name)
		}
		sort.Strings(matches)
		return nil, fmt.Errorf("multiple pods matched %q; specify host-namespace/pod-name: %s", identifier, strings.Join(matches, ", "))
	}
	pod := pods[0]
	r := podLocateResult(pod)
	// Optional enrichment has a separate, shared budget; Kubernetes identity remains usable on failure.
	enrichCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if s.queueLookup != nil && r.Queue != "" && (r.VC == "" || looksLikePodVCUID(r.VC)) {
		queue, err := s.queueLookup(enrichCtx, r.Queue)
		if err != nil {
			r.Warnings = append(r.Warnings, "resolve queue: "+err.Error())
		} else if queue != nil {
			r.VC = firstNonEmpty(queue.VClusterName, r.VC)
			r.Queue = firstNonEmpty(queue.Name, r.Queue)
			r.Cluster = firstNonEmpty(r.Cluster, queue.ClusterName)
			r.Workspace = firstNonEmpty(r.Workspace, queue.WorkspaceName)
		}
	}
	if s.resolver != nil && looksLikePodVCUID(r.VC) {
		uid := strings.TrimPrefix(r.VC, "vc-")
		names, _, err := s.resolver.ResolveDisplayNamesWithProfiles(enrichCtx, []string{uid})
		if err != nil {
			r.Warnings = append(r.Warnings, "resolve VC: "+err.Error())
		} else if names[uid] != "" {
			r.VC = names[uid]
		}
	}
	if long {
		seen := map[string]bool{}
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim == nil {
				continue
			}
			name := volume.PersistentVolumeClaim.ClaimName
			if seen[name] {
				continue
			}
			seen[name] = true
			item := PodPVCResult{Namespace: pod.Namespace, Name: name}
			pvc, err := s.client.CoreV1().PersistentVolumeClaims(pod.Namespace).Get(enrichCtx, name, metav1.GetOptions{})
			if err != nil {
				item.Warning = err.Error()
			} else {
				item.AFS, _, _ = s.storage.resolvePVCFrontendInfo(enrichCtx, pvc)
				if item.AFS == "" || item.AFS == "-" {
					item.Warning = "AFS mapping unavailable"
				}
			}
			r.PVCs = append(r.PVCs, item)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r, nil
}

func looksLikePodVCUID(value string) bool {
	return looksLikeUUID(strings.TrimPrefix(value, "vc-"))
}

func podLocateResult(pod corev1.Pod) *PodLocateResult {
	value := func(key string) string { return firstNonEmpty(pod.Annotations[key], pod.Labels[key]) }
	identity := jobIdentityFromPod(pod)
	r := &PodLocateResult{
		HostNamespace: pod.Namespace, HostPod: pod.Name,
		VCPod: value("vcluster.loft.sh/object-name"), VCNamespace: firstNonEmpty(value("vcluster.loft.sh/object-namespace"), value("vcluster.loft.sh/namespace")),
		WorkloadType: "UNKNOWN", WorkloadName: value(sspWorkloadNameLabel), WorkloadUID: value(sspWorkloadUIDLabel),
		Workspace: value(sspWorkspaceNameLabel), Queue: firstNonEmpty(value(nodeQueueNameLabelKey), value(sspQueueUIDLabel)),
		Cluster: value(sspClusterNameLabel), VC: identity.VClusterName,
		Creator: firstNonEmpty(value("identity.compute.sensecore.cn/user-name"), value("identity.compute.sensecore.cn/user-id"), value("lepton.sensetime.com/submitter")),
		Tenant:  firstNonEmpty(value("identity.compute.sensecore.cn/tenant-name"), value(sspIdentityTenantIDLabel), value(sspTenantIDLabel)),
		Node:    pod.Spec.NodeName, NodeIP: pod.Status.HostIP, PodIP: pod.Status.PodIP,
		Phase: string(pod.Status.Phase), Status: nodePodStatus(pod), Ready: isPodReady(pod),
	}
	r.Restarts, _ = podevidence.RestartSummary([]corev1.Pod{pod})
	if pod.DeletionTimestamp != nil {
		r.DeletingFor = max(time.Duration(0), time.Since(pod.DeletionTimestamp.Time)).Round(time.Second).String()
	}
	switch strings.ToLower(value(sspWorkloadTypeLabel)) {
	case "training-job", "ait":
		r.WorkloadType = "AIT"
	case "aid":
		r.WorkloadType = "AID"
	case "air", "inference", "inference-job", "inference-service":
		r.WorkloadType = "AIR"
	case "ate":
		r.WorkloadType = "ATE"
	case "ecs":
		r.WorkloadType = "ECS"
	}
	var vmName, vmUID string
	var hasVMOwner bool
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "VirtualMachineInstance" {
			hasVMOwner = true
			vmName, vmUID = owner.Name, string(owner.UID)
			break
		}
	}
	domain := value("kubevirt.io/domain")
	if r.WorkloadType == "UNKNOWN" && (hasVMOwner || domain != "" || value("kubevirt.io") == "virt-launcher") {
		r.WorkloadType = "ECS"
	}
	if r.WorkloadType == "ECS" {
		r.WorkloadName = firstNonEmpty(value("resource.compute.sensecore.cn/name"), r.WorkloadName, value("vm.kubevirt.io/name"), domain, vmName)
		// The VMI owner UID belongs to Kubernetes; prefer the platform resource UID.
		r.WorkloadUID = firstNonEmpty(value("resource.compute.sensecore.cn/uid"), value("resourcemanager_id"), r.WorkloadUID, vmUID)
	}
	if r.WorkloadType == "UNKNOWN" && value(sspWorkloadTypeLabel) == "" && identity.Name != "" {
		r.WorkloadType, r.WorkloadName, r.WorkloadUID = "ATE", identity.Name, identity.UID
	}
	if r.WorkloadType == "UNKNOWN" && pod.Namespace == "kube-system" {
		r.WorkloadType = "SYSTEM"
	}
	if r.WorkloadName != "" {
		command := map[string]string{"AIT": "ait get", "AID": "aid get", "AIR": "air job get", "ATE": "ate get", "ECS": "ecs get"}[r.WorkloadType]
		if command != "" {
			r.Suggestions = append(r.Suggestions, "rayctl "+command+" "+r.WorkloadName)
		}
	}
	if r.Node != "" {
		r.Suggestions = append(r.Suggestions, "rayctl node health "+r.Node)
	}
	return r
}
