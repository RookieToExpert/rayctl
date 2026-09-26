package service

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sort"
	"strings"
)

func extractJobSpecDetails(job *unstructured.Unstructured) ([]string, []VolumeClaimRef) {
	tasks, found, err := unstructured.NestedSlice(job.Object, "spec", "tasks")
	if err != nil || !found {
		return nil, nil
	}

	secretSet := make(map[string]struct{})
	claimSet := make(map[string]VolumeClaimRef)

	for _, task := range tasks {
		taskMap, ok := task.(map[string]any)
		if !ok {
			continue
		}
		spec, found, err := unstructured.NestedMap(taskMap, "template", "spec")
		if err != nil || !found {
			continue
		}
		collectPodSpecDetails(spec, secretSet, claimSet)
	}

	return normalizeSpecDetails(secretSet, claimSet)
}

func extractPodSpecDetailsFromPods(pods []corev1.Pod) ([]string, []VolumeClaimRef) {
	secretSet := make(map[string]struct{})
	claimSet := make(map[string]VolumeClaimRef)

	for _, pod := range pods {
		for _, secret := range pod.Spec.ImagePullSecrets {
			if strings.TrimSpace(secret.Name) != "" {
				secretSet[secret.Name] = struct{}{}
			}
		}
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim == nil {
				continue
			}
			claimSet[volume.Name] = VolumeClaimRef{
				Name:      volume.Name,
				ClaimName: volume.PersistentVolumeClaim.ClaimName,
			}
		}
	}

	return normalizeSpecDetails(secretSet, claimSet)
}

func collectPodSpecDetails(spec map[string]any, secretSet map[string]struct{}, claimSet map[string]VolumeClaimRef) {
	if imagePullSecrets, found, err := unstructured.NestedSlice(spec, "imagePullSecrets"); err == nil && found {
		for _, secret := range imagePullSecrets {
			secretMap, ok := secret.(map[string]any)
			if !ok {
				continue
			}
			name := strings.TrimSpace(fmt.Sprintf("%v", secretMap["name"]))
			if name != "" {
				secretSet[name] = struct{}{}
			}
		}
	}

	if volumes, found, err := unstructured.NestedSlice(spec, "volumes"); err == nil && found {
		for _, volume := range volumes {
			volumeMap, ok := volume.(map[string]any)
			if !ok {
				continue
			}
			pvc, ok := volumeMap["persistentVolumeClaim"].(map[string]any)
			if !ok {
				continue
			}
			name := strings.TrimSpace(fmt.Sprintf("%v", volumeMap["name"]))
			claimName := strings.TrimSpace(fmt.Sprintf("%v", pvc["claimName"]))
			if name == "" || claimName == "" {
				continue
			}
			claimSet[name] = VolumeClaimRef{Name: name, ClaimName: claimName}
		}
	}
}

func normalizeSpecDetails(secretSet map[string]struct{}, claimSet map[string]VolumeClaimRef) ([]string, []VolumeClaimRef) {
	secrets := make([]string, 0, len(secretSet))
	for secret := range secretSet {
		secrets = append(secrets, secret)
	}
	sort.Strings(secrets)

	claims := make([]VolumeClaimRef, 0, len(claimSet))
	for _, claim := range claimSet {
		claims = append(claims, claim)
	}
	sort.Slice(claims, func(i, j int) bool {
		if claims[i].Name == claims[j].Name {
			return claims[i].ClaimName < claims[j].ClaimName
		}
		return claims[i].Name < claims[j].Name
	})

	return secrets, claims
}

func (s *JobService) getPlatformSecret(ctx context.Context, identity *jobIdentity, namespace string, secretName string) (*corev1.Secret, error) {
	if s.vcClient == nil || identity == nil {
		return nil, fmt.Errorf("platform job identity is required")
	}
	if strings.TrimSpace(identity.ProfileName) != "" {
		return s.vcClient.GetSecretForProfile(ctx, identity.ProfileName, identity.VClusterName, namespace, secretName)
	}
	return s.vcClient.GetSecret(ctx, identity.VClusterName, namespace, secretName)
}

