package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"

	"rayctl/internal/platform"
)

type StorageService struct {
	clientset kubernetes.Interface
	vcClient  *platform.VirtualClusterClient
}

type AFSCheckResult struct {
	AFSName     string
	HostPVs     []string
	HostPVCs    []string
	VirtualPVCs []string
}

type PVCCheckItemResult struct {
	PVCName   string
	AFSName   string
	Partition string
	JobNames  []string
}

type PVCCheckResult struct {
	Items []PVCCheckItemResult
}

func NewStorageService(clientset kubernetes.Interface, vcClient *platform.VirtualClusterClient) *StorageService {
	return &StorageService{
		clientset: clientset,
		vcClient:  vcClient,
	}
}

func (s *StorageService) CheckAFS(ctx context.Context, identifier string) (*AFSCheckResult, error) {
	if s.vcClient == nil {
		return nil, fmt.Errorf("afs check requires platform configuration")
	}

	resource, err := s.vcClient.FindStorageVolumeResource(ctx, identifier)
	if err != nil {
		return nil, err
	}

	hostPVs, hostPVCs, err := s.findHostVolumesForAFS(ctx, resource.ID)
	if err != nil {
		return nil, err
	}
	if len(hostPVs) == 0 {
		return nil, fmt.Errorf("no host pv found for afs %q", resource.Name)
	}

	virtualPVCs, err := s.findVirtualPVCsForHostPVs(ctx, hostPVs)
	if err != nil {
		return nil, err
	}

	return &AFSCheckResult{
		AFSName:     firstNonEmpty(resource.Name, resource.DisplayName, resource.ID),
		HostPVs:     hostPVs,
		HostPVCs:    hostPVCs,
		VirtualPVCs: virtualPVCs,
	}, nil
}

func (s *StorageService) CheckPVC(ctx context.Context, pvcName string) (*PVCCheckResult, error) {
	pvcName = strings.TrimSpace(pvcName)
	if pvcName == "" {
		return nil, fmt.Errorf("pvc name is required")
	}

	pvcs, err := s.clientset.CoreV1().PersistentVolumeClaims(metav1.NamespaceAll).List(ctx, metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("metadata.name", pvcName).String(),
	})
	if err != nil {
		return nil, fmt.Errorf("list pvc %q: %w", pvcName, err)
	}
	if len(pvcs.Items) == 0 {
		return nil, fmt.Errorf("pvc %q not found", pvcName)
	}

	items := make([]PVCCheckItemResult, 0, len(pvcs.Items))
	seen := make(map[string]struct{})
	for i := range pvcs.Items {
		afsName, partitionName := s.resolvePVCFrontendInfo(ctx, &pvcs.Items[i])
		key := fmt.Sprintf("%s|%s|%s", pvcs.Items[i].Name, afsName, partitionName)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		items = append(items, PVCCheckItemResult{
			PVCName:   pvcs.Items[i].Name,
			AFSName:   afsName,
			Partition: partitionName,
			JobNames:  s.findJobsUsingPVC(ctx, &pvcs.Items[i]),
		})
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].PVCName == items[j].PVCName {
			return items[i].AFSName < items[j].AFSName
		}
		return items[i].PVCName < items[j].PVCName
	})

	return &PVCCheckResult{Items: items}, nil
}

func (s *StorageService) findHostVolumesForAFS(ctx context.Context, resourceUID string) ([]string, []string, error) {
	pvs, err := s.clientset.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("list persistentvolumes: %w", err)
	}

	hostPVs := make([]string, 0)
	hostPVCs := make([]string, 0)
	seenPV := make(map[string]struct{})
	seenPVC := make(map[string]struct{})

	for _, pv := range pvs.Items {
		if pv.Spec.ClaimRef == nil {
			continue
		}
		claimName := strings.TrimSpace(pv.Spec.ClaimRef.Name)
		if claimName == "" || !strings.Contains(claimName, resourceUID) {
			continue
		}
		if _, ok := seenPV[pv.Name]; !ok {
			seenPV[pv.Name] = struct{}{}
			hostPVs = append(hostPVs, pv.Name)
		}
		if _, ok := seenPVC[claimName]; !ok {
			seenPVC[claimName] = struct{}{}
			hostPVCs = append(hostPVCs, claimName)
		}
	}

	sort.Strings(hostPVs)
	sort.Strings(hostPVCs)
	return hostPVs, hostPVCs, nil
}

func (s *StorageService) findVirtualPVCsForHostPVs(ctx context.Context, hostPVs []string) ([]string, error) {
	virtualPVCs := make([]string, 0)
	seen := make(map[string]struct{})

	for _, hostPV := range hostPVs {
		pvs, err := s.clientset.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("source-pv=%s", hostPV),
		})
		if err != nil {
			return nil, fmt.Errorf("list virtual pvs for host pv %q: %w", hostPV, err)
		}
		for _, pv := range pvs.Items {
			if pv.Spec.ClaimRef == nil {
				continue
			}
			claimName := strings.TrimSpace(pv.Spec.ClaimRef.Name)
			namespace := strings.TrimSpace(pv.Spec.ClaimRef.Namespace)
			if claimName == "" {
				continue
			}
			key := namespace + "/" + claimName
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			virtualPVCs = append(virtualPVCs, claimName)
		}
	}

	sort.Strings(virtualPVCs)
	return virtualPVCs, nil
}

