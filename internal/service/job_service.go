package service

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