func (s *JobService) getPlatformPersistentVolumeClaim(ctx context.Context, identity *jobIdentity, namespace string, claimName string) (*corev1.PersistentVolumeClaim, error) {
	if s.vcClient == nil || identity == nil {
		return nil, fmt.Errorf("platform job identity is required")
	}
	if strings.TrimSpace(identity.ProfileName) != "" {
		return s.vcClient.GetPersistentVolumeClaimForProfile(ctx, identity.ProfileName, identity.VClusterName, namespace, claimName)
	}
	return s.vcClient.GetPersistentVolumeClaim(ctx, identity.VClusterName, namespace, claimName)
}

func (s *JobService) getPlatformPersistentVolume(ctx context.Context, identity *jobIdentity, pvName string) (*corev1.PersistentVolume, error) {
	if s.vcClient == nil || identity == nil {
		return nil, fmt.Errorf("platform job identity is required")
	}
	if strings.TrimSpace(identity.ProfileName) != "" {
		return s.vcClient.GetPersistentVolumeForProfile(ctx, identity.ProfileName, identity.VClusterName, pvName)
	}
	return s.vcClient.GetPersistentVolume(ctx, identity.VClusterName, pvName)
}

func (s *JobService) resolveVolumeClaimRefs(ctx context.Context, identity *jobIdentity, refs []VolumeClaimRef) []VolumeClaimRef {
	if len(refs) == 0 {
		return refs
	}
	hostNamespace := ""
	vclusterName := ""
	namespace := ""
	if identity != nil {
		hostNamespace = strings.TrimSpace(identity.HostNamespace)
		vclusterName = strings.TrimSpace(identity.VClusterName)
		namespace = strings.TrimSpace(identity.Namespace)
	}
	return boundedMap(ctx, refs, 4, func(ctx context.Context, ref VolumeClaimRef) VolumeClaimRef {
		return s.resolveVolumeClaimRef(ctx, identity, ref, hostNamespace, vclusterName, namespace)
	})
}

func (s *JobService) resolveVolumeClaimRef(ctx context.Context, identity *jobIdentity, ref VolumeClaimRef, hostNamespace string, vclusterName string, namespace string) VolumeClaimRef {
	current := ref
	if strings.TrimSpace(current.PVName) != "" {
		current.Status = "Bound"
	}
	switch {
	case s.vcClient != nil && vclusterName != "" && namespace != "":
		pvc, err := s.getPlatformPersistentVolumeClaim(ctx, identity, namespace, ref.ClaimName)
		if err == nil {
			current.Status = dashIfEmpty(string(pvc.Status.Phase))
			current.Message = firstNonEmpty(firstPVCConditionMessage(pvc), strings.TrimSpace(pvc.Spec.VolumeName))
			current.PVName = strings.TrimSpace(pvc.Spec.VolumeName)
			if current.PVName != "" {
				if pv, pvErr := s.clientset.CoreV1().PersistentVolumes().Get(ctx, current.PVName, metav1.GetOptions{}); pvErr == nil {
					current.BackendPV = strings.TrimSpace(pv.Labels["source-pv"])
				} else if pv, pvErr := s.getPlatformPersistentVolume(ctx, identity, current.PVName); pvErr == nil {
					current.BackendPV = strings.TrimSpace(pv.Labels["source-pv"])
				}
				if current.BackendPV == "" {
					current.BackendPV = strings.TrimSpace(current.PVName)
				}
			}
			s.enrichObjectStorageVolumeClaimRefFromVirtualCluster(ctx, identity, namespace, pvc, &current)
		} else {
			current.Status = "Unknown"
			current.Message = classifyPVCErrorMessage(err)
			if current.Message == "PVC 在当前集群不存在" {
				current.Status = "NotFound"
			}
		}
	case hostNamespace != "":
		pvc, err := s.clientset.CoreV1().PersistentVolumeClaims(hostNamespace).Get(ctx, ref.ClaimName, metav1.GetOptions{})
		if err == nil {
			current.Status = dashIfEmpty(string(pvc.Status.Phase))
			current.Message = firstNonEmpty(firstPVCConditionMessage(pvc), strings.TrimSpace(pvc.Spec.VolumeName))
			current.BackendPV = strings.TrimSpace(pvc.Spec.VolumeName)
		} else {
			current.Status = "Unknown"
			current.Message = classifyPVCErrorMessage(err)
			if current.Message == "PVC 在当前集群不存在" {
				current.Status = "NotFound"
			}
		}
	}
	if strings.TrimSpace(current.FrontendVolume) == "" {
		s.enrichObjectStorageVolumeClaimRef(ctx, hostNamespace, &current)
	}
	s.enrichHostVolumeClaimRef(ctx, &current)
	current.DisplayPV = firstNonEmpty(current.BackendPV, current.PVName)
	return current
}

