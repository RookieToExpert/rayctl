package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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
	Tenant      string
	HostPVs     []string
	HostPVCs    []string
	VirtualPVCs []string
}

type PVCCheckItemResult struct {
	PVCName   string
	AFSName   string
	Partition string
	Tenant    string
	JobNames  []string
}

type PVCCheckResult struct {
	Items []PVCCheckItemResult
}

type PVCheckResult struct {
	HostPVName  string
	HostPVCName string
	StorageType string
	AFSName     string
	Tenant      string
}

type PVCCreateRequest struct {
	Name       string
	Namespace  string
	AFSUUID    string
	SecretName string
	Size       string
}

func NewStorageService(clientset kubernetes.Interface, vcClient *platform.VirtualClusterClient) *StorageService {
	return &StorageService{
		clientset: clientset,
		vcClient:  vcClient,
	}
}

func (s *StorageService) CreatePVC(ctx context.Context, req PVCCreateRequest) (*corev1.PersistentVolumeClaim, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("pvc name is required")
	}

	namespace := strings.TrimSpace(req.Namespace)
	if namespace == "" {
		namespace = "default"
	}

	afsUUID := strings.TrimSpace(req.AFSUUID)
	if afsUUID == "" {
		return nil, fmt.Errorf("afs uuid is required")
	}
	if !strings.HasPrefix(afsUUID, "csi://") {
		afsUUID = "csi://" + afsUUID
	}

	secretName := strings.TrimSpace(req.SecretName)
	if secretName == "" {
		return nil, fmt.Errorf("afs secret name is required")
	}

	size := strings.TrimSpace(req.Size)
	if size == "" {
		size = "1000Mi"
	}
	quantity, err := resource.ParseQuantity(size)
	if err != nil {
		return nil, fmt.Errorf("invalid pvc size %q: %w", size, err)
	}

	storageClassName := "quark-vcproxy-sc"
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				"afs.endpoint":   afsUUID,
				"afs.secretName": secretName,
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteMany,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: quantity,
				},
			},
			StorageClassName: &storageClassName,
		},
	}

	created, err := s.clientset.CoreV1().PersistentVolumeClaims(namespace).Create(ctx, pvc, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("create pvc %s/%s: %w", namespace, name, err)
	}
	return created, nil
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
		Tenant:      strings.TrimSpace(resource.ProfileName),
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
		afsName, partitionName, tenantName := s.resolvePVCFrontendInfo(ctx, &pvcs.Items[i])
		key := fmt.Sprintf("%s|%s|%s|%s", pvcs.Items[i].Name, afsName, partitionName, tenantName)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		items = append(items, PVCCheckItemResult{
			PVCName:   pvcs.Items[i].Name,
			AFSName:   afsName,
			Partition: partitionName,
			Tenant:    tenantName,
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

func (s *StorageService) CheckPV(ctx context.Context, identifier string) (*PVCheckResult, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, fmt.Errorf("pv name or uid is required")
	}

	hostPVName := identifier
	if !strings.HasPrefix(strings.ToLower(hostPVName), "pvc-") {
		hostPVName = "pvc-" + hostPVName
	}

	pv, err := s.clientset.CoreV1().PersistentVolumes().Get(ctx, hostPVName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get host pv %q: %w", hostPVName, err)
	}

	hostPVCName := "-"
	hostPVCNamespace := ""
	resourceUID := ""
	if pv.Spec.ClaimRef != nil {
		hostPVCName = firstNonEmpty(strings.TrimSpace(pv.Spec.ClaimRef.Name), "-")
		hostPVCNamespace = strings.TrimSpace(pv.Spec.ClaimRef.Namespace)
		resourceUID = extractResourceUIDFromName(strings.TrimSpace(pv.Spec.ClaimRef.Name))
	}

	storageType := "AFS"
	afsName := "-"
	tenant := "-"
	var hostPVC *corev1.PersistentVolumeClaim
	if hostPVCNamespace != "" && hostPVCName != "-" {
		if pvc, pvcErr := s.clientset.CoreV1().PersistentVolumeClaims(hostPVCNamespace).Get(ctx, hostPVCName, metav1.GetOptions{}); pvcErr == nil {
			hostPVC = pvc
		}
	}

	if isObjectStoragePV(pv, hostPVC) {
		storageType = "AOSS"
		afsName = s.resolveObjectStorageLocationForPV(ctx, pv, hostPVC)
	} else if resourceUID != "" && s.vcClient != nil {
		resource, resourceErr := s.vcClient.FindStorageVolumeResourceByUID(ctx, resourceUID)
		if resourceErr == nil && resource != nil {
			afsName = firstNonEmpty(resource.Name, resource.DisplayName, resource.ID, "-")
			tenant = firstNonEmpty(strings.TrimSpace(resource.ProfileName), "-")
		}
	}

	return &PVCheckResult{
		HostPVName:  pv.Name,
		HostPVCName: hostPVCName,
		StorageType: storageType,
		AFSName:     afsName,
		Tenant:      tenant,
	}, nil
}

func isObjectStoragePV(pv *corev1.PersistentVolume, pvc *corev1.PersistentVolumeClaim) bool {
	if pvc != nil {
		storageClassName := ""
		if pvc.Spec.StorageClassName != nil {
			storageClassName = strings.TrimSpace(*pvc.Spec.StorageClassName)
		}
		storageClass := firstNonEmpty(
			storageClassName,
			pvc.Annotations["volume.kubernetes.io/storage-provisioner"],
			pvc.Annotations["volume.beta.kubernetes.io/storage-provisioner"],
		)
		if looksLikeObjectStoragePVC(storageClass, pvc.Annotations) {
			return true
		}
	}

	if pv == nil {
		return false
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(pv.Spec.StorageClassName)), "aoss") ||
		strings.Contains(strings.ToLower(strings.TrimSpace(pv.Spec.StorageClassName)), "s3") {
		return true
	}
	if pv.Spec.CSI == nil {
		return false
	}
	driver := strings.ToLower(strings.TrimSpace(pv.Spec.CSI.Driver))
	if strings.Contains(driver, "aoss") || strings.Contains(driver, "s3") {
		return true
	}
	for key, value := range pv.Spec.CSI.VolumeAttributes {
		text := strings.ToLower(strings.TrimSpace(key + "=" + value))
		if strings.Contains(text, "aoss") || strings.Contains(text, "s3") || strings.Contains(text, "bucket") {
			return true
		}
	}
	return false
}

