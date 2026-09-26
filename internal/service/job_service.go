package service

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"net"
	"net/url"
	"os"
	"os/exec"
	"rayctl/internal/platform"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
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

func (s *JobService) resolveJobIdentity(ctx context.Context, identifier string) (*jobIdentity, []corev1.Pod, error) {
	job, err := s.findJob(ctx, identifier)
	if err == nil {
		return s.resolveJobIdentityFromJobObject(ctx, job)
	}

	return s.resolveJobFromPods(ctx, identifier, err)
}

func (s *JobService) resolveJobInCurrentCluster(ctx context.Context, identifier string) (*jobIdentity, []corev1.Pod, *unstructured.Unstructured, error) {
	job, err := s.findJob(ctx, identifier)
	if err == nil {
		identity, pods, resolveErr := s.resolveJobIdentityFromJobObject(ctx, job)
		if resolveErr != nil {
			return nil, nil, nil, resolveErr
		}
		return identity, pods, job, nil
	}

	identity, pods, resolveErr := s.resolveJobFromPods(ctx, identifier, err)
	if resolveErr != nil {
		return nil, nil, nil, resolveErr
	}
	return identity, pods, nil, nil
}

func (s *JobService) resolveJobIdentityFromJobObject(ctx context.Context, job *unstructured.Unstructured) (*jobIdentity, []corev1.Pod, error) {
	hostNamespace := firstNonEmpty(
		getNestedString(job.Object, "metadata", "annotations", "vcluster.loft.sh/object-host-namespace"),
		job.GetNamespace(),
	)
	namespace := firstNonEmpty(
		getNestedString(job.Object, "metadata", "annotations", "vcluster.loft.sh/object-namespace"),
		job.GetNamespace(),
	)
	jobName := firstNonEmpty(
		getNestedString(job.Object, "metadata", "annotations", "vcluster.loft.sh/object-name"),
		job.GetName(),
	)
	jobUID := firstNonEmpty(
		getNestedString(job.Object, "metadata", "annotations", "vcluster.loft.sh/object-uid"),
		string(job.GetUID()),
	)
	submitter := firstNonEmpty(
		getNestedString(job.Object, "metadata", "labels", "lepton.sensetime.com/submitter"),
		getNestedString(job.Object, "metadata", "annotations", "lepton.sensetime.com/submitter"),
		"-",
	)

	podGroupName, _ := s.findOwnedPodGroupName(ctx, hostNamespace, string(job.GetUID()))
	pods, podErr := s.listJobPods(ctx, hostNamespace, jobName, jobUID)
	if podErr != nil {
		return nil, nil, podErr
	}

	return &jobIdentity{
		Name:          jobName,
		Namespace:     namespace,
		UID:           jobUID,
		Submitter:     submitter,
		PodGroupName:  firstNonEmpty(podGroupName, derivePodGroupName(jobName, jobUID)),
		VClusterName:  s.resolveVClusterNameFromNamespace(ctx, hostNamespace),
		HostNamespace: hostNamespace,
		HostJobName:   job.GetName(),
	}, pods, nil
}

func (s *JobService) locateJobForPlatform(ctx context.Context, identifier string) (*jobIdentity, error) {
	if looksLikeUUID(identifier) {
		identity, err := s.locateJobByUID(ctx, strings.TrimSpace(identifier))
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(identity.VClusterName) == "" {
			return nil, fmt.Errorf("unable to determine vcluster for job uid %q", identifier)
		}
		return identity, nil
	}

	if s.vcClient != nil && shouldLookupJobByPlatform(identifier) {
		if identity, err := s.findPlatformJobIdentity(ctx, identifier); err == nil {
			return identity, nil
		} else {
			var ambiguousErr *ambiguousJobMatchError
			if errors.As(err, &ambiguousErr) {
				return nil, err
			}
		}
	}

	if looksLikePodBackedIdentifier(identifier) {
		if pod, err := s.findSinglePodByJobName(ctx, identifier); err == nil && pod != nil {
			return s.platformIdentityFromHostPod(ctx, *pod), nil
		}
		if pod, err := s.findSeedPodForJobName(ctx, identifier); err == nil && pod != nil {
			return s.platformIdentityFromHostPod(ctx, *pod), nil
		}
	}

	if pod, err := s.findHostPodForIdentifier(ctx, identifier); err == nil && pod != nil {
		return s.platformIdentityFromHostPod(ctx, *pod), nil
	}

	identity, _, err := s.resolveJobIdentity(ctx, identifier)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(identity.VClusterName) == "" {
		return nil, fmt.Errorf("unable to determine vcluster for %q", identifier)
	}
	return identity, nil
}

func (s *JobService) platformIdentityFromHostPod(ctx context.Context, pod corev1.Pod) *jobIdentity {
	identity := jobIdentityFromPod(pod)
	if s.vcClient != nil {
		identity.ProfileName = strings.TrimSpace(s.vcClient.CurrentProfileName())
	}
	if strings.TrimSpace(identity.VClusterName) == "" {
		identity.VClusterName = s.resolveVClusterNameFromNamespace(ctx, pod.Namespace)
	}
	return identity
}

type jobUIDLookupResult struct {
	identity *jobIdentity
	err      error
}

func (s *JobService) locateJobByUID(ctx context.Context, uid string) (*jobIdentity, error) {
	lookupCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	resultCount := 0
	results := make(chan jobUIDLookupResult, 2)
	if s.clientset != nil {
		resultCount++
		go func() {
			identity, err := s.findPodIdentityByUID(lookupCtx, uid)
			results <- jobUIDLookupResult{identity: identity, err: err}
		}()
	}
	if s.dynamicClient != nil {
		resultCount++
		go func() {
			identity, err := s.findHostJobIdentityByUID(lookupCtx, uid)
			results <- jobUIDLookupResult{identity: identity, err: err}
		}()
	}
	if resultCount == 0 {
		return nil, fmt.Errorf("kubernetes clients are required for job uid lookup")
	}

	errorsBySource := make([]string, 0, resultCount)
	for range resultCount {
		result := <-results
		if result.identity != nil {
			cancel()
			return result.identity, nil
		}
		if result.err != nil && !errors.Is(result.err, context.Canceled) {
			errorsBySource = append(errorsBySource, result.err.Error())
		}
	}
	if len(errorsBySource) > 0 {
		return nil, fmt.Errorf("job uid %q not found: %s", uid, strings.Join(errorsBySource, "; "))
	}
	return nil, fmt.Errorf("job uid %q not found", uid)
}

