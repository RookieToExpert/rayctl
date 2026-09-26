package service

import (
	"context"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sort"
	"strings"
	"time"
)

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

func derivePodGroupName(jobName string, jobUID string) string {
	jobName = strings.TrimSpace(jobName)
	jobUID = strings.TrimSpace(jobUID)
	if jobName == "" || jobUID == "" {
		return ""
	}
	return jobName + "-" + jobUID
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