func (s *StorageService) resolveObjectStorageLocationForPV(ctx context.Context, pv *corev1.PersistentVolume, pvc *corev1.PersistentVolumeClaim) string {
	endpoint := ""
	bucket := ""
	if pvc != nil {
		endpoint = s.resolveObjectStorageEndpointForPVC(ctx, pvc)
		bucket = firstNonEmpty(
			pvc.Annotations["bucket"],
			pvc.Annotations["bucketName"],
			pvc.Annotations["bucket-name"],
			pvc.Annotations["aoss.bucket"],
		)
	}

	if pv != nil && pv.Spec.CSI != nil {
		attributes := pv.Spec.CSI.VolumeAttributes
		if endpoint == "" {
			endpoint = firstNonEmptyMapValue(attributes, "endpoint", "domain", "host", "url")
			endpoint = normalizeEndpointString(endpoint)
		}
		if bucket == "" {
			bucket = firstNonEmptyMapValue(attributes, "bucket", "bucketName", "bucket-name")
		}
		if bucket == "" {
			parts := strings.Split(strings.Trim(strings.TrimSpace(pv.Spec.CSI.VolumeHandle), "/"), "/")
			if len(parts) > 1 {
				bucket = strings.TrimSpace(parts[len(parts)-1])
			}
		}
	}

	return formatObjectStorageLocation(endpoint, bucket)
}