func (s *JobService) findPodIdentityByUID(ctx context.Context, uid string) (*jobIdentity, error) {
	identity, err := s.findPodIdentityByUIDAndSelector(ctx, uid, "volcano.sh/job-name")
	if err == nil {
		return identity, nil
	}
	return s.findPodIdentityByUIDAndSelector(ctx, uid, "")
}

func (s *JobService) findPodIdentityByUIDAndSelector(ctx context.Context, uid string, labelSelector string) (*jobIdentity, error) {
	options := metav1.ListOptions{
		LabelSelector:   labelSelector,
		Limit:           jobUIDLookupPageSize,
		ResourceVersion: "0",
	}
	for {
		podMetadata, continueToken, err := s.listPodMetadataPage(ctx, options)
		if err != nil {
			return nil, fmt.Errorf("list pods for job uid: %w", err)
		}
		for i := range podMetadata {
			pod := corev1.Pod{ObjectMeta: podMetadata[i].ObjectMeta}
			if string(pod.UID) != uid && deriveJobUID(pod) != uid && !hasOwnerUID(pod.OwnerReferences, uid) {
				continue
			}
			identity := jobIdentityFromPod(pod)
			identity.UID = firstNonEmpty(identity.UID, uid)
			if strings.TrimSpace(identity.VClusterName) == "" {
				identity.VClusterName = s.resolveVClusterNameFromNamespace(ctx, pod.Namespace)
			}
			return identity, nil
		}
		if strings.TrimSpace(continueToken) == "" {
			break
		}
		options.Continue = continueToken
		options.ResourceVersion = ""
	}
	return nil, fmt.Errorf("pod for job uid not found")
}

func (s *JobService) listPodMetadataPage(ctx context.Context, options metav1.ListOptions) ([]metav1.PartialObjectMetadata, string, error) {
	restClient := s.clientset.CoreV1().RESTClient()
	restClientValue := reflect.ValueOf(restClient)
	if restClient != nil && !(restClientValue.Kind() == reflect.Pointer && restClientValue.IsNil()) {
		list := &metav1.PartialObjectMetadataList{}
		err := restClient.Get().
			Resource("pods").
			VersionedParams(&options, metav1.ParameterCodec).
			SetHeader("Accept", "application/json;as=PartialObjectMetadataList;v=v1;g=meta.k8s.io").
			Do(ctx).
			Into(list)
		if err != nil {
			return nil, "", err
		}
		return list.Items, list.GetContinue(), nil
	}

	pods, err := s.clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, options)
	if err != nil {
		return nil, "", err
	}
	items := make([]metav1.PartialObjectMetadata, 0, len(pods.Items))
	for i := range pods.Items {
		items = append(items, metav1.PartialObjectMetadata{ObjectMeta: pods.Items[i].ObjectMeta})
	}
	return items, pods.Continue, nil
}

func (s *JobService) findHostJobIdentityByUID(ctx context.Context, uid string) (*jobIdentity, error) {
	options := metav1.ListOptions{
		Limit:           jobUIDLookupPageSize,
		ResourceVersion: "0",
	}
	for {
		jobs, err := s.dynamicClient.Resource(volcanoJobGVR).Namespace(metav1.NamespaceAll).List(ctx, options)
		if err != nil {
			return nil, fmt.Errorf("list volcano jobs for uid: %w", err)
		}
		for i := range jobs.Items {
			job := &jobs.Items[i]
			logicalUID := strings.TrimSpace(getNestedString(job.Object, "metadata", "annotations", "vcluster.loft.sh/object-uid"))
			if string(job.GetUID()) != uid && logicalUID != uid {
				continue
			}
			hostNamespace := firstNonEmpty(
				getNestedString(job.Object, "metadata", "annotations", "vcluster.loft.sh/object-host-namespace"),
				job.GetNamespace(),
			)
			identity := &jobIdentity{
				Name:          firstNonEmpty(getNestedString(job.Object, "metadata", "annotations", "vcluster.loft.sh/object-name"), job.GetName()),
				Namespace:     firstNonEmpty(getNestedString(job.Object, "metadata", "annotations", "vcluster.loft.sh/object-namespace"), job.GetNamespace()),
				UID:           firstNonEmpty(logicalUID, string(job.GetUID()), uid),
				Submitter:     firstNonEmpty(getNestedString(job.Object, "metadata", "labels", "lepton.sensetime.com/submitter"), getNestedString(job.Object, "metadata", "annotations", "lepton.sensetime.com/submitter"), "-"),
				HostNamespace: hostNamespace,
				HostJobName:   job.GetName(),
			}
			identity.PodGroupName = derivePodGroupName(identity.Name, identity.UID)
			identity.VClusterName = s.resolveVClusterNameFromNamespace(ctx, hostNamespace)
			return identity, nil
		}
		if strings.TrimSpace(jobs.GetContinue()) == "" {
			break
		}
		options.Continue = jobs.GetContinue()
		options.ResourceVersion = ""
	}
	return nil, fmt.Errorf("volcano job uid not found")
}

func looksLikePodBackedIdentifier(identifier string) bool {
	id := strings.ToLower(strings.TrimSpace(identifier))
	if id == "" {
		return false
	}
	return strings.Contains(id, "-worker-") || strings.Contains(id, "-master-") || strings.Contains(id, "-launcher-") || strings.Contains(id, "-chief-")
}

func (s *JobService) findSeedPodForJobName(ctx context.Context, jobName string) (*corev1.Pod, error) {
	candidates := []string{
		jobName + "-worker-0",
		jobName + "-master-0",
		jobName + "-launcher-0",
		jobName + "-chief-0",
	}

	for _, candidate := range candidates {
		pod, err := s.findSinglePodByName(ctx, candidate)
		if err == nil && pod != nil {
			return pod, nil
		}
	}

	return nil, fmt.Errorf("seed pod for job %q not found", jobName)
}

