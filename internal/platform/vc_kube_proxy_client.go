package platform

import (
	"context"
	"encoding/json"
	"fmt"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func (c *VirtualClusterClient) GetVolcanoJob(ctx context.Context, vclusterName string, namespace string, jobName string) (*unstructured.Unstructured, error) {
	var lastErr error
	for _, profile := range c.orderedProfiles() {
		obj, err := c.getVolcanoJobWithProfile(ctx, profile, vclusterName, namespace, jobName)
		if err != nil {
			lastErr = err
			continue
		}
		return obj, nil
	}
	return nil, lastErr
}

func (c *VirtualClusterClient) GetVolcanoJobForProfile(ctx context.Context, profileName string, vclusterName string, namespace string, jobName string) (*unstructured.Unstructured, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	return c.getVolcanoJobWithProfile(ctx, profile, vclusterName, namespace, jobName)
}

func (c *VirtualClusterClient) getVolcanoJobWithProfile(ctx context.Context, profile clientProfile, vclusterName string, namespace string, jobName string) (*unstructured.Unstructured, error) {
	reqURL := c.kubernetesResourceURLForProfile(profile, vclusterName, fmt.Sprintf("/apis/batch.volcano.sh/v1alpha1/namespaces/%s/jobs/%s", namespace, jobName), nil)
	var obj unstructured.Unstructured
	if err := c.getJSONWithProfile(ctx, profile, reqURL, &obj); err != nil {
		return nil, err
	}
	return &obj, nil
}

func (c *VirtualClusterClient) ListVolcanoJobs(ctx context.Context, vclusterName string, namespace string) ([]unstructured.Unstructured, error) {
	return c.ListVolcanoJobsByLabelSelector(ctx, vclusterName, namespace, "")
}

func (c *VirtualClusterClient) ListVolcanoJobsForProfile(ctx context.Context, profileName string, vclusterName string, namespace string) ([]unstructured.Unstructured, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	return c.listVolcanoJobsWithProfile(ctx, profile, vclusterName, namespace, "")
}

// ListVolcanoJobsPageForProfile uses the ECP cached Job endpoint so callers can
// apply server-side filtering and pagination without downloading Pods first.
// A limit of zero reads every matching page.
func (c *VirtualClusterClient) ListVolcanoJobsPageForProfile(ctx context.Context, profileName string, vclusterName string, filter string, limit int) ([]unstructured.Unstructured, error) {
	return c.ListVolcanoJobsPageForProfileSince(ctx, profileName, vclusterName, filter, limit, time.Time{})
}

func (c *VirtualClusterClient) ListVolcanoJobsPageForProfileSince(ctx context.Context, profileName string, vclusterName string, filter string, limit int, createdAfter time.Time) ([]unstructured.Unstructured, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	if limit < 0 {
		return nil, fmt.Errorf("volcano job limit must not be negative")
	}

	const maxPageSize = 100
	failedFilter := strings.Contains(strings.ToLower(strings.TrimSpace(filter)), `state="failed"`)
	items := make([]unstructured.Unstructured, 0)
	for skip := 0; ; {
		pageSize := maxPageSize
		if limit > 0 && limit-len(items) < pageSize {
			pageSize = limit - len(items)
		}
		if pageSize <= 0 {
			break
		}

		query := url.Values{}
		query.Set("page_size", strconv.Itoa(pageSize))
		query.Set("skip", strconv.Itoa(skip))
		query.Set("filter", strings.TrimSpace(filter))
		order := "created_at desc"
		if failedFilter {
			order = "updated_at desc"
		}
		query.Set("order", order)
		reqURL := c.kubernetesResourceURLForProfile(profile, vclusterName, "/apis/batch.volcano.sh/v1alpha1/jobs", query)

		var page unstructured.UnstructuredList
		if err := c.getJSONWithProfile(ctx, profile, reqURL, &page); err != nil {
			return nil, err
		}
		reachedCutoff := false
		for _, item := range page.Items {
			eventTime := item.GetCreationTimestamp().Time
			if failedFilter {
				eventTime = volcanoJobTerminalTime(item)
			}
			if !createdAfter.IsZero() && !eventTime.IsZero() && eventTime.Before(createdAfter) {
				reachedCutoff = true
				continue
			}
			items = append(items, item)
		}
		count := len(page.Items)
		remaining := page.GetRemainingItemCount()
		if reachedCutoff || count == 0 || count < pageSize || (remaining != nil && *remaining <= 0) || (limit > 0 && len(items) >= limit) {
			break
		}
		skip += count
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func volcanoJobTerminalTime(job unstructured.Unstructured) time.Time {
	latest := time.Time{}
	for _, path := range [][]string{
		{"status", "state", "lastTransitionTime"},
		{"status", "completionTime"},
		{"status", "finishedAt"},
	} {
		value, _, _ := unstructured.NestedString(job.Object, path...)
		if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value)); err == nil && parsed.After(latest) {
			latest = parsed
		}
	}
	conditions, _, _ := unstructured.NestedSlice(job.Object, "status", "conditions")
	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		for _, field := range []string{"lastTransitionTime", "transitionTime", "lastUpdateTime"} {
			value, _, _ := unstructured.NestedString(condition, field)
			if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value)); err == nil && parsed.After(latest) {
				latest = parsed
			}
		}
	}
	if latest.IsZero() {
		return job.GetCreationTimestamp().Time
	}
	return latest
}