func ensurePVCGetDiagnosis(status string, terminal bool, stage string, diagnosis []string, refs []VolumeClaimRef) (string, []string) {
	if terminal || strings.EqualFold(strings.TrimSpace(status), "Running") {
		return stage, diagnosis
	}

	message := ""
	for _, ref := range refs {
		if strings.EqualFold(strings.TrimSpace(ref.Status), "NotFound") || ref.Message == "PVC 在当前集群不存在" {
			message = "存在 PVC 在当前集群不存在，任务因此无法继续调度。"
			break
		}
		if strings.EqualFold(strings.TrimSpace(ref.Status), string(corev1.ClaimPending)) {
			message = "PVC 仍处于 Pending，当前大概率是 AFS 的 AK/SK 错误。"
		}
	}
	if message == "" {
		return stage, diagnosis
	}
	for _, existing := range diagnosis {
		if strings.Contains(existing, "PVC") {
			return firstNonEmpty(stage, "scheduling"), diagnosis
		}
	}
	return firstNonEmpty(stage, "scheduling"), append([]string{message}, diagnosis...)
}

func (s *JobService) enrichHostVolumeClaimRef(ctx context.Context, ref *VolumeClaimRef) {
	if ref == nil {
		return
	}

	hostPVName := strings.TrimSpace(ref.BackendPV)
	if hostPVName == "" {
		return
	}

	pv, err := s.clientset.CoreV1().PersistentVolumes().Get(ctx, hostPVName, metav1.GetOptions{})
	if err != nil {
		return
	}

	if pv.Spec.ClaimRef != nil {
		ref.HostPVCName = strings.TrimSpace(pv.Spec.ClaimRef.Name)
		ref.HostPVCNamespace = strings.TrimSpace(pv.Spec.ClaimRef.Namespace)
	}

	if s.vcClient == nil || strings.TrimSpace(ref.HostPVCName) == "" {
		return
	}

	resourceUID := extractResourceUIDFromName(ref.HostPVCName)
	if resourceUID == "" {
		return
	}

	resource, err := s.vcClient.FindStorageVolumeResourceByUID(ctx, resourceUID)
	if err != nil || resource == nil {
		return
	}
	ref.FrontendVolume = firstNonEmpty(resource.Name, resource.DisplayName, resource.ID)
}