func firstNonEmptyMapValue(values map[string]string, keys ...string) string {
	for _, wantedKey := range keys {
		for key, value := range values {
			if strings.EqualFold(strings.TrimSpace(key), wantedKey) && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func formatObjectStorageLocation(endpoint string, bucket string) string {
	endpoint = strings.TrimSuffix(strings.TrimSpace(endpoint), "/")
	bucket = strings.Trim(strings.TrimSpace(bucket), "/")
	switch {
	case endpoint != "" && bucket != "":
		if strings.HasSuffix(endpoint, "/"+bucket) || endpoint == bucket {
			return endpoint
		}
		return endpoint + "/" + bucket
	case endpoint != "":
		return endpoint
	case bucket != "":
		return "bucket=" + bucket
	default:
		return "-"
	}
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

func (s *StorageService) resolvePVCFrontendInfo(ctx context.Context, pvc *corev1.PersistentVolumeClaim) (string, string, string) {
	if pvc == nil {
		return "-", "-", "-"
	}

	partitionName, partitionTenant := s.resolvePVCVirtualClusterName(ctx, pvc)
	partitionName = firstNonEmpty(partitionName, "-")
	hostPVName := strings.TrimSpace(pvc.Spec.VolumeName)
	if hostPVName == "" {
		if endpoint := s.resolveObjectStorageEndpointForPVC(ctx, pvc); endpoint != "" {
			return endpoint, partitionName, firstNonEmpty(partitionTenant, "-")
		}
		return "-", partitionName, firstNonEmpty(partitionTenant, "-")
	}

	pv, err := s.clientset.CoreV1().PersistentVolumes().Get(ctx, hostPVName, metav1.GetOptions{})
	if err != nil {
		if endpoint := s.resolveObjectStorageEndpointForPVC(ctx, pvc); endpoint != "" {
			return endpoint, partitionName, firstNonEmpty(partitionTenant, "-")
		}
		return "-", partitionName, firstNonEmpty(partitionTenant, "-")
	}

	if sourcePV := strings.TrimSpace(pv.Labels["source-pv"]); sourcePV != "" {
		hostPVName = sourcePV
		pv, err = s.clientset.CoreV1().PersistentVolumes().Get(ctx, hostPVName, metav1.GetOptions{})
		if err != nil {
			if endpoint := s.resolveObjectStorageEndpointForPVC(ctx, pvc); endpoint != "" {
				return endpoint, partitionName, firstNonEmpty(partitionTenant, "-")
			}
			return "-", partitionName, firstNonEmpty(partitionTenant, "-")
		}
	}

	if pv.Spec.ClaimRef == nil {
		if endpoint := s.resolveObjectStorageEndpointForPVC(ctx, pvc); endpoint != "" {
			return endpoint, partitionName, firstNonEmpty(partitionTenant, "-")
		}
		return "-", partitionName, firstNonEmpty(partitionTenant, "-")
	}

	resourceUID := extractResourceUIDFromName(strings.TrimSpace(pv.Spec.ClaimRef.Name))
	if resourceUID == "" || s.vcClient == nil {
		if endpoint := s.resolveObjectStorageEndpointForPVC(ctx, pvc); endpoint != "" {
			return endpoint, partitionName, firstNonEmpty(partitionTenant, "-")
		}
		return "-", partitionName, firstNonEmpty(partitionTenant, "-")
	}

	resource, err := s.vcClient.FindStorageVolumeResource(ctx, resourceUID)
	if err != nil || resource == nil {
		if endpoint := s.resolveObjectStorageEndpointForPVC(ctx, pvc); endpoint != "" {
			return endpoint, partitionName, firstNonEmpty(partitionTenant, "-")
		}
		return "-", partitionName, firstNonEmpty(partitionTenant, "-")
	}

	return firstNonEmpty(resource.Name, resource.DisplayName, resource.ID), partitionName, firstNonEmpty(strings.TrimSpace(resource.ProfileName), partitionTenant, "-")
}

func (s *StorageService) resolveObjectStorageEndpointForPVC(ctx context.Context, pvc *corev1.PersistentVolumeClaim) string {
	if pvc == nil {
		return ""
	}

	storageClassName := ""
	if pvc.Spec.StorageClassName != nil {
		storageClassName = strings.TrimSpace(*pvc.Spec.StorageClassName)
	}
	storageClass := strings.TrimSpace(firstNonEmpty(storageClassName, pvc.Annotations["volume.kubernetes.io/storage-provisioner"], pvc.Annotations["volume.beta.kubernetes.io/storage-provisioner"]))
	if !looksLikeObjectStoragePVC(storageClass, pvc.Annotations) {
		return ""
	}

	secretName := strings.TrimSpace(pvc.Annotations["secretName"])
	if secretName == "" {
		return ""
	}

	secret, err := s.clientset.CoreV1().Secrets(pvc.Namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil || secret == nil {
		return ""
	}
	return decodeObjectStorageEndpoint(secret)
}

func (s *StorageService) resolvePVCVirtualClusterName(ctx context.Context, pvc *corev1.PersistentVolumeClaim) (string, string) {
	if pvc == nil || s.vcClient == nil {
		return "", ""
	}

	namespace := strings.TrimSpace(pvc.Namespace)
	if namespace == "" {
		return "", ""
	}

	if name, tenant := s.resolveVirtualClusterNameFromNamespace(ctx, namespace); strings.TrimSpace(name) != "" {
		return name, tenant
	}

	pods, err := s.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", ""
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

	return "", ""
}

func (s *StorageService) resolveVirtualClusterDisplayName(ctx context.Context, value string) (string, string) {
	value = strings.TrimSpace(value)
	if value == "" || s.vcClient == nil {
		return "", ""
	}

	uid := value
	if strings.HasPrefix(uid, "vc-") {
		uid = strings.TrimPrefix(uid, "vc-")
	}

	names, profiles, err := s.vcClient.ResolveDisplayNamesWithProfiles(ctx, []string{uid})
	if err != nil {
		return "", ""
	}
	return strings.TrimSpace(names[uid]), strings.TrimSpace(profiles[uid])
}

func (s *StorageService) resolveVirtualClusterNameFromNamespace(ctx context.Context, namespace string) (string, string) {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" || s.vcClient == nil {
		return "", ""
	}

	ns, err := s.clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		return "", ""
	}

	if vcName := strings.TrimSpace(ns.Labels["vcluster.loft.sh/vcluster-name"]); vcName != "" {
		if displayName, tenant := s.resolveVirtualClusterDisplayName(ctx, vcName); strings.TrimSpace(displayName) != "" {
			return displayName, tenant
		}
		if strings.TrimSpace(vcName) != "" {
			return vcName, ""
		}
	}

	vclusterNamespace := firstNonEmpty(
		strings.TrimSpace(ns.Labels[nsVClusterNamespaceLabelKey]),
		namespace,
	)
	if strings.TrimSpace(vclusterNamespace) == "" {
		return "", ""
	}

	nodes, err := s.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", nodeVClusterNamespaceLabelKey, vclusterNamespace),
	})
	if err != nil || len(nodes.Items) == 0 {
		return "", ""
	}

	for _, node := range nodes.Items {
		vcUID := strings.TrimSpace(node.Labels["resource.compute.sensecore.cn/vc-uid"])
		if vcUID == "" {
			continue
		}
		if name, tenant := s.resolveVirtualClusterDisplayName(ctx, vcUID); strings.TrimSpace(name) != "" {
			return name, tenant
		}
	}

	return "", ""
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