func (s *JobService) findPlatformJobIdentity(ctx context.Context, identifier string) (*jobIdentity, error) {
	currentProfile := strings.TrimSpace(s.vcClient.CurrentProfileName())
	currentClusters, currentErr := s.vcClient.ListCurrentProfileVirtualClusters(ctx)
	if currentErr == nil {
		identity, err := s.findPlatformJobIdentityInClusters(ctx, identifier, currentClusters)
		if err == nil {
			return identity, nil
		}
		var ambiguousErr *ambiguousJobMatchError
		if errors.As(err, &ambiguousErr) {
			return nil, err
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
	}

	vclusters, err := s.vcClient.ListVirtualClusters(ctx)
	if err != nil {
		if currentErr != nil {
			return nil, currentErr
		}
		return nil, err
	}
	if currentErr == nil && currentProfile != "" {
		filtered := vclusters[:0]
		for _, vc := range vclusters {
			if strings.EqualFold(strings.TrimSpace(vc.ProfileName), currentProfile) {
				continue
			}
			filtered = append(filtered, vc)
		}
		vclusters = filtered
	}
	return s.findPlatformJobIdentityInClusters(ctx, identifier, vclusters)
}

func (s *JobService) findPlatformJobIdentityInClusters(ctx context.Context, identifier string, vclusters []platform.VirtualCluster) (*jobIdentity, error) {
	if len(vclusters) == 0 {
		return nil, fmt.Errorf("platform volcano job %q not found", identifier)
	}

	type searchResult struct {
		identity *jobIdentity
		err      error
	}

	scanCtx, cancel := context.WithTimeout(ctx, platformJobScanTimeout)
	defer cancel()

	results := make(chan searchResult, len(vclusters))
	semaphore := make(chan struct{}, platformJobProbeConcurrency)
	var wg sync.WaitGroup

	for _, vc := range vclusters {
		vc := vc
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-scanCtx.Done():
				results <- searchResult{err: scanCtx.Err()}
				return
			}
			for _, vcRef := range platformVCRefs(vc) {
				// Names are not confined to default; preserve every namespace match.
				jobs, getErr := s.vcClient.ListVolcanoJobsPageForProfile(scanCtx, vc.ProfileName, vcRef, "name="+strconv.Quote(identifier), 0)
				if getErr != nil {
					continue
				}
				for i := range jobs {
					job := &jobs[i]
					if job.GetName() != identifier {
						continue
					}
					results <- searchResult{
						identity: &jobIdentity{
							Name:         job.GetName(),
							Namespace:    job.GetNamespace(),
							UID:          string(job.GetUID()),
							Submitter:    firstNonEmpty(getNestedString(job.Object, "metadata", "labels", "lepton.sensetime.com/submitter"), "-"),
							PodGroupName: fmt.Sprintf("%s-%s", job.GetName(), string(job.GetUID())),
							VClusterName: vcRef,
							VClusterUID:  vc.UID,
							ProfileName:  vc.ProfileName,
						},
					}
				}
				return
			}

			results <- searchResult{}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	matches := make([]jobIdentity, 0, len(vclusters))
	seen := make(map[string]struct{}, len(vclusters))
	for result := range results {
		if result.err != nil {
			continue
		}
		if result.identity != nil {
			key := strings.Join([]string{
				strings.TrimSpace(result.identity.VClusterName),
				strings.TrimSpace(result.identity.Namespace),
				strings.TrimSpace(result.identity.Name),
				strings.TrimSpace(result.identity.UID),
			}, "|")
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			matches = append(matches, *result.identity)
		}
	}

	for index := range matches {
		matches[index].HostNamespace = s.findVClusterHostNamespace(ctx, matches[index].VClusterUID, matches[index].Namespace)
	}
	if len(matches) == 1 {
		return &matches[0], nil
	}
	if len(matches) > 1 {
		return nil, &ambiguousJobMatchError{
			Identifier: identifier,
			Matches:    matches,
		}
	}
	if err := scanCtx.Err(); err != nil {
		return nil, fmt.Errorf("platform volcano job %q scan did not finish: %w", identifier, err)
	}

	return nil, fmt.Errorf("platform volcano job %q not found", identifier)
}

func (s *JobService) findVClusterHostNamespace(ctx context.Context, vclusterUID string, virtualNamespace string) string {
	if s == nil || s.clientset == nil {
		return ""
	}
	controlNamespace := "vc-" + strings.TrimPrefix(strings.TrimSpace(vclusterUID), "vc-")
	if controlNamespace == "vc-" {
		return ""
	}
	namespaces, err := s.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: clusterVirtualNamespaceLabelKey + "=" + controlNamespace,
	})
	if err != nil {
		return ""
	}
	for _, namespace := range namespaces.Items {
		logicalName := firstNonEmpty(
			strings.TrimSpace(namespace.Labels[clusterLogicalNamespaceLabelKey]),
			strings.TrimSpace(namespace.Annotations[clusterLogicalNamespaceAnnotKey]),
		)
		if strings.EqualFold(logicalName, strings.TrimSpace(virtualNamespace)) {
			return namespace.Name
		}
	}
	return ""
}

func platformVCRefs(vc platform.VirtualCluster) []string {
	candidates := make([]string, 0, 2)
	seen := make(map[string]struct{}, 2)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		candidates = append(candidates, value)
	}

	if uid := strings.TrimSpace(vc.UID); uid != "" {
		add("vc-" + uid)
		return candidates
	}
	add(vc.Name)
	add(vc.DisplayName)
	return candidates
}

func (s *JobService) locatePodGroupForPlatform(ctx context.Context, identifier string) (*jobIdentity, error) {
	pod, err := s.findHostPodForIdentifier(ctx, identifier)
	if err == nil && pod != nil {
		identity := jobIdentityFromPod(*pod)
		if strings.TrimSpace(identity.PodGroupName) != "" {
			return identity, nil
		}
	}

	return nil, fmt.Errorf("unable to determine vcluster podgroup identity for %q from host cluster pod data", identifier)
}

func (s *JobService) GetPodGroup(ctx context.Context, identifier string) (*PodGroupGetResult, error) {
	if s.vcClient != nil {
		if result, err := s.getPodGroupViaPlatform(ctx, identifier); err == nil {
			return result, nil
		}
	}

	pg, err := s.findPodGroup(ctx, identifier)
	if err != nil {
		return nil, err
	}

	status := firstNonEmpty(
		getNestedText(pg.Object, "status", "phase"),
		getNestedText(pg.Object, "status", "state"),
		"-",
	)
	minMember := firstNonEmpty(
		getNestedText(pg.Object, "spec", "minMember"),
		getNestedText(pg.Object, "spec", "minMemberCount"),
		"-",
	)
	queue := firstNonEmpty(
		getNestedText(pg.Object, "spec", "queue"),
		getNestedText(pg.Object, "spec", "queueName"),
		"-",
	)

	return &PodGroupGetResult{
		Name:           pg.GetName(),
		Namespace:      pg.GetNamespace(),
		Status:         status,
		MinMember:      minMember,
		Queue:          queue,
		CreatedAt:      pg.GetCreationTimestamp().Local().Format(time.RFC3339),
		StatusMessages: extractPodGroupMessages(pg),
	}, nil
}

