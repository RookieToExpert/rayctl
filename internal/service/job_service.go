package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"net"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

var (
	volcanoJobGVR = schema.GroupVersionResource{
		Group:    "batch.volcano.sh",
		Version:  "v1alpha1",
		Resource: "jobs",
	}
	volcanoPodGroupGVR = schema.GroupVersionResource{
		Group:    "scheduling.volcano.sh",
		Version:  "v1beta1",
		Resource: "podgroups",
	}
	utcPlus8 = time.FixedZone("UTC+8", 8*60*60)
)

const (
	defaultTailLogLines         = int64(10)
	defaultEventLimit           = 10
	defaultRegistryHost         = "registry2.d.pjlab.org.cn"
	jobUIDLookupPageSize        = int64(5000)
	platformJobScanTimeout      = 4 * time.Second
	platformJobProbeConcurrency = 16
	registryLookupTimeout       = 750 * time.Millisecond
	dockerLoginTimeout          = 3 * time.Second
)

func looksLikeUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, ch := range value {
		switch i {
		case 8, 13, 18, 23:
			if ch != '-' {
				return false
			}
		default:
			if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') && (ch < 'A' || ch > 'F') {
				return false
			}
		}
	}
	return true
}

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

func (s *JobService) resolveImagePullSecretsFromPlatform(ctx context.Context, identity *jobIdentity, namespace string, secretNames []string) []string {
	if s.vcClient == nil || identity == nil || strings.TrimSpace(identity.VClusterName) == "" || strings.TrimSpace(namespace) == "" {
		return secretNames
	}

	resolved := make([]string, 0, len(secretNames))
	for _, secretName := range secretNames {
		secret, err := s.getPlatformSecret(ctx, identity, namespace, secretName)
		if err != nil {
			resolved = append(resolved, secretName)
			continue
		}
		resolved = append(resolved, formatImagePullSecret(secretName, secret))
	}
	return resolved
}

func (s *JobService) resolveImagePullSecrets(ctx context.Context, identity *jobIdentity, secretNames []string) []string {
	hostNamespace := ""
	vclusterName := ""
	namespace := ""
	if identity != nil {
		hostNamespace = strings.TrimSpace(identity.HostNamespace)
		vclusterName = strings.TrimSpace(identity.VClusterName)
		namespace = strings.TrimSpace(identity.Namespace)
	}

	if hostNamespace != "" {
		return s.resolveImagePullSecretsFromKube(ctx, hostNamespace, secretNames)
	}
	if vclusterName != "" && namespace != "" {
		return s.resolveImagePullSecretsFromPlatform(ctx, identity, namespace, secretNames)
	}
	return secretNames
}

func (s *JobService) resolveImagePullSecretsFromKube(ctx context.Context, namespace string, secretNames []string) []string {
	if strings.TrimSpace(namespace) == "" {
		return secretNames
	}

	return boundedMap(ctx, secretNames, 4, func(ctx context.Context, secretName string) string {
		secret, err := s.clientset.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
		if err != nil {
			return secretName
		}
		return formatImagePullSecret(secretName, secret)
	})
}

func formatImagePullSecret(secretName string, secret *corev1.Secret) string {
	creds := decodeDockerSecret(secret)
	if len(creds) == 0 {
		return secretName
	}
	return fmt.Sprintf("%s | %s", secretName, strings.Join(creds, "; "))
}

func decodeDockerSecret(secret *corev1.Secret) []string {
	credentials := decodeDockerCredentials(secret)
	results := make([]string, 0, len(credentials))
	for _, credential := range credentials {
		registryDisplay := credential.Registry
		if credential.RegistryHasWhitespace {
			registryDisplay = fmt.Sprintf("%q [registry 两端有空格]", credential.RegistryRaw)
		}
		if credential.Registry != "" {
			results = append(results, fmt.Sprintf("%s username: %s password: %s", registryDisplay, credential.Username, credential.Password))
		} else {
			results = append(results, fmt.Sprintf("username: %s password: %s", credential.Username, credential.Password))
		}
	}
	return results
}

