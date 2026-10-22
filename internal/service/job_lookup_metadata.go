package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const jobLookupMetadataPageSize = jobUIDLookupPageSize

const jobLookupMetadataAccept = "application/vnd.kubernetes.protobuf;as=PartialObjectMetadataList;v=v1;g=meta.k8s.io,application/json;as=PartialObjectMetadataList;v=v1;g=meta.k8s.io"

// These resources are excluded by isSSPManagedWorkloadPod too. Filtering them
// at the server avoids transferring unrelated inference/training annotations.
const atePodLookupSelector = sspWorkloadTypeLabel + " notin (" + sspTrainingJobWorkloadType + "," + sspAIDWorkloadTypeValue + ")"

type jobNotFoundError struct{ identifier string }

func (e *jobNotFoundError) Error() string {
	return fmt.Sprintf("ATE 任务 %q 不存在", e.identifier)
}

type jobLookupError struct {
	identifier string
	cause      error
}

func (e *jobLookupError) Error() string {
	return fmt.Sprintf("ATE 任务 %q 查询未完成: %v", e.identifier, e.cause)
}
func (e *jobLookupError) Unwrap() error { return e.cause }

func jobLookupFailure(identifier string, errs ...error) error {
	causes := []error{}
	for _, err := range errs {
		var missing *jobNotFoundError
		if err != nil && !errors.As(err, &missing) {
			causes = append(causes, err)
		}
	}
	if len(causes) == 0 {
		return &jobNotFoundError{identifier: identifier}
	}
	return &jobLookupError{identifier: identifier, cause: errors.Join(causes...)}
}

func isJobLookupFailure(err error) bool {
	var missing *jobNotFoundError
	var incomplete *jobLookupError
	return errors.As(err, &missing) || errors.As(err, &incomplete) || errors.Is(err, ErrPTATEUnavailable)
}

func (s *JobService) listJobLookupMetadata(ctx context.Context) ([]unstructured.Unstructured, error) {
	options := metav1.ListOptions{Limit: jobLookupMetadataPageSize}
	items := []unstructured.Unstructured{}
	seen := map[string]bool{}
	for {
		page, next, err := s.listJobLookupMetadataPage(ctx, options)
		if err != nil {
			return nil, fmt.Errorf("list volcano job metadata: %w", err)
		}
		items = append(items, page...)
		if next == "" {
			return items, nil
		}
		if seen[next] {
			return nil, fmt.Errorf("HC Job metadata pagination cursor repeated; lookup incomplete")
		}
		seen[next] = true
		options.Continue = next
	}
}

func (s *JobService) listJobLookupMetadataPage(ctx context.Context, options metav1.ListOptions) ([]unstructured.Unstructured, string, error) {
	if s.clientset != nil {
		client := s.clientset.CoreV1().RESTClient()
		value := reflect.ValueOf(client)
		if client != nil && !(value.Kind() == reflect.Pointer && value.IsNil()) {
			page := &metav1.PartialObjectMetadataList{}
			err := client.Get().AbsPath("/apis/batch.volcano.sh/v1alpha1/jobs").
				VersionedParams(&options, metav1.ParameterCodec).
				SetHeader("Accept", jobLookupMetadataAccept).
				Do(ctx).Into(page)
			if apierrors.IsNotFound(err) {
				return nil, "", nil // This HC does not serve Volcano Jobs.
			}
			if err == nil {
				items := make([]unstructured.Unstructured, 0, len(page.Items))
				for _, metadata := range page.Items {
					item := unstructured.Unstructured{Object: map[string]any{}}
					item.SetName(metadata.Name)
					item.SetGenerateName(metadata.GenerateName)
					item.SetNamespace(metadata.Namespace)
					item.SetUID(metadata.UID)
					item.SetAnnotations(metadata.Annotations)
					item.SetLabels(metadata.Labels)
					items = append(items, item)
				}
				return items, page.Continue, nil
			}
			if !apierrors.IsNotAcceptable(err) {
				return nil, "", err
			}
		}
	}
	if s.dynamicClient == nil {
		return nil, "", fmt.Errorf("Kubernetes Job client unavailable")
	}
	page, err := s.dynamicClient.Resource(volcanoJobGVR).Namespace(metav1.NamespaceAll).List(ctx, options)
	if apierrors.IsNotFound(err) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	return page.Items, page.GetContinue(), nil
}

func (s *JobService) matchingHostPodMetadata(ctx context.Context, identifier string) ([]corev1.Pod, error) {
	if s.clientset == nil {
		return nil, fmt.Errorf("Kubernetes Pod client unavailable")
	}
	options := metav1.ListOptions{Limit: jobLookupMetadataPageSize, LabelSelector: atePodLookupSelector}
	matched := []corev1.Pod{}
	seen := map[string]bool{}
	for {
		items, next, err := s.listPodMetadataPage(ctx, options)
		if err != nil {
			return nil, fmt.Errorf("list Pod metadata: %w", err)
		}
		for _, metadata := range items {
			pod := corev1.Pod{ObjectMeta: metadata.ObjectMeta}
			if !isSSPManagedWorkloadPod(pod) && podMatchesIdentifier(pod, identifier) {
				matched = append(matched, pod)
			}
		}
		if next == "" {
			return matched, nil
		}
		if seen[next] {
			return nil, fmt.Errorf("HC Pod metadata pagination cursor repeated; lookup incomplete")
		}
		seen[next] = true
		options.Continue = next
	}
}