func (s *JobService) getPodGroupViaPlatform(ctx context.Context, identifier string) (*PodGroupGetResult, error) {
	identity, err := s.locatePodGroupForPlatform(ctx, identifier)
	if err != nil {
		return nil, err
	}

	pg, err := s.vcClient.GetPodGroup(ctx, identity.VClusterName, identity.Namespace, identity.PodGroupName)
	if err != nil {
		return nil, err
	}

	return &PodGroupGetResult{
		Name:      pg.GetName(),
		Namespace: pg.GetNamespace(),
		Status: firstNonEmpty(
			getNestedText(pg.Object, "status", "phase"),
			getNestedText(pg.Object, "status", "state"),
			"-",
		),
		MinMember: firstNonEmpty(
			getNestedText(pg.Object, "spec", "minMember"),
			getNestedText(pg.Object, "spec", "minMemberCount"),
			"-",
		),
		Queue: firstNonEmpty(
			getNestedText(pg.Object, "spec", "queue"),
			getNestedText(pg.Object, "spec", "queueName"),
			"-",
		),
		CreatedAt:      pg.GetCreationTimestamp().Local().Format(time.RFC3339),
		StatusMessages: extractPodGroupMessages(pg),
	}, nil
}

func (s *JobService) findJob(ctx context.Context, identifier string) (*unstructured.Unstructured, error) {
	list, err := s.dynamicClient.Resource(volcanoJobGVR).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list volcano jobs: %w", err)
	}

	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, fmt.Errorf("job identifier is required")
	}

	var exact []*unstructured.Unstructured
	var fuzzy []*unstructured.Unstructured
	for i := range list.Items {
		item := &list.Items[i]
		logicalName := strings.TrimSpace(getNestedString(item.Object, "metadata", "annotations", "vcluster.loft.sh/object-name"))
		logicalUID := strings.TrimSpace(getNestedString(item.Object, "metadata", "annotations", "vcluster.loft.sh/object-uid"))
		switch {
		case item.GetName() == identifier || string(item.GetUID()) == identifier || logicalName == identifier || logicalUID == identifier:
			exact = append(exact, item)
		case strings.HasPrefix(item.GetName(), identifier), strings.HasPrefix(item.GetGenerateName(), identifier), strings.HasPrefix(logicalName, identifier):
			fuzzy = append(fuzzy, item)
		}
	}

	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		return nil, &ambiguousJobMatchError{
			Identifier: identifier,
			Matches:    buildAmbiguousMatchesFromJobs(exact),
		}
	}
	if len(fuzzy) == 1 {
		return fuzzy[0], nil
	}
	if len(fuzzy) > 1 {
		return nil, &ambiguousJobMatchError{
			Identifier: identifier,
			Matches:    buildAmbiguousMatchesFromJobs(fuzzy),
		}
	}

	return nil, fmt.Errorf("volcano job %q not found in current cluster", identifier)
}

func buildAmbiguousMatchesFromJobs(items []*unstructured.Unstructured) []jobIdentity {
	matches := make([]jobIdentity, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		match := jobIdentity{
			Name:          firstNonEmpty(getNestedString(item.Object, "metadata", "annotations", "vcluster.loft.sh/object-name"), item.GetName()),
			Namespace:     firstNonEmpty(getNestedString(item.Object, "metadata", "annotations", "vcluster.loft.sh/object-namespace"), item.GetNamespace()),
			UID:           firstNonEmpty(getNestedString(item.Object, "metadata", "annotations", "vcluster.loft.sh/object-uid"), string(item.GetUID())),
			Submitter:     firstNonEmpty(getNestedString(item.Object, "metadata", "labels", "lepton.sensetime.com/submitter"), getNestedString(item.Object, "metadata", "annotations", "lepton.sensetime.com/submitter"), "-"),
			HostNamespace: firstNonEmpty(getNestedString(item.Object, "metadata", "annotations", "vcluster.loft.sh/object-host-namespace"), item.GetNamespace()),
			HostJobName:   item.GetName(),
		}
		key := strings.Join([]string{match.Namespace, match.Name, match.UID, match.HostNamespace, match.HostJobName}, "|")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		matches = append(matches, match)
	}
	return matches
}

func (s *JobService) resolveVClusterNameFromNamespace(ctx context.Context, namespace string) string {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return ""
	}
	ns, err := s.clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		return ""
	}
	return firstNonEmpty(
		strings.TrimSpace(ns.Labels["vcluster.loft.sh/vcluster-name"]),
		strings.TrimSpace(ns.Labels["vcluster.loft.sh/vcluster-namespace"]),
	)
}

func extractVolcanoJobPhase(job *unstructured.Unstructured) string {
	if job == nil {
		return "-"
	}
	return dashIfEmpty(firstNonEmpty(
		getNestedText(job.Object, "status", "state", "phase"),
		getNestedText(job.Object, "status", "phase"),
		getNestedText(job.Object, "status", "state"),
	))
}

func isActiveVolcanoJobPhase(phase string) bool {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "running", "pending":
		return true
	default:
		return false
	}
}

func isTerminalVolcanoJobPhase(phase string) bool {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "aborted", "completed", "failed", "terminated", "succeeded":
		return true
	default:
		return false
	}
}

func summarizeJobPods(pods []corev1.Pod) (runningCount int, pendingCount int, activeCount int) {
	for _, pod := range pods {
		switch pod.Status.Phase {
		case corev1.PodRunning:
			runningCount++
			activeCount++
		case corev1.PodPending:
			pendingCount++
			activeCount++
		case corev1.PodUnknown:
			activeCount++
		}
	}
	return runningCount, pendingCount, activeCount
}