func (s *StorageService) resolvePVCFrontendInfo(ctx context.Context, pvc *corev1.PersistentVolumeClaim) (string, string) {
	if pvc == nil {
		return "-", "-"
	}

	hostPVName := strings.TrimSpace(pvc.Spec.VolumeName)
	if hostPVName == "" {
		return "-", "-"
	}

	pv, err := s.clientset.CoreV1().PersistentVolumes().Get(ctx, hostPVName, metav1.GetOptions{})
	if err != nil {
		return "-", "-"
	}

	if sourcePV := strings.TrimSpace(pv.Labels["source-pv"]); sourcePV != "" {
		hostPVName = sourcePV
		pv, err = s.clientset.CoreV1().PersistentVolumes().Get(ctx, hostPVName, metav1.GetOptions{})
		if err != nil {
			return "-", "-"
		}
	}

	if pv.Spec.ClaimRef == nil {
		return "-", "-"
	}

	resourceUID := extractResourceUIDFromName(strings.TrimSpace(pv.Spec.ClaimRef.Name))
	if resourceUID == "" || s.vcClient == nil {
		return "-", "-"
	}

	resource, err := s.vcClient.FindStorageVolumeResource(ctx, resourceUID)
	if err != nil || resource == nil {
		return "-", "-"
	}

	vclusterName := s.resolvePVCVirtualClusterName(ctx, pvc)
	return firstNonEmpty(resource.Name, resource.DisplayName, resource.ID), firstNonEmpty(vclusterName, "-")
}

func (s *StorageService) resolvePVCVirtualClusterName(ctx context.Context, pvc *corev1.PersistentVolumeClaim) string {
	if pvc == nil || s.vcClient == nil {
		return ""
	}

	if vcUID := strings.TrimSpace(pvc.Labels["vcluster.loft.sh/vcluster-name"]); vcUID != "" {
		return s.resolveVirtualClusterDisplayName(ctx, vcUID)
	}

	namespace := strings.TrimSpace(pvc.Namespace)
	if namespace == "" {
		return ""
	}

	pods, err := s.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return ""
	}
	for _, pod := range pods.Items {
		if !podUsesPVC(&pod, pvc.Name) {
			continue
		}
		vcName := firstNonEmpty(
			pod.Annotations["vcluster.loft.sh/vcluster-name"],
			pod.Labels["vcluster.loft.sh/vcluster-name"],
		)
		if strings.TrimSpace(vcName) == "" {
			continue
		}
		return s.resolveVirtualClusterDisplayName(ctx, vcName)
	}

	return s.resolveVirtualClusterNameFromNamespace(ctx, namespace)
}

func (s *StorageService) resolveVirtualClusterDisplayName(ctx context.Context, value string) string {
	value = strings.TrimSpace(value)
	if value == "" || s.vcClient == nil {
		return ""
	}

	uid := value
	if strings.HasPrefix(uid, "vc-") {
		uid = strings.TrimPrefix(uid, "vc-")
	}

	names, err := s.vcClient.ResolveDisplayNames(ctx, []string{uid})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(names[uid])
}

func (s *StorageService) resolveVirtualClusterNameFromNamespace(ctx context.Context, namespace string) string {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" || s.vcClient == nil {
		return ""
	}

	ns, err := s.clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		return ""
	}

	vclusterNamespace := firstNonEmpty(
		strings.TrimSpace(ns.Labels[nsVClusterNamespaceLabelKey]),
		namespace,
	)
	if strings.TrimSpace(vclusterNamespace) == "" {
		return ""
	}

	nodes, err := s.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", nodeVClusterNamespaceLabelKey, vclusterNamespace),
	})
	if err != nil || len(nodes.Items) == 0 {
		return ""
	}

	for _, node := range nodes.Items {
		vcUID := strings.TrimSpace(node.Labels["resource.compute.sensecore.cn/vc-uid"])
		if vcUID == "" {
			continue
		}
		if name := s.resolveVirtualClusterDisplayName(ctx, vcUID); strings.TrimSpace(name) != "" {
			return name
		}
	}

	return ""
}

func (s *StorageService) findJobsUsingPVC(ctx context.Context, pvc *corev1.PersistentVolumeClaim) []string {
	if pvc == nil {
		return nil
	}

	pods, err := s.clientset.CoreV1().Pods(pvc.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}

	jobSet := make(map[string]struct{})
	for _, pod := range pods.Items {
		if !podUsesPVC(&pod, pvc.Name) {
			continue
		}
		jobName := strings.TrimSpace(pod.Labels["volcano.sh/job-name"])
		if jobName == "" {
			jobName = strings.TrimSpace(pod.Annotations["volcano.sh/job-name"])
		}
		if jobName == "" {
			jobName = deriveJobName(pod)
		}
		if jobName == "" {
			continue
		}
		jobSet[jobName] = struct{}{}
	}

	jobs := make([]string, 0, len(jobSet))
	for jobName := range jobSet {
		jobs = append(jobs, jobName)
	}
	sort.Strings(jobs)
	return jobs
}

func podUsesPVC(pod *corev1.Pod, pvcName string) bool {
	if pod == nil || strings.TrimSpace(pvcName) == "" {
		return false
	}
	for _, volume := range pod.Spec.Volumes {
		if volume.PersistentVolumeClaim == nil {
			continue
		}
		if strings.TrimSpace(volume.PersistentVolumeClaim.ClaimName) == pvcName {
			return true
		}
	}
	return false
}
