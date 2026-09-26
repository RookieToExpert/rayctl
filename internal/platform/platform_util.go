package platform

import (
	"fmt"
	"strings"
)

func virtualClusterUID(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "vc-") {
		return strings.TrimSpace(strings.TrimPrefix(value, "vc-"))
	}
	return value
}

func stringFromAny(value any) string {
	if value == nil {
		return ""
	}
	switch value.(type) {
	case map[string]any, []any:
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

func stringFromNested(item map[string]any, keys ...string) string {
	if len(item) == 0 {
		return ""
	}

	roots := []map[string]any{item}
	for _, key := range []string{"attributes", "resource", "fields", "labels", "dimensions", "data"} {
		if nested, ok := item[key].(map[string]any); ok {
			roots = append(roots, nested)
		}
	}

	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		for _, root := range roots {
			if value := stringAtPath(root, key); value != "" {
				return value
			}
		}
		for _, root := range roots {
			if value := findNestedString(root, key); value != "" {
				return value
			}
		}
	}
	return ""
}

func stringAtPath(root map[string]any, path string) string {
	parts := strings.Split(path, ".")
	var current any = root
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return ""
		}
		currentMap, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current, ok = currentMap[part]
		if !ok {
			return ""
		}
	}
	return stringFromAny(current)
}

func findNestedString(root map[string]any, key string) string {
	if value := stringFromAny(root[key]); value != "" {
		return value
	}
	for _, value := range root {
		nested, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if found := findNestedString(nested, key); found != "" {
			return found
		}
	}
	return ""
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