func (c *VirtualClusterClient) ListVolcanoJobMetadataForProfile(ctx context.Context, profileName string, vclusterName string, namespace string) ([]metav1.PartialObjectMetadata, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	reqURL := c.kubernetesResourceURLForProfile(profile, vclusterName, fmt.Sprintf("/apis/batch.volcano.sh/v1alpha1/namespaces/%s/jobs", namespace), nil)
	body, err := c.doSignedRequest(ctx, profile, http.MethodGet, reqURL, "", nil, false, "", map[string]string{
		"Accept": "application/json;as=PartialObjectMetadataList;v=v1;g=meta.k8s.io",
	})
	if err != nil {
		return nil, err
	}

	var list metav1.PartialObjectMetadataList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("decode response from %s: %w", reqURL, err)
	}
	return list.Items, nil
}

func (c *VirtualClusterClient) CountVolcanoJobsForProfile(ctx context.Context, profileName string, vclusterName string, namespace string) (int, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return 0, fmt.Errorf("platform profile %q not found", profileName)
	}
	query := url.Values{}
	query.Set("page_size", "1")
	query.Set("skip", "0")
	query.Set("filter", "")
	query.Set("order", "created_at desc")
	reqURL := c.kubernetesResourceURLForProfile(profile, vclusterName, "/apis/batch.volcano.sh/v1alpha1/jobs", query)
	body, err := c.doSignedRequest(ctx, profile, http.MethodGet, reqURL, "", nil, false, "", map[string]string{
		"Accept": "application/json;as=PartialObjectMetadataList;v=v1;g=meta.k8s.io",
	})
	if err != nil {
		return 0, err
	}

	var list metav1.PartialObjectMetadataList
	if err := json.Unmarshal(body, &list); err != nil {
		return 0, fmt.Errorf("decode response from %s: %w", reqURL, err)
	}
	if list.RemainingItemCount != nil {
		return len(list.Items) + int(*list.RemainingItemCount), nil
	}

	items, err := c.ListVolcanoJobMetadataForProfile(ctx, profileName, vclusterName, namespace)
	if err != nil {
		return 0, err
	}
	return len(items), nil
}

func (c *VirtualClusterClient) ListVolcanoJobsByLabelSelector(ctx context.Context, vclusterName string, namespace string, labelSelector string) ([]unstructured.Unstructured, error) {
	var lastErr error
	for _, profile := range c.orderedProfiles() {
		items, err := c.listVolcanoJobsWithProfile(ctx, profile, vclusterName, namespace, labelSelector)
		if err != nil {
			lastErr = err
			continue
		}
		return items, nil
	}
	return nil, lastErr
}

