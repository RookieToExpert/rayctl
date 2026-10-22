package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// An empty platform index is not proof that a running VCJob has no Pods. Read
// only its mapped HC namespace and verify the Job UID before using a mirror.
func (s *JobService) recoverATEJobPods(ctx context.Context, identity *jobIdentity, indexErr error) ([]corev1.Pod, []string, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	warnings := []string{}
	if indexErr != nil {
		warnings = append(warnings, "VC Pod 索引查询失败: "+indexErr.Error())
	}
	if s.clientset == nil {
		return nil, append(warnings, "HC client unavailable for Pod lookup"), indexErr
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if identity.HostNamespace == "" {
		ref := firstNonEmpty(identity.VClusterUID, identity.VClusterName)
		identity.HostNamespace = s.findVClusterHostNamespace(lookupCtx, strings.TrimPrefix(ref, "vc-"), identity.Namespace)
	}
	if identity.HostNamespace == "" {
		if err := ctx.Err(); err != nil {
			return nil, warnings, err
		}
		return nil, append(warnings, "当前 HC kubeconfig 看不到该 VC 的 namespace 映射，无法核对 Pod；请检查 -k/--kubeconfig"), indexErr
	}
	for _, selector := range []string{"volcano.sh/job-name=" + identity.Name, ""} {
		options := metav1.ListOptions{LabelSelector: selector, Limit: 500}
		seenTokens := map[string]bool{}
		matched := []corev1.Pod{}
		for {
			page, err := s.clientset.CoreV1().Pods(identity.HostNamespace).List(lookupCtx, options)
			if err != nil {
				if ctx.Err() != nil {
					return nil, warnings, ctx.Err()
				}
				return nil, append(warnings, "HC Pod lookup: "+err.Error()), indexErr
			}
			for _, host := range page.Items {
				if !ateHostPodBelongsToJob(host, *identity) {
					continue
				}
				pod := host.DeepCopy()
				if pod.Annotations == nil {
					pod.Annotations = map[string]string{}
				}
				pod.Annotations["vcluster.loft.sh/object-host-name"] = host.Name
				pod.Annotations["vcluster.loft.sh/object-host-namespace"] = host.Namespace
				pod.Name = firstNonEmpty(host.Annotations["vcluster.loft.sh/object-name"], host.Annotations["vcluster.loft.sh/name"], host.Name)
				pod.Namespace = identity.Namespace
				pod.UID = types.UID(firstNonEmpty(host.Annotations["vcluster.loft.sh/object-uid"], host.Annotations["vcluster.loft.sh/uid"], string(host.UID)))
				matched = append(matched, *pod)
			}
			next := page.Continue
			if next == "" {
				break
			}
			if seenTokens[next] {
				return nil, append(warnings, "HC Pod 分页游标重复，无法确认结果完整"), indexErr
			}
			seenTokens[next] = true
			options.Continue = next
		}
		if len(matched) > 0 {
			sort.Slice(matched, func(i, j int) bool { return podInspectLess(matched[i], matched[j]) })
			warnings = append(warnings, fmt.Sprintf("VC Pod 查询未返回可用结果，已从 HC %s 按 Job UID 恢复 %d 个 Pod", identity.HostNamespace, len(matched)))
			return matched, warnings, nil
		}
	}
	return nil, append(warnings, "VC Pod 索引为空，HC 中也未找到属于该 Job UID 的 Pod"), indexErr
}

func ateHostPodBelongsToJob(pod corev1.Pod, identity jobIdentity) bool {
	logicalNS := firstNonEmpty(pod.Annotations["vcluster.loft.sh/object-namespace"], pod.Annotations["vcluster.loft.sh/namespace"], pod.Labels["volcano.sh/job-namespace"])
	if pod.Namespace != identity.HostNamespace || logicalNS != "" && logicalNS != identity.Namespace {
		return false
	}
	vc := firstNonEmpty(pod.Annotations["vcluster.loft.sh/vcluster-name"], pod.Labels["vcluster.loft.sh/vcluster-name"])
	expectedVC := firstNonEmpty(identity.VClusterUID, identity.VClusterName)
	if vc != "" && expectedVC != "" && strings.TrimPrefix(vc, "vc-") != strings.TrimPrefix(expectedVC, "vc-") {
		return false
	}
	name, uid, _ := parseOwnerReferenceAnnotation(pod.Annotations["vcluster.loft.sh/owner-references"])
	if uid == "" {
		for _, owner := range pod.OwnerReferences {
			if owner.Kind == "Job" {
				name, uid = owner.Name, string(owner.UID)
				break
			}
		}
	}
	// Names can be reused after a Job is recreated; a label alone is insufficient.
	return identity.UID != "" && uid == identity.UID && name == identity.Name
}