func decodeDockerCredentials(secret *corev1.Secret) []dockerCredential {
	if secret == nil {
		return nil
	}

	type dockerConfigEntry struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Auth     string `json:"auth"`
	}
	type dockerConfigJSON struct {
		Auths map[string]dockerConfigEntry `json:"auths"`
	}

	var raw []byte
	switch {
	case len(secret.Data[corev1.DockerConfigJsonKey]) > 0:
		raw = secret.Data[corev1.DockerConfigJsonKey]
	case len(secret.Data[corev1.DockerConfigKey]) > 0:
		raw = secret.Data[corev1.DockerConfigKey]
	default:
		return nil
	}

	var payload dockerConfigJSON
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}

	registries := make([]string, 0, len(payload.Auths))
	for registry := range payload.Auths {
		registries = append(registries, registry)
	}
	sort.Strings(registries)

	results := make([]dockerCredential, 0, len(registries))
	for _, registry := range registries {
		entry := payload.Auths[registry]
		registryTrimmed := strings.TrimSpace(registry)
		username := strings.TrimSpace(entry.Username)
		password := strings.TrimSpace(entry.Password)
		if (username == "" || password == "") && strings.TrimSpace(entry.Auth) != "" {
			decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
			if err == nil {
				parts := strings.SplitN(string(decoded), ":", 2)
				if len(parts) == 2 {
					if username == "" {
						username = parts[0]
					}
					if password == "" {
						password = parts[1]
					}
				}
			}
		}
		if username == "" && password == "" {
			continue
		}
		results = append(results, dockerCredential{
			Registry:              registryTrimmed,
			RegistryRaw:           registry,
			RegistryHasWhitespace: registryTrimmed != registry,
			Username:              username,
			Password:              password,
		})
	}

	return results
}

func joinMessageReason(message string, reason string) string {
	message = strings.TrimSpace(message)
	reason = strings.TrimSpace(reason)
	switch {
	case message == "" && reason == "":
		return ""
	case message == "":
		return reason
	case reason == "", strings.EqualFold(message, reason):
		return message
	default:
		return fmt.Sprintf("%s | %s", message, reason)
	}
}

func getNestedString(obj map[string]any, fields ...string) string {
	value, found, err := unstructured.NestedString(obj, fields...)
	if err != nil || !found {
		return ""
	}
	return strings.TrimSpace(value)
}

func getNestedText(obj map[string]any, fields ...string) string {
	if value := getNestedString(obj, fields...); value != "" {
		return value
	}
	raw, found, err := unstructured.NestedFieldNoCopy(obj, fields...)
	if err != nil || !found || raw == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", raw))
}