func (c *VirtualClusterClient) listVolcanoJobsWithProfile(ctx context.Context, profile clientProfile, vclusterName string, namespace string, labelSelector string) ([]unstructured.Unstructured, error) {
	query := url.Values{}
	if strings.TrimSpace(labelSelector) != "" {
		query.Set("labelSelector", strings.TrimSpace(labelSelector))
	}
	reqURL := c.kubernetesResourceURLForProfile(profile, vclusterName, fmt.Sprintf("/apis/batch.volcano.sh/v1alpha1/namespaces/%s/jobs", namespace), query)
	var list unstructured.UnstructuredList
	if err := c.getJSONWithProfile(ctx, profile, reqURL, &list); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func (c *VirtualClusterClient) ListPods(ctx context.Context, vclusterName string, namespace string) ([]corev1.Pod, error) {
	var lastErr error
	for _, profile := range c.orderedProfiles() {
		pods, err := c.listPodsWithProfile(ctx, profile, vclusterName, namespace, "")
		if err != nil {
			lastErr = err
			continue
		}
		return pods, nil
	}
	return nil, lastErr
}

func (c *VirtualClusterClient) ListPodsForProfile(ctx context.Context, profileName string, vclusterName string, namespace string) ([]corev1.Pod, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	return c.listPodsWithProfile(ctx, profile, vclusterName, namespace, "")
}

func (c *VirtualClusterClient) ListActivePodsForProfile(ctx context.Context, profileName string, vclusterName string, namespace string) ([]corev1.Pod, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	return c.listPodsWithProfile(ctx, profile, vclusterName, namespace, "status.phase!=Succeeded,status.phase!=Failed")
}

func (c *VirtualClusterClient) listPodsWithProfile(ctx context.Context, profile clientProfile, vclusterName string, namespace string, fieldSelector string) ([]corev1.Pod, error) {
	path := "/api/v1/pods"
	if namespace = strings.TrimSpace(namespace); namespace != "" {
		path = fmt.Sprintf("/api/v1/namespaces/%s/pods", url.PathEscape(namespace))
	}
	query := url.Values{}
	if strings.TrimSpace(fieldSelector) != "" {
		query.Set("fieldSelector", strings.TrimSpace(fieldSelector))
	}
	reqURL := c.kubernetesResourceURLForProfile(profile, vclusterName, path, query)
	var podList corev1.PodList
	if err := c.getJSONWithProfile(ctx, profile, reqURL, &podList); err != nil {
		return nil, err
	}
	return podList.Items, nil
}

func (c *VirtualClusterClient) ListKubernetesNodesForProfile(ctx context.Context, profileName string, vclusterName string) ([]corev1.Node, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	reqURL := c.kubernetesResourceURLForProfile(profile, vclusterName, "/api/v1/nodes", nil)
	var nodeList corev1.NodeList
	if err := c.getJSONWithProfile(ctx, profile, reqURL, &nodeList); err != nil {
		return nil, err
	}
	return nodeList.Items, nil
}

func (c *VirtualClusterClient) ListKubernetesNamespacesForProfile(ctx context.Context, profileName string, vclusterName string) ([]corev1.Namespace, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	reqURL := c.kubernetesResourceURLForProfile(profile, vclusterName, "/api/v1/namespaces", nil)
	var namespaceList corev1.NamespaceList
	if err := c.getJSONWithProfile(ctx, profile, reqURL, &namespaceList); err != nil {
		return nil, err
	}
	return namespaceList.Items, nil
}

func (c *VirtualClusterClient) GetVolcanoQueueForProfile(ctx context.Context, profileName string, vclusterName string, queueName string) (*unstructured.Unstructured, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	queueName = strings.TrimSpace(queueName)
	if queueName == "" {
		return nil, fmt.Errorf("volcano queue name is required")
	}
	reqURL := c.kubernetesResourceURLForProfile(
		profile,
		vclusterName,
		fmt.Sprintf("/apis/scheduling.volcano.sh/v1beta1/queues/%s", url.PathEscape(queueName)),
		nil,
	)
	var queue unstructured.Unstructured
	if err := c.getJSONWithProfile(ctx, profile, reqURL, &queue); err != nil {
		return nil, err
	}
	return &queue, nil
}

func (c *VirtualClusterClient) GetPodGroup(ctx context.Context, vclusterName string, namespace string, podGroupName string) (*unstructured.Unstructured, error) {
	var lastErr error
	for _, profile := range c.orderedProfiles() {
		reqURL := c.kubernetesResourceURLForProfile(profile, vclusterName, fmt.Sprintf("/apis/scheduling.volcano.sh/v1beta1/namespaces/%s/podgroups/%s", namespace, podGroupName), nil)
		var obj unstructured.Unstructured
		if err := c.getJSONWithProfile(ctx, profile, reqURL, &obj); err != nil {
			lastErr = err
			continue
		}
		return &obj, nil
	}
	return nil, lastErr
}

func (c *VirtualClusterClient) GetSecret(ctx context.Context, vclusterName string, namespace string, secretName string) (*corev1.Secret, error) {
	var lastErr error
	for _, profile := range c.orderedProfiles() {
		secret, err := c.getSecretWithProfile(ctx, profile, vclusterName, namespace, secretName)
		if err != nil {
			lastErr = err
			continue
		}
		return secret, nil
	}
	return nil, lastErr
}

func (c *VirtualClusterClient) GetSecretForProfile(ctx context.Context, profileName string, vclusterName string, namespace string, secretName string) (*corev1.Secret, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	return c.getSecretWithProfile(ctx, profile, vclusterName, namespace, secretName)
}

func (c *VirtualClusterClient) getSecretWithProfile(ctx context.Context, profile clientProfile, vclusterName string, namespace string, secretName string) (*corev1.Secret, error) {
	reqURL := c.kubernetesResourceURLForProfile(profile, vclusterName, fmt.Sprintf("/api/v1/namespaces/%s/secrets/%s", namespace, secretName), nil)
	var secret corev1.Secret
	if err := c.getJSONWithProfile(ctx, profile, reqURL, &secret); err != nil {
		return nil, err
	}
	return &secret, nil
}

func (c *VirtualClusterClient) GetPersistentVolumeClaim(ctx context.Context, vclusterName string, namespace string, claimName string) (*corev1.PersistentVolumeClaim, error) {
	var lastErr error
	for _, profile := range c.orderedProfiles() {
		pvc, err := c.getPersistentVolumeClaimWithProfile(ctx, profile, vclusterName, namespace, claimName)
		if err != nil {
			lastErr = err
			continue
		}
		return pvc, nil
	}
	return nil, lastErr
}

func (c *VirtualClusterClient) GetPersistentVolumeClaimForProfile(ctx context.Context, profileName string, vclusterName string, namespace string, claimName string) (*corev1.PersistentVolumeClaim, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	return c.getPersistentVolumeClaimWithProfile(ctx, profile, vclusterName, namespace, claimName)
}

func (c *VirtualClusterClient) getPersistentVolumeClaimWithProfile(ctx context.Context, profile clientProfile, vclusterName string, namespace string, claimName string) (*corev1.PersistentVolumeClaim, error) {
	reqURL := c.kubernetesResourceURLForProfile(profile, vclusterName, fmt.Sprintf("/api/v1/namespaces/%s/persistentvolumeclaims/%s", namespace, claimName), nil)
	var pvc corev1.PersistentVolumeClaim
	if err := c.getJSONWithProfile(ctx, profile, reqURL, &pvc); err != nil {
		return nil, err
	}
	return &pvc, nil
}

func (c *VirtualClusterClient) GetPersistentVolume(ctx context.Context, vclusterName string, pvName string) (*corev1.PersistentVolume, error) {
	var lastErr error
	for _, profile := range c.orderedProfiles() {
		pv, err := c.getPersistentVolumeWithProfile(ctx, profile, vclusterName, pvName)
		if err != nil {
			lastErr = err
			continue
		}
		return pv, nil
	}
	return nil, lastErr
}

func (c *VirtualClusterClient) GetPersistentVolumeForProfile(ctx context.Context, profileName string, vclusterName string, pvName string) (*corev1.PersistentVolume, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	return c.getPersistentVolumeWithProfile(ctx, profile, vclusterName, pvName)
}

func (c *VirtualClusterClient) getPersistentVolumeWithProfile(ctx context.Context, profile clientProfile, vclusterName string, pvName string) (*corev1.PersistentVolume, error) {
	reqURL := c.kubernetesResourceURLForProfile(profile, vclusterName, fmt.Sprintf("/api/v1/persistentvolumes/%s", pvName), nil)
	var pv corev1.PersistentVolume
	if err := c.getJSONWithProfile(ctx, profile, reqURL, &pv); err != nil {
		return nil, err
	}
	return &pv, nil
}

func (c *VirtualClusterClient) ListClusterRoleBindings(ctx context.Context, vclusterName string, labelSelector string) ([]rbacv1.ClusterRoleBinding, error) {
	return c.listClusterRoleBindingsWithProfiles(ctx, c.orderedProfiles(), vclusterName, labelSelector, "")
}

func (c *VirtualClusterClient) ReviewRBACCreateAccessForProfileToken(ctx context.Context, profileName string, vclusterName string, namespace string, resource string, bearerToken string) (*authorizationv1.SelfSubjectAccessReview, error) {
	return c.ReviewRBACAccessForProfileToken(ctx, profileName, vclusterName, namespace, "post", resource, bearerToken)
}

func (c *VirtualClusterClient) ReviewRBACAccessForProfileToken(ctx context.Context, profileName string, vclusterName string, namespace string, verb string, resource string, bearerToken string) (*authorizationv1.SelfSubjectAccessReview, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	if strings.TrimSpace(bearerToken) == "" {
		return nil, fmt.Errorf("console bearer token is required")
	}

	review := authorizationv1.SelfSubjectAccessReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "authorization.k8s.io/v1",
			Kind:       "SelfSubjectAccessReview",
		},
		Spec: authorizationv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Namespace: strings.TrimSpace(namespace),
				Verb:      strings.TrimSpace(verb),
				Group:     "rbac.authorization.k8s.io",
				Resource:  strings.TrimSpace(resource),
			},
		},
	}
	reqURL := c.kubernetesClusterURLForProfile(profile, vclusterName, "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews", nil)
	var out authorizationv1.SelfSubjectAccessReview
	if err := c.postJSONWithBearerProfile(ctx, profile, reqURL, review, bearerToken, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *VirtualClusterClient) ListClusterRoleBindingsForProfile(ctx context.Context, profileName string, vclusterName string, labelSelector string) ([]rbacv1.ClusterRoleBinding, error) {
	return c.ListClusterRoleBindingsForProfileToken(ctx, profileName, vclusterName, labelSelector, "")
}