func desiredReplicaTotal(job *unstructured.Unstructured) int {
	if job == nil {
		return 0
	}
	total := 0
	tasks, found, err := unstructured.NestedSlice(job.Object, "spec", "tasks")
	if err == nil && found {
		for _, task := range tasks {
			taskMap, ok := task.(map[string]any)
			if !ok {
				continue
			}
			replicas, found, err := unstructured.NestedInt64(taskMap, "replicas")
			if err == nil && found {
				total += int(replicas)
			}
		}
	}
	if total > 0 {
		return total
	}
	if minAvailable, found, err := unstructured.NestedInt64(job.Object, "spec", "minAvailable"); err == nil && found {
		return int(minAvailable)
	}
	return 0
}

func (s *JobService) resolveClusterTarget(ctx context.Context, identifier string) (string, string, string, map[string]struct{}, error) {
	id := strings.TrimSpace(identifier)
	if id == "" {
		return "", "", "", nil, fmt.Errorf("cluster identifier is required")
	}

	if s.vcClient == nil {
		return id, id, "", clusterRefSet(clusterRefCandidates(id)), nil
	}

	vclusters, err := s.vcClient.ListCurrentProfileVirtualClusters(ctx)
	if err != nil {
		vclusters = nil
	}
	selected := matchingVirtualClusters(vclusters, id)
	if len(selected) == 0 {
		vclusters, err = s.vcClient.ListVirtualClusters(ctx)
		if err != nil {
			return id, id, "", clusterRefSet(clusterRefCandidates(id)), nil
		}
		selected = matchingVirtualClusters(vclusters, id)
	}

	if len(selected) == 0 {
		return id, id, "", clusterRefSet(clusterRefCandidates(id)), nil
	}
	if len(selected) > 1 {
		names := make([]string, 0, len(selected))
		for _, vc := range selected {
			names = append(names, firstNonEmpty(strings.TrimSpace(vc.Name), strings.TrimSpace(vc.DisplayName), "vc-"+strings.TrimSpace(vc.UID)))
		}
		sort.Strings(names)
		return "", "", "", nil, fmt.Errorf("multiple clusters matched %q: %s", id, strings.Join(names, ", "))
	}

	vc := selected[0]
	display := firstNonEmpty(strings.TrimSpace(vc.Name), strings.TrimSpace(vc.DisplayName), "vc-"+strings.TrimSpace(vc.UID))
	return display, firstNonEmpty("vc-"+strings.TrimSpace(vc.UID), strings.TrimSpace(vc.Name), strings.TrimSpace(vc.UID)), strings.TrimSpace(vc.ProfileName), clusterRefSet(clusterRefCandidates(vc.Name, vc.DisplayName, vc.UID, "vc-"+strings.TrimSpace(vc.UID))), nil
}

func matchingVirtualClusters(vclusters []platform.VirtualCluster, identifier string) []platform.VirtualCluster {
	exact := make([]platform.VirtualCluster, 0)
	fuzzy := make([]platform.VirtualCluster, 0)
	for _, vc := range vclusters {
		candidates := clusterRefCandidates(vc.Name, vc.DisplayName, vc.UID, "vc-"+strings.TrimSpace(vc.UID))
		if clusterCandidateContains(candidates, identifier, true) {
			exact = append(exact, vc)
			continue
		}
		if clusterCandidateContains(candidates, identifier, false) {
			fuzzy = append(fuzzy, vc)
		}
	}

	if len(exact) > 0 {
		return exact
	}
	return fuzzy
}

func clusterRefCandidates(values ...string) []string {
	result := make([]string, 0, len(values)+2)
	seen := make(map[string]struct{}, len(values)+2)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	for _, value := range values {
		add(value)
		if strings.HasPrefix(strings.TrimSpace(value), "vc-") {
			add(strings.TrimPrefix(strings.TrimSpace(value), "vc-"))
		} else if looksLikeUUID(strings.TrimSpace(value)) {
			add("vc-" + strings.TrimSpace(value))
		}
	}
	return result
}

func clusterRefSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[strings.TrimSpace(value)] = struct{}{}
	}
	return result
}

func clusterCandidateContains(candidates []string, identifier string, exact bool) bool {
	id := strings.TrimSpace(identifier)
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if exact {
			if candidate == id {
				return true
			}
			continue
		}
		if strings.HasPrefix(candidate, id) {
			return true
		}
	}
	return false
}

func (s *JobService) findPodGroup(ctx context.Context, identifier string) (*unstructured.Unstructured, error) {
	list, err := s.dynamicClient.Resource(volcanoPodGroupGVR).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list podgroups: %w", err)
	}

	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, fmt.Errorf("podgroup identifier is required")
	}

	var matched []*unstructured.Unstructured
	for i := range list.Items {
		item := &list.Items[i]
		if item.GetName() == identifier || string(item.GetUID()) == identifier {
			matched = append(matched, item)
		}
	}

	if len(matched) == 1 {
		return matched[0], nil
	}
	if len(matched) > 1 {
		names := make([]string, 0, len(matched))
		for _, item := range matched {
			names = append(names, fmt.Sprintf("%s/%s", item.GetNamespace(), item.GetName()))
		}
		sort.Strings(names)
		return nil, fmt.Errorf("multiple podgroups matched %q: %s", identifier, strings.Join(names, ", "))
	}

	return nil, fmt.Errorf("podgroup %q not found in current cluster", identifier)
}

func (s *JobService) findOwnedPodGroupName(ctx context.Context, namespace string, ownerUID string) (string, error) {
	list, err := s.dynamicClient.Resource(volcanoPodGroupGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("list podgroups in namespace %q: %w", namespace, err)
	}

	for i := range list.Items {
		item := &list.Items[i]
		owners, found, err := unstructured.NestedSlice(item.Object, "metadata", "ownerReferences")
		if !found || err != nil {
			continue
		}
		for _, owner := range owners {
			ownerMap, ok := owner.(map[string]any)
			if !ok {
				continue
			}
			if fmt.Sprintf("%v", ownerMap["uid"]) == ownerUID {
				return item.GetName(), nil
			}
		}
	}

	return "", nil
}