func (s *JobService) enrichObjectStorageVolumeClaimRef(ctx context.Context, hostNamespace string, ref *VolumeClaimRef) {
	if ref == nil || strings.TrimSpace(ref.FrontendVolume) != "" {
		return
	}
	hostNamespace = strings.TrimSpace(hostNamespace)
	if hostNamespace == "" || strings.TrimSpace(ref.ClaimName) == "" {
		return
	}

	pvc, err := s.clientset.CoreV1().PersistentVolumeClaims(hostNamespace).Get(ctx, ref.ClaimName, metav1.GetOptions{})
	if err != nil || pvc == nil {
		return
	}

	storageClassName := ""
	if pvc.Spec.StorageClassName != nil {
		storageClassName = strings.TrimSpace(*pvc.Spec.StorageClassName)
	}
	storageClass := strings.TrimSpace(firstNonEmpty(storageClassName, pvc.Annotations["volume.kubernetes.io/storage-provisioner"], pvc.Annotations["volume.beta.kubernetes.io/storage-provisioner"]))
	if !looksLikeObjectStoragePVC(storageClass, pvc.Annotations) {
		return
	}

	secretName := strings.TrimSpace(pvc.Annotations["secretName"])
	endpoint := ""
	if secretName != "" {
		secret, secretErr := s.clientset.CoreV1().Secrets(hostNamespace).Get(ctx, secretName, metav1.GetOptions{})
		if secretErr == nil && secret != nil {
			endpoint = decodeObjectStorageEndpoint(secret)
		}
	}

	if location := formatObjectStorageLocation(endpoint, objectStorageBucketFromPVC(pvc)); location != "-" {
		ref.FrontendVolume = location
	}
}

func (s *JobService) enrichObjectStorageVolumeClaimRefFromVirtualCluster(ctx context.Context, identity *jobIdentity, namespace string, pvc *corev1.PersistentVolumeClaim, ref *VolumeClaimRef) {
	if ref == nil || strings.TrimSpace(ref.FrontendVolume) != "" || s.vcClient == nil || identity == nil || pvc == nil {
		return
	}

	namespace = strings.TrimSpace(namespace)
	if strings.TrimSpace(identity.VClusterName) == "" || namespace == "" {
		return
	}

	storageClassName := ""
	if pvc.Spec.StorageClassName != nil {
		storageClassName = strings.TrimSpace(*pvc.Spec.StorageClassName)
	}
	storageClass := strings.TrimSpace(firstNonEmpty(storageClassName, pvc.Annotations["volume.kubernetes.io/storage-provisioner"], pvc.Annotations["volume.beta.kubernetes.io/storage-provisioner"]))
	if !looksLikeObjectStoragePVC(storageClass, pvc.Annotations) {
		return
	}

	secretName := strings.TrimSpace(firstNonEmpty(pvc.Annotations["secretName"], pvc.Annotations["afs.secretName"]))
	endpoint := ""
	if secretName != "" {
		secret, secretErr := s.getPlatformSecret(ctx, identity, namespace, secretName)
		if secretErr == nil && secret != nil {
			endpoint = decodeObjectStorageEndpoint(secret)
		}
	}

	if location := formatObjectStorageLocation(endpoint, objectStorageBucketFromPVC(pvc)); location != "-" {
		ref.FrontendVolume = location
	}
}

func objectStorageBucketFromPVC(pvc *corev1.PersistentVolumeClaim) string {
	if pvc == nil {
		return ""
	}
	return firstNonEmpty(
		pvc.Annotations["bucket"],
		pvc.Annotations["bucketName"],
		pvc.Annotations["bucket-name"],
		pvc.Annotations["aoss.bucket"],
	)
}

func looksLikeObjectStoragePVC(storageClass string, annotations map[string]string) bool {
	storageClass = strings.ToLower(strings.TrimSpace(storageClass))
	if strings.Contains(storageClass, "aoss") || strings.Contains(storageClass, "s3") {
		return true
	}
	for key, value := range annotations {
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.ToLower(strings.TrimSpace(value))
		if strings.Contains(key, "bucket") || strings.Contains(key, "secretname") || strings.Contains(value, "aoss") || strings.Contains(value, "s3") {
			return true
		}
	}
	return false
}