func (c *VirtualClusterClient) ListClusterRoleBindingsForProfileToken(ctx context.Context, profileName string, vclusterName string, labelSelector string, bearerToken string) ([]rbacv1.ClusterRoleBinding, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	return c.listClusterRoleBindingsWithProfiles(ctx, []clientProfile{profile}, vclusterName, labelSelector, bearerToken)
}

func (c *VirtualClusterClient) listClusterRoleBindingsWithProfiles(ctx context.Context, profiles []clientProfile, vclusterName string, labelSelector string, bearerToken string) ([]rbacv1.ClusterRoleBinding, error) {
	query := url.Values{}
	if strings.TrimSpace(labelSelector) != "" {
		query.Set("labelSelector", strings.TrimSpace(labelSelector))
	}

	var lastErr error
	for _, profile := range profiles {
		reqURL := c.kubernetesClusterURLForProfile(profile, vclusterName, "/apis/rbac.authorization.k8s.io/v1/clusterrolebindings", query)
		var list rbacv1.ClusterRoleBindingList
		var err error
		if strings.TrimSpace(bearerToken) != "" {
			err = c.getJSONWithBearerProfile(ctx, profile, reqURL, bearerToken, &list)
		} else {
			err = c.getJSONWithProfile(ctx, profile, reqURL, &list)
		}
		if err != nil {
			lastErr = err
			continue
		}
		return list.Items, nil
	}
	return nil, lastErr
}