func (s *JobService) resolveJobFromPods(ctx context.Context, identifier string, jobLookupErr error) (*jobIdentity, []corev1.Pod, error) {
	allPods, err := s.clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("%v; fallback list pods failed: %w", jobLookupErr, err)
	}

	candidates := make([]corev1.Pod, 0)
	for _, pod := range allPods.Items {
		if !isSSPManagedWorkloadPod(pod) && podMatchesIdentifier(pod, identifier) {
			candidates = append(candidates, pod)
		}
	}

	if len(candidates) == 0 {
		return nil, nil, jobLookupErr
	}

	groups := groupPodsByDerivedJob(candidates)
	if len(groups) == 0 {
		return nil, nil, jobLookupErr
	}
	if len(groups) > 1 {
		return nil, nil, &ambiguousJobMatchError{
			Identifier: identifier,
			Matches:    buildAmbiguousMatchesFromPodGroups(groups),
		}
	}

	var selectedKey string
	var selectedPods []corev1.Pod
	for key, pods := range groups {
		selectedKey = key
		selectedPods = pods
	}

	sort.Slice(selectedPods, func(i, j int) bool {
		return podInspectLess(selectedPods[i], selectedPods[j])
	})

	seed := selectedPods[0]
	identity := &jobIdentity{
		Name:         deriveJobName(seed),
		Namespace:    deriveLogicalNamespace(seed),
		UID:          deriveJobUID(seed),
		Submitter:    firstNonEmpty(seed.Labels["lepton.sensetime.com/submitter"], seed.Annotations["lepton.sensetime.com/submitter"], "-"),
		PodGroupName: firstNonEmpty(seed.Annotations["scheduling.k8s.io/group-name"], seed.Labels["scheduling.k8s.io/group-name"]),
		VClusterName: firstNonEmpty(seed.Annotations["vcluster.loft.sh/vcluster-name"], seed.Labels["vcluster.loft.sh/vcluster-name"]),
	}

	_ = selectedKey
	siblingPods, err := s.listSiblingPods(ctx, seed, identity)
	if err != nil {
		return identity, selectedPods, nil
	}
	if len(siblingPods) == 0 {
		return identity, selectedPods, nil
	}
	return identity, siblingPods, nil
}

func buildAmbiguousMatchesFromPodGroups(groups map[string][]corev1.Pod) []jobIdentity {
	matches := make([]jobIdentity, 0, len(groups))
	for _, pods := range groups {
		if len(pods) == 0 {
			continue
		}
		identity := jobIdentityFromPod(pods[0])
		matches = append(matches, *identity)
	}
	return matches
}

func (s *JobService) listSiblingPods(ctx context.Context, seed corev1.Pod, identity *jobIdentity) ([]corev1.Pod, error) {
	namespace := seed.Namespace
	if strings.TrimSpace(namespace) == "" {
		return []corev1.Pod{seed}, nil
	}

	if jobName := strings.TrimSpace(seed.Labels["volcano.sh/job-name"]); jobName != "" {
		pods, err := s.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("volcano.sh/job-name=%s", jobName),
		})
		if err == nil && len(pods.Items) > 0 {
			return pods.Items, nil
		}
	}

	allPods, err := s.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	jobName := identity.Name
	jobUID := identity.UID
	matched := make([]corev1.Pod, 0)
	for _, pod := range allPods.Items {
		if deriveJobName(pod) == jobName {
			matched = append(matched, pod)
			continue
		}
		if jobUID != "" && deriveJobUID(pod) == jobUID {
			matched = append(matched, pod)
		}
	}
	return matched, nil
}

func (s *JobService) findHostPodForIdentifier(ctx context.Context, identifier string) (*corev1.Pod, error) {
	id := strings.TrimSpace(identifier)
	if id == "" {
		return nil, fmt.Errorf("job identifier is required")
	}

	if !looksLikeUUID(id) {
		if pod, err := s.findSinglePodByName(ctx, id); err == nil && pod != nil && !isSSPManagedWorkloadPod(*pod) {
			return pod, nil
		}
		if pod, err := s.findSinglePodByJobName(ctx, id); err == nil && pod != nil && !isSSPManagedWorkloadPod(*pod) {
			return pod, nil
		}
	}

	allPods, err := s.clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("fallback list pods: %w", err)
	}

	matched := make([]corev1.Pod, 0)
	for _, pod := range allPods.Items {
		if !isSSPManagedWorkloadPod(pod) && podMatchesIdentifier(pod, id) {
			matched = append(matched, pod)
		}
	}
	if len(matched) == 0 {
		return nil, fmt.Errorf("host cluster pod for %q not found", id)
	}

	groups := groupPodsByDerivedJob(matched)
	if len(groups) > 1 {
		return nil, &ambiguousJobMatchError{
			Identifier: id,
			Matches:    buildAmbiguousMatchesFromPodGroups(groups),
		}
	}

	sort.Slice(matched, func(i, j int) bool {
		return podInspectLess(matched[i], matched[j])
	})
	return &matched[0], nil
}

func (s *JobService) findSinglePodByName(ctx context.Context, podName string) (*corev1.Pod, error) {
	pods, err := s.clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{
		FieldSelector:   fields.OneTermEqualSelector("metadata.name", podName).String(),
		ResourceVersion: "0",
	})
	if err != nil {
		return nil, err
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("pod %q not found", podName)
	}
	if len(pods.Items) > 1 {
		return nil, fmt.Errorf("multiple pods named %q found", podName)
	}
	return &pods.Items[0], nil
}

func (s *JobService) findSinglePodByJobName(ctx context.Context, jobName string) (*corev1.Pod, error) {
	pods, err := s.clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{
		LabelSelector:   fmt.Sprintf("volcano.sh/job-name=%s", jobName),
		ResourceVersion: "0",
	})
	if err != nil {
		return nil, err
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("job pod %q not found", jobName)
	}
	sort.Slice(pods.Items, func(i, j int) bool {
		return podInspectLess(pods.Items[i], pods.Items[j])
	})
	return &pods.Items[0], nil
}

func (s *JobService) listJobPods(ctx context.Context, namespace string, jobName string, jobUID string) ([]corev1.Pod, error) {
	options := metav1.ListOptions{
		LabelSelector: fmt.Sprintf("volcano.sh/job-name=%s", jobName),
	}
	podList, err := s.clientset.CoreV1().Pods(namespace).List(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("list pods for volcano job %q in namespace %q: %w", jobName, namespace, err)
	}

	if len(podList.Items) > 0 {
		return podList.Items, nil
	}

	allPods, err := s.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("fallback list pods for namespace %q: %w", namespace, err)
	}

	matched := make([]corev1.Pod, 0)
	for _, pod := range allPods.Items {
		if pod.Labels["volcano.sh/job-name"] == jobName {
			matched = append(matched, pod)
			continue
		}
		if hasOwnerUID(pod.OwnerReferences, jobUID) {
			matched = append(matched, pod)
		}
	}
	return matched, nil
}

