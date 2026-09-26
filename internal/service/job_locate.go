package service

import (
	"context"
	"errors"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"rayctl/internal/platform"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
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