func (c *VirtualClusterClient) ListRoleBindingsForProfileToken(ctx context.Context, profileName string, vclusterName string, namespace string, labelSelector string, bearerToken string) ([]rbacv1.RoleBinding, error) {
	return c.listRoleBindingsForProfileToken(ctx, profileName, vclusterName, strings.TrimSpace(namespace), labelSelector, bearerToken)
}

func (c *VirtualClusterClient) ListAllRoleBindingsForProfileToken(ctx context.Context, profileName string, vclusterName string, labelSelector string, bearerToken string) ([]rbacv1.RoleBinding, error) {
	return c.listRoleBindingsForProfileToken(ctx, profileName, vclusterName, "", labelSelector, bearerToken)
}

func (c *VirtualClusterClient) listRoleBindingsForProfileToken(ctx context.Context, profileName string, vclusterName string, namespace string, labelSelector string, bearerToken string) ([]rbacv1.RoleBinding, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	query := url.Values{}
	if strings.TrimSpace(labelSelector) != "" {
		query.Set("labelSelector", strings.TrimSpace(labelSelector))
	}
	path := "/apis/rbac.authorization.k8s.io/v1/rolebindings"
	if namespace != "" {
		path = fmt.Sprintf("/apis/rbac.authorization.k8s.io/v1/namespaces/%s/rolebindings", url.PathEscape(namespace))
	}
	reqURL := c.kubernetesClusterURLForProfile(profile, vclusterName, path, query)
	var list rbacv1.RoleBindingList
	if err := c.getJSONWithBearerProfile(ctx, profile, reqURL, bearerToken, &list); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func (c *VirtualClusterClient) CreateClusterRoleBindingForProfileToken(ctx context.Context, profileName string, vclusterName string, binding rbacv1.ClusterRoleBinding, bearerToken string) (*rbacv1.ClusterRoleBinding, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	reqURL := c.kubernetesClusterURLForProfile(profile, vclusterName, "/apis/rbac.authorization.k8s.io/v1/clusterrolebindings", nil)
	var out rbacv1.ClusterRoleBinding
	if err := c.postJSONWithBearerProfile(ctx, profile, reqURL, binding, bearerToken, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *VirtualClusterClient) CreateRoleBindingForProfileToken(ctx context.Context, profileName string, vclusterName string, namespace string, binding rbacv1.RoleBinding, bearerToken string) (*rbacv1.RoleBinding, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	path := fmt.Sprintf("/apis/rbac.authorization.k8s.io/v1/namespaces/%s/rolebindings", url.PathEscape(strings.TrimSpace(namespace)))
	reqURL := c.kubernetesClusterURLForProfile(profile, vclusterName, path, nil)
	var out rbacv1.RoleBinding
	if err := c.postJSONWithBearerProfile(ctx, profile, reqURL, binding, bearerToken, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *VirtualClusterClient) DeleteClusterRoleBindingForProfileToken(ctx context.Context, profileName string, vclusterName string, bindingName string, bearerToken string) error {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return fmt.Errorf("platform profile %q not found", profileName)
	}
	bindingName = strings.TrimSpace(bindingName)
	if bindingName == "" {
		return fmt.Errorf("clusterrolebinding name is required")
	}
	path := fmt.Sprintf("/apis/rbac.authorization.k8s.io/v1/clusterrolebindings/%s", url.PathEscape(bindingName))
	reqURL := c.kubernetesClusterURLForProfile(profile, vclusterName, path, nil)
	return c.deleteWithBearerProfile(ctx, profile, reqURL, bearerToken)
}

func (c *VirtualClusterClient) DeleteRoleBindingForProfileToken(ctx context.Context, profileName string, vclusterName string, namespace string, bindingName string, bearerToken string) error {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return fmt.Errorf("platform profile %q not found", profileName)
	}
	namespace = strings.TrimSpace(namespace)
	bindingName = strings.TrimSpace(bindingName)
	if namespace == "" {
		return fmt.Errorf("rolebinding namespace is required")
	}
	if bindingName == "" {
		return fmt.Errorf("rolebinding name is required")
	}
	path := fmt.Sprintf(
		"/apis/rbac.authorization.k8s.io/v1/namespaces/%s/rolebindings/%s",
		url.PathEscape(namespace),
		url.PathEscape(bindingName),
	)
	reqURL := c.kubernetesClusterURLForProfile(profile, vclusterName, path, nil)
	return c.deleteWithBearerProfile(ctx, profile, reqURL, bearerToken)
}

func (c *VirtualClusterClient) ListJobPods(ctx context.Context, vclusterName string, namespace string, jobName string) ([]corev1.Pod, error) {
	var lastErr error
	for _, profile := range c.orderedProfiles() {
		pods, err := c.listJobPodsWithProfile(ctx, profile, vclusterName, namespace, jobName)
		if err != nil {
			lastErr = err
			continue
		}
		return pods, nil
	}
	return nil, lastErr
}

func (c *VirtualClusterClient) ListJobPodsForProfile(ctx context.Context, profileName string, vclusterName string, namespace string, jobName string) ([]corev1.Pod, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	return c.listJobPodsWithProfile(ctx, profile, vclusterName, namespace, jobName)
}

func (c *VirtualClusterClient) listJobPodsWithProfile(ctx context.Context, profile clientProfile, vclusterName string, namespace string, jobName string) ([]corev1.Pod, error) {
	query := url.Values{}
	query.Set("labelSelector", fmt.Sprintf("volcano.sh/job-name=%s", jobName))
	query.Set("filter", fmt.Sprintf("namespace=%q", namespace))
	query.Set("order", "name asc")

	reqURL := c.kubernetesResourceURLForProfile(profile, vclusterName, "/api/v1/pods", query)
	var podList corev1.PodList
	if err := c.getJSONWithProfile(ctx, profile, reqURL, &podList); err != nil {
		return nil, err
	}
	return podList.Items, nil
}

func (c *VirtualClusterClient) ListEvents(ctx context.Context, vclusterName string, namespace string, name string, kind string) ([]corev1.Event, error) {
	query := url.Values{}
	fieldSelector := []string{fmt.Sprintf("involvedObject.name=%s", name)}
	if strings.TrimSpace(kind) != "" {
		fieldSelector = append(fieldSelector, fmt.Sprintf("involvedObject.kind=%s", kind))
	}
	query.Set("fieldSelector", strings.Join(fieldSelector, ","))

	var lastErr error
	for _, profile := range c.orderedCMSProfiles() {
		clusterRef, err := c.cmsClusterRefForProfile(ctx, profile, vclusterName)
		if err != nil {
			lastErr = err
			continue
		}
		queryToken, err := c.cmsQueryTokenForProfile(ctx, profile, clusterRef)
		if err != nil {
			lastErr = err
			continue
		}
		reqURL := c.kubernetesClusterURLForProfile(profile, clusterRef, fmt.Sprintf("/api/v1/namespaces/%s/events", namespace), query)
		var eventList corev1.EventList
		if err := c.getJSONWithCMSProfileToken(ctx, profile, reqURL, queryToken, &eventList); err != nil {
			lastErr = err
			continue
		}
		return eventList.Items, nil
	}
	return nil, lastErr
}

func (c *VirtualClusterClient) kubernetesResourceURLForProfile(profile clientProfile, vclusterName string, path string, query url.Values) string {
	u, _ := url.Parse(profile.KubernetesBaseURL)
	u.Path = fmt.Sprintf("/ecp/v1/kubernetes/virtualClusters/%s%s", vclusterName, path)
	if query != nil {
		u.RawQuery = query.Encode()
	}
	return u.String()
}

func (c *VirtualClusterClient) kubernetesClusterURLForProfile(profile clientProfile, vclusterName string, path string, query url.Values) string {
	u, _ := url.Parse(profile.KubernetesBaseURL)
	u.Path = fmt.Sprintf("/ecp/v1/clusters/%s%s", vclusterName, path)
	if query != nil {
		u.RawQuery = query.Encode()
	}
	return u.String()
}