func (s *JobService) listEventsForObject(ctx context.Context, namespace string, kind string, name string, uid string, limit int) ([]EventItem, error) {
	fieldSelector := fields.Set{
		"involvedObject.kind": kind,
		"involvedObject.name": name,
	}.AsSelector().String()

	events, err := s.clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fieldSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("list events for %s %q in namespace %q: %w", kind, name, namespace, err)
	}

	sort.Slice(events.Items, func(i, j int) bool {
		return eventTimestamp(events.Items[i]).After(eventTimestamp(events.Items[j]))
	})

	result := make([]EventItem, 0, min(limit, len(events.Items)))
	for _, event := range events.Items {
		if uid != "" && string(event.InvolvedObject.UID) != "" && string(event.InvolvedObject.UID) != uid {
			continue
		}
		result = append(result, EventItem{
			Time:    eventTimestamp(event).Local().Format(time.RFC3339),
			Type:    dashIfEmpty(event.Type),
			Reason:  dashIfEmpty(event.Reason),
			Message: dashIfEmpty(event.Message),
		})
		if len(result) >= limit {
			break
		}
	}

	if len(result) == 0 {
		return []EventItem{{Time: "-", Type: "-", Reason: "-", Message: "no events"}}, nil
	}

	return result, nil
}

func (s *JobService) tailPodLogs(ctx context.Context, namespace string, podName string, tailLines int64) ([]string, error) {
	stream, err := s.clientset.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		TailLines: &tailLines,
	}).Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("stream logs for pod %q in namespace %q: %w", podName, namespace, err)
	}
	defer stream.Close()

	lines := make([]string, 0)
	reader := bufio.NewScanner(stream)
	for reader.Scan() {
		lines = append(lines, reader.Text())
	}
	if err := reader.Err(); err != nil && err != io.EOF {
		return nil, fmt.Errorf("read logs for pod %q in namespace %q: %w", podName, namespace, err)
	}
	if len(lines) == 0 {
		return []string{"<empty log output>"}, nil
	}
	return lines, nil
}

func chooseInspectPod(pods []corev1.Pod) *corev1.Pod {
	if len(pods) == 0 {
		return nil
	}

	sort.Slice(pods, func(i, j int) bool {
		return podInspectLess(pods[i], pods[j])
	})
	return &pods[0]
}

func chooseMasterLogPod(pods []corev1.Pod) *corev1.Pod {
	masters := make([]corev1.Pod, 0, 1)
	for _, pod := range pods {
		taskSpec := firstNonEmpty(pod.Labels["volcano.sh/task-spec"], pod.Annotations["volcano.sh/task-spec"])
		if strings.EqualFold(strings.TrimSpace(taskSpec), "master") || strings.Contains(strings.ToLower(pod.Name), "-master-") {
			masters = append(masters, pod)
		}
	}
	if len(masters) > 0 {
		return chooseInspectPod(masters)
	}
	return chooseInspectPod(append([]corev1.Pod(nil), pods...))
}

func podInspectLess(left corev1.Pod, right corev1.Pod) bool {
	leftScore := podInspectScore(left)
	rightScore := podInspectScore(right)
	if leftScore != rightScore {
		return leftScore < rightScore
	}
	return left.Name < right.Name
}

func podInspectScore(pod corev1.Pod) int {
	taskSpec := firstNonEmpty(pod.Labels["volcano.sh/task-spec"], pod.Annotations["volcano.sh/task-spec"])
	taskIndex := firstNonEmpty(pod.Labels["volcano.sh/task-index"], pod.Annotations["volcano.sh/task-index"])

	switch {
	case strings.EqualFold(taskSpec, "master"), strings.EqualFold(taskSpec, "chief"), strings.EqualFold(taskSpec, "launcher"):
		return 0
	case taskIndex == "0":
		return 1
	case strings.Contains(strings.ToLower(pod.Name), "master"):
		return 2
	default:
		return 3
	}
}

func jobPodLess(left JobPodItem, right JobPodItem) bool {
	if left.TaskSpec != right.TaskSpec {
		return left.TaskSpec < right.TaskSpec
	}
	if left.TaskIndex != right.TaskIndex {
		return left.TaskIndex < right.TaskIndex
	}
	return left.Name < right.Name
}

func hasOwnerUID(owners []metav1.OwnerReference, uid string) bool {
	for _, owner := range owners {
		if string(owner.UID) == uid {
			return true
		}
	}
	return false
}

func podMatchesIdentifier(pod corev1.Pod, identifier string) bool {
	id := strings.TrimSpace(identifier)
	if id == "" {
		return false
	}

	jobName := deriveJobName(pod)
	jobUID := deriveJobUID(pod)
	logicalNamespace := deriveLogicalNamespace(pod)

	switch {
	case pod.Name == id:
		return true
	case string(pod.UID) == id:
		return true
	case jobName == id:
		return true
	case jobUID == id:
		return true
	case strings.HasPrefix(pod.Name, id):
		return true
	case strings.HasPrefix(jobName, id):
		return true
	case strings.HasPrefix(jobUID, id):
		return true
	case logicalNamespace == id:
		return true
	default:
		return false
	}
}

func isSSPManagedWorkloadPod(pod corev1.Pod) bool {
	switch strings.ToLower(strings.TrimSpace(pod.Labels[sspWorkloadTypeLabel])) {
	case sspTrainingJobWorkloadType, sspAIDWorkloadTypeValue:
		return true
	default:
		return false
	}
}

func groupPodsByDerivedJob(pods []corev1.Pod) map[string][]corev1.Pod {
	grouped := make(map[string][]corev1.Pod)
	for _, pod := range pods {
		jobName := deriveJobName(pod)
		if jobName == "" {
			jobName = pod.Name
		}
		key := fmt.Sprintf("%s/%s", pod.Namespace, jobName)
		grouped[key] = append(grouped[key], pod)
	}
	return grouped
}

func deriveJobName(pod corev1.Pod) string {
	if value := strings.TrimSpace(pod.Labels["volcano.sh/job-name"]); value != "" {
		return value
	}
	if value := strings.TrimSpace(pod.Annotations["volcano.sh/job-name"]); value != "" {
		return value
	}

	name, _, _ := parseOwnerReferenceAnnotation(pod.Annotations["vcluster.loft.sh/owner-references"])
	return name
}

func deriveJobUID(pod corev1.Pod) string {
	_, uid, _ := parseOwnerReferenceAnnotation(pod.Annotations["vcluster.loft.sh/owner-references"])
	return uid
}