func eventTimestamp(event corev1.Event) time.Time {
	switch {
	case !event.EventTime.IsZero():
		return event.EventTime.Time
	case !event.LastTimestamp.IsZero():
		return event.LastTimestamp.Time
	case !event.FirstTimestamp.IsZero():
		return event.FirstTimestamp.Time
	default:
		return event.CreationTimestamp.Time
	}
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func dashIfEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func boolToYesNo(value bool) string {
	if value {
		return "Yes"
	}
	return "No"
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

func extractResourceUIDFromName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if looksLikeUUID(value) {
		return value
	}
	for start := 0; start+36 <= len(value); start++ {
		candidate := value[start : start+36]
		if looksLikeUUID(candidate) {
			return candidate
		}
	}
	return ""
}

func hasFailedSecretCheck(items []SecretCheckItem) bool {
	for _, item := range items {
		if strings.EqualFold(item.Status, "FAIL") {
			return true
		}
	}
	return false
}

func hasErroredSecretCheck(items []SecretCheckItem) bool {
	for _, item := range items {
		if strings.EqualFold(item.Status, "ERROR") {
			return true
		}
	}
	return false
}

func (s *JobService) checkImagePullSecrets(ctx context.Context, identity *jobIdentity, secretNames []string) []SecretCheckItem {
	if len(secretNames) == 0 {
		return nil
	}

	namespace := ""
	useHostSecret := false
	vclusterName := ""
	vcNamespace := ""
	if identity != nil {
		vclusterName = strings.TrimSpace(identity.VClusterName)
		vcNamespace = strings.TrimSpace(identity.Namespace)
	}
	if identity != nil && strings.TrimSpace(identity.HostNamespace) != "" {
		namespace = identity.HostNamespace
		useHostSecret = true
	} else if identity != nil {
		namespace = identity.Namespace
	}

	results := make([]SecretCheckItem, 0, len(secretNames))
	for _, secretName := range secretNames {
		var secret *corev1.Secret
		var err error

		switch {
		case useHostSecret:
			secret, err = s.clientset.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
			if err != nil && apierrors.IsNotFound(err) && s.vcClient != nil && identity != nil && vclusterName != "" && vcNamespace != "" {
				secret, err = s.getPlatformSecret(ctx, identity, vcNamespace, secretName)
			}
		case s.vcClient != nil && identity != nil && strings.TrimSpace(identity.VClusterName) != "" && strings.TrimSpace(namespace) != "":
			secret, err = s.getPlatformSecret(ctx, identity, namespace, secretName)
		default:
			err = fmt.Errorf("unable to determine secret namespace")
		}
		if err != nil {
			results = append(results, SecretCheckItem{
				SecretName: secretName,
				Status:     "FAIL",
				Message:    formatSecretCheckMessage(err, secretName, namespace),
			})
			continue
		}

		credentials := decodeDockerCredentials(secret)
		if len(credentials) == 0 {
			results = append(results, SecretCheckItem{
				SecretName: secretName,
				Status:     "FAIL",
				Message:    "secret does not contain docker registry credentials",
			})
			continue
		}

		credential := credentials[0]
		if credential.RegistryHasWhitespace {
			results = append(results, SecretCheckItem{
				SecretName: secretName,
				Registry:   credential.RegistryRaw,
				Username:   credential.Username,
				Password:   credential.Password,
				Status:     "FAIL",
				Message:    fmt.Sprintf("secret 中 registry 地址 %q 两端存在空格，请去掉空格后重试", credential.RegistryRaw),
			})
			continue
		}

		registry := strings.TrimSpace(credential.Registry)
		if registry == "" {
			registry = defaultRegistryHost
		}
		if err := verifyDockerLogin(ctx, registry, credential.Username, credential.Password); err != nil {
			status, message := classifyDockerLoginError(err, registry)
			results = append(results, SecretCheckItem{
				SecretName: secretName,
				Registry:   registry,
				Username:   credential.Username,
				Password:   credential.Password,
				Status:     status,
				Message:    message,
			})
			continue
		}

		results = append(results, SecretCheckItem{
			SecretName: secretName,
			Registry:   registry,
			Username:   credential.Username,
			Password:   credential.Password,
			Status:     "OK",
			Message:    "docker login succeeded",
		})
	}

	return results
}

func formatSecretCheckMessage(err error, secretName string, namespace string) string {
	if err == nil {
		return ""
	}

	message := strings.TrimSpace(err.Error())
	lower := strings.ToLower(message)
	if strings.Contains(lower, "secret") && strings.Contains(lower, "not found") {
		ns := strings.TrimSpace(namespace)
		if ns == "" {
			return fmt.Sprintf("secret %q 在当前 namespace 不存在，镜像密钥检测不通过", secretName)
		}
		return fmt.Sprintf("secret %q 在当前 namespace %q 不存在，镜像密钥检测不通过", secretName, ns)
	}
	return message
}

func verifyDockerLogin(ctx context.Context, registry string, username string, password string) error {
	if strings.TrimSpace(username) == "" || strings.TrimSpace(password) == "" {
		return fmt.Errorf("missing username or password")
	}
	if err := preflightDockerRegistry(ctx, registry); err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "rayctl-docker-login-*")
	if err != nil {
		return fmt.Errorf("create temp docker config: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	loginCtx, cancel := context.WithTimeout(ctx, dockerLoginTimeout)
	defer cancel()

	cmd := exec.CommandContext(loginCtx, "docker", "--config", tmpDir, "login", registry, "--username", username, "--password-stdin")
	cmd.Stdin = strings.NewReader(password)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if loginCtx.Err() != nil {
			return fmt.Errorf("docker login exceeded %s: %w", dockerLoginTimeout, loginCtx.Err())
		}
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("%s", message)
	}
	return nil
}

func preflightDockerRegistry(ctx context.Context, registry string) error {
	host, err := dockerRegistryHostname(registry)
	if err != nil {
		return err
	}
	if net.ParseIP(host) != nil || strings.EqualFold(host, "localhost") {
		return nil
	}

	lookupCtx, cancel := context.WithTimeout(ctx, registryLookupTimeout)
	defer cancel()
	if _, err := net.DefaultResolver.LookupHost(lookupCtx, host); err != nil {
		return fmt.Errorf("lookup registry host %q: %w", host, err)
	}
	return nil
}

func dockerRegistryHostname(registry string) (string, error) {
	registry = strings.TrimSpace(registry)
	if registry == "" {
		return "", fmt.Errorf("invalid URL: empty registry address")
	}

	candidate := registry
	if !strings.Contains(candidate, "://") {
		candidate = "//" + candidate
	}
	parsed, err := url.Parse(candidate)
	if err != nil {
		return "", fmt.Errorf("invalid URL %q: %w", registry, err)
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return "", fmt.Errorf("invalid URL %q: registry host is empty", registry)
	}
	return host, nil
}

func isDockerUnavailableError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "executable file not found") || strings.Contains(message, "no such file or directory")
}

func classifyDockerLoginError(err error, registry string) (string, string) {
	if err == nil {
		return "OK", "docker login succeeded"
	}
	if isDockerRegistryAddressError(err) {
		return "FAIL", fmt.Sprintf(
			"镜像地址错误：镜像仓库 %q 无法解析或地址格式不正确，请检查 imagePullSecret 中的 registry 地址。原始错误：%s",
			strings.TrimSpace(registry),
			strings.TrimSpace(err.Error()),
		)
	}
	if isDockerUnavailableError(err) {
		return "ERROR", strings.TrimSpace(err.Error())
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "ERROR", fmt.Sprintf("镜像仓库登录校验超过 %s，暂时无法确认账号密码是否有效", dockerLoginTimeout)
	}
	return "FAIL", strings.TrimSpace(err.Error())
}

func isDockerRegistryAddressError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"no such host",
		"name or service not known",
		"temporary failure in name resolution",
		"server misbehaving",
		"invalid reference format",
		"unsupported protocol scheme",
		"missing port in address",
		"invalid url",
	} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}