func decodeObjectStorageEndpoint(secret *corev1.Secret) string {
	if secret == nil {
		return ""
	}

	preferredKeys := []string{"endpoint", "ENDPOINT", "Endpoint", "domain", "DOMAIN", "host", "HOST", "url", "URL"}
	for _, key := range preferredKeys {
		if value, ok := secret.Data[key]; ok {
			endpoint := normalizeEndpointString(string(value))
			if endpoint != "" {
				return endpoint
			}
		}
	}

	for key, value := range secret.Data {
		lowerKey := strings.ToLower(strings.TrimSpace(key))
		if strings.Contains(lowerKey, "endpoint") || strings.Contains(lowerKey, "domain") || strings.Contains(lowerKey, "host") || strings.Contains(lowerKey, "url") {
			endpoint := normalizeEndpointString(string(value))
			if endpoint != "" {
				return endpoint
			}
		}
	}

	for _, value := range secret.Data {
		endpoint := normalizeEndpointString(string(value))
		if endpoint != "" {
			return endpoint
		}
	}

	return ""
}

func normalizeEndpointString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.TrimSuffix(value, "/")
	if strings.Contains(value, " ") {
		return ""
	}
	if strings.Contains(value, ".") || strings.Contains(value, ":") {
		return value
	}
	return ""
}

func duplicatePVRefs(refs []VolumeClaimRef) []VolumeClaimRef {
	byPV := make(map[string][]VolumeClaimRef)
	for _, ref := range refs {
		pvName := strings.TrimSpace(firstNonEmpty(ref.BackendPV, ref.PVName))
		if pvName == "" {
			continue
		}
		byPV[pvName] = append(byPV[pvName], ref)
	}

	duplicates := make([]VolumeClaimRef, 0)
	for _, group := range byPV {
		if len(group) < 2 {
			continue
		}
		duplicates = append(duplicates, group...)
	}

	sort.Slice(duplicates, func(i, j int) bool {
		leftPV := firstNonEmpty(duplicates[i].BackendPV, duplicates[i].PVName)
		rightPV := firstNonEmpty(duplicates[j].BackendPV, duplicates[j].PVName)
		if leftPV == rightPV {
			if duplicates[i].ClaimName == duplicates[j].ClaimName {
				return duplicates[i].Name < duplicates[j].Name
			}
			return duplicates[i].ClaimName < duplicates[j].ClaimName
		}
		return leftPV < rightPV
	})
	return duplicates
}

func pvcRefsToChecks(refs []VolumeClaimRef) []PVCCheckItem {
	if len(refs) == 0 {
		return nil
	}
	items := make([]PVCCheckItem, 0, len(refs))
	for _, ref := range refs {
		items = append(items, PVCCheckItem{
			Name:             ref.Name,
			ClaimName:        ref.ClaimName,
			PVName:           ref.PVName,
			BackendPV:        ref.BackendPV,
			DisplayPV:        firstNonEmpty(ref.DisplayPV, ref.BackendPV, ref.PVName),
			HostPVCName:      ref.HostPVCName,
			HostPVCNamespace: ref.HostPVCNamespace,
			FrontendVolume:   ref.FrontendVolume,
			Status:           "Bound",
			Message:          "重复的 PV 挂载有冲突",
		})
	}
	return items
}

func firstPVCConditionMessage(pvc *corev1.PersistentVolumeClaim) string {
	if pvc == nil {
		return ""
	}
	for i := len(pvc.Status.Conditions) - 1; i >= 0; i-- {
		message := strings.TrimSpace(pvc.Status.Conditions[i].Message)
		reason := strings.TrimSpace(pvc.Status.Conditions[i].Reason)
		if combined := joinMessageReason(message, reason); combined != "" {
			return combined
		}
	}
	return ""
}

func classifyPVCErrorMessage(err error) string {
	if err == nil {
		return "-"
	}
	if apierrors.IsNotFound(err) {
		return "PVC 在当前集群不存在"
	}
	message := strings.TrimSpace(err.Error())
	if strings.Contains(message, "persistentvolumeclaims") && strings.Contains(message, "not found") {
		return "PVC 在当前集群不存在"
	}
	return message
}