func deriveLogicalNamespace(pod corev1.Pod) string {
	return firstNonEmpty(
		pod.Annotations["vcluster.loft.sh/namespace"],
		pod.Labels["volcano.sh/job-namespace"],
		pod.Annotations["volcano.sh/job-namespace"],
		pod.Namespace,
	)
}

func derivePodGroupName(jobName string, jobUID string) string {
	jobName = strings.TrimSpace(jobName)
	jobUID = strings.TrimSpace(jobUID)
	if jobName == "" || jobUID == "" {
		return ""
	}
	return jobName + "-" + jobUID
}

func parseOwnerReferenceAnnotation(raw string) (string, string, string) {
	if strings.TrimSpace(raw) == "" {
		return "", "", ""
	}

	var refs []map[string]any
	if err := json.Unmarshal([]byte(raw), &refs); err != nil {
		return "", "", ""
	}
	for _, ref := range refs {
		kind := strings.TrimSpace(fmt.Sprintf("%v", ref["kind"]))
		if kind != "Job" {
			continue
		}
		return strings.TrimSpace(fmt.Sprintf("%v", ref["name"])), strings.TrimSpace(fmt.Sprintf("%v", ref["uid"])), kind
	}
	return "", "", ""
}

func jobIdentityFromPod(pod corev1.Pod) *jobIdentity {
	return &jobIdentity{
		Name:          deriveJobName(pod),
		Namespace:     deriveLogicalNamespace(pod),
		UID:           deriveJobUID(pod),
		Submitter:     firstNonEmpty(pod.Labels["lepton.sensetime.com/submitter"], pod.Annotations["lepton.sensetime.com/submitter"], "-"),
		PodGroupName:  firstNonEmpty(pod.Annotations["scheduling.k8s.io/group-name"], pod.Labels["scheduling.k8s.io/group-name"]),
		VClusterName:  firstNonEmpty(pod.Annotations["vcluster.loft.sh/vcluster-name"], pod.Labels["vcluster.loft.sh/vcluster-name"]),
		HostNamespace: pod.Namespace,
	}
}

func formatEventItems(events []corev1.Event, limit int) []EventItem {
	if len(events) == 0 {
		return []EventItem{{Time: "-", Type: "-", Reason: "-", Message: "no events"}}
	}

	sort.Slice(events, func(i, j int) bool {
		return eventTimestamp(events[i]).After(eventTimestamp(events[j]))
	})

	items := make([]EventItem, 0, min(limit, len(events)))
	for _, event := range events {
		items = append(items, EventItem{
			Time:    eventTimestamp(event).Local().Format(time.RFC3339),
			Type:    dashIfEmpty(event.Type),
			Reason:  dashIfEmpty(event.Reason),
			Message: dashIfEmpty(event.Message),
		})
		if len(items) >= limit {
			break
		}
	}
	return items
}

func extractPodGroupStatus(obj *unstructured.Unstructured) (string, []string) {
	return firstNonEmpty(
			getNestedText(obj.Object, "status", "phase"),
			getNestedText(obj.Object, "status", "state"),
			"-",
		),
		extractPodGroupMessages(obj)
}

func extractPodGroupPhase(obj *unstructured.Unstructured) string {
	return firstNonEmpty(
		getNestedText(obj.Object, "status", "phase"),
		getNestedText(obj.Object, "status", "state"),
		"-",
	)
}

func extractPodGroupMessages(obj *unstructured.Unstructured) []string {
	conditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil || !found || len(conditions) == 0 {
		return []string{"no podgroup status message"}
	}

	messages := make([]string, 0, len(conditions))
	seen := make(map[string]struct{})
	for i := len(conditions) - 1; i >= 0; i-- {
		condition, ok := conditions[i].(map[string]any)
		if !ok {
			continue
		}
		message := strings.TrimSpace(fmt.Sprintf("%v", condition["message"]))
		reason := strings.TrimSpace(fmt.Sprintf("%v", condition["reason"]))
		combined := joinMessageReason(message, reason)
		if combined == "" {
			continue
		}
		if _, exists := seen[combined]; exists {
			continue
		}
		seen[combined] = struct{}{}
		messages = append(messages, combined)
	}
	if len(messages) == 0 {
		return []string{"no podgroup status message"}
	}
	return messages
}

func podHasRunnableLogs(pod corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodSucceeded
}

func isFailedPod(pod corev1.Pod) bool {
	if pod.Status.Phase == corev1.PodFailed {
		return true
	}
	for _, status := range pod.Status.InitContainerStatuses {
		if isFailedContainerStatus(status) {
			return true
		}
	}
	for _, status := range pod.Status.ContainerStatuses {
		if isFailedContainerStatus(status) {
			return true
		}
	}
	return false
}

func podFailureReason(pod corev1.Pod) string {
	if pod.Status.Phase == corev1.PodFailed {
		if strings.TrimSpace(pod.Status.Message) != "" {
			return pod.Status.Message
		}
		if strings.TrimSpace(pod.Status.Reason) != "" {
			return pod.Status.Reason
		}
	}
	for _, status := range pod.Status.InitContainerStatuses {
		if reason := containerFailureReason(status); reason != "" {
			return reason
		}
	}
	for _, status := range pod.Status.ContainerStatuses {
		if reason := containerFailureReason(status); reason != "" {
			return reason
		}
	}
	return ""
}

func isFailedContainerStatus(status corev1.ContainerStatus) bool {
	return containerFailureReason(status) != ""
}

func containerFailureReason(status corev1.ContainerStatus) string {
	if status.State.Terminated == nil {
		return ""
	}
	terminated := status.State.Terminated
	if terminated.ExitCode == 0 {
		return ""
	}
	return firstNonEmpty(
		strings.TrimSpace(terminated.Message),
		strings.TrimSpace(terminated.Reason),
		fmt.Sprintf("exit code %d", terminated.ExitCode),
	)
}

func isPodReady(pod corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

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

func shouldLookupJobByPlatform(identifier string) bool {
	id := strings.TrimSpace(identifier)
	if id == "" {
		return false
	}
	if looksLikeUUID(id) {
		return false
	}
	lowerID := strings.ToLower(id)
	if strings.Contains(lowerID, "-worker-") || strings.Contains(lowerID, "-master-") || strings.Contains(lowerID, "-launcher-") {
		return false
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
