package service

import (
	"strings"
	"time"
)

// Deduplicate before filtering and limiting, so repeated views/pages cannot
// occupy two result slots or retain an outdated state over a newer snapshot.
func deduplicateAIRJobs(items []SSPAIRJobItem) []SSPAIRJobItem {
	result := make([]SSPAIRJobItem, 0, len(items))
	seen := make(map[string]int, len(items))
	for _, item := range items {
		key := airJobDedupKey(item)
		if key != "" {
			if index, exists := seen[key]; exists {
				if airJobRevisionTime(item).After(airJobRevisionTime(result[index])) {
					result[index] = item
				}
				continue
			}
			seen[key] = len(result)
		}
		result = append(result, item)
	}
	return result
}

func airJobDedupKey(item SSPAIRJobItem) string {
	if uid := strings.TrimSpace(item.UID); uid != "" {
		return "uid\x00" + uid
	}
	if id := strings.TrimSpace(item.Raw.ID); id != "" {
		return "rid\x00" + item.Region + "\x00" + id
	}
	if strings.TrimSpace(item.Name) == "" {
		return ""
	}
	return strings.Join([]string{"scope", item.Region, firstNonEmpty(item.Raw.SubscriptionName, item.Raw.Ownership.TenantID), item.Workspace, item.Name}, "\x00")
}

func airJobRevisionTime(item SSPAIRJobItem) time.Time {
	for _, value := range []string{item.Raw.Status.UpdateTime, item.UpdatedAt, item.Raw.Status.CreateTime, item.CreatedAt} {
		if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return parsed
		}
		if parsed, err := time.ParseInLocation("2006-01-02 15:04:05", value, utcPlus8); err == nil {
			return parsed
		}
	}
	return time.Time{}
}
