package output

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// A command renders sequentially after its concurrent queries have completed.
// The session collects result objects before any table formatting takes place.
type JSONSession struct {
	Items    []any
	Warnings []string
}

var jsonSession *JSONSession

func BeginJSON() *JSONSession {
	jsonSession = &JSONSession{Items: []any{}, Warnings: []string{}}
	return jsonSession
}

func EndJSON()     { jsonSession = nil }
func IsJSON() bool { return jsonSession != nil }

func captureJSON(value any) bool {
	if jsonSession == nil {
		return false
	}
	v := reflect.ValueOf(value)
	for v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) {
		if v.IsNil() {
			return true
		}
		v = v.Elem()
	}
	if v.IsValid() && (v.Kind() == reflect.Slice || v.Kind() == reflect.Array) {
		for i := 0; i < v.Len(); i++ {
			captureJSON(v.Index(i).Interface())
		}
		return true
	}
	warnings := []string{}
	object := publicJSON(reflect.ValueOf(value), "", &warnings)
	if item, ok := object.(map[string]any); ok {
		if old, ok := item["warnings"].([]any); ok {
			for _, w := range old {
				warnings = append(warnings, fmt.Sprint(w))
			}
		}
		item["warnings"] = warnings
	}
	jsonSession.Items = append(jsonSession.Items, object)
	return true
}

func (s *JSONSession) Write(w io.Writer, list bool, requested int, queryErr error) error {
	errors := []string{}
	if queryErr != nil {
		errors = append(errors, redactText(queryErr.Error()))
	}
	items := []any{}
	metadata := []any{}
	var total any
	for _, value := range s.Items {
		if object, ok := value.(map[string]any); ok && list {
			if children, exists := object["items"].([]any); exists {
				if value, ok := object["total"]; ok {
					total = value
				}
				for _, child := range children {
					if item, ok := child.(map[string]any); ok && len(s.Items) > 1 {
						context := map[string]any{}
						for _, key := range []string{"queue", "cluster_name", "cluster_uid", "profile_name", "workspace"} {
							if value, ok := object[key]; ok {
								context[key] = value
							}
						}
						if len(context) > 0 {
							item["context"] = context
						}
					}
					items = append(items, child)
				}
				delete(object, "items")
				metadata = append(metadata, object)
				continue
			}
		}
		items = append(items, value)
	}
	var result any
	if !list && requested <= 1 && len(items) == 1 {
		if object, ok := items[0].(map[string]any); ok {
			object["errors"] = errors
			result = object
		}
	}
	if result == nil {
		result = map[string]any{"items": items, "summary": map[string]any{"returned": len(items), "total": total, "requested": requested, "complete": queryErr == nil}, "metadata": metadata, "warnings": s.Warnings, "errors": errors}
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(result)
}

var credentialText = regexp.MustCompile(`(?i)((?:password|secret_key|access_key|token|密码)\s*[:=]\s*)[^\s|,]+`)

var bearerText = regexp.MustCompile(`(?i)(Bearer|Basic)\s+[A-Za-z0-9._~+/=-]+`)

func redactText(value string) string {
	value = credentialText.ReplaceAllString(value, "${1}***redacted***")
	value = bearerText.ReplaceAllString(value, "${1} ***redacted***")
	if u, err := url.Parse(value); err == nil && u.User != nil {
		u.User = url.User("***redacted***")
		value = u.String()
	}
	return value
}

func jsonKey(name string) string {
	runes := []rune(name)
	var out strings.Builder
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) && (unicode.IsLower(runes[i-1]) || (i+1 < len(runes) && unicode.IsLower(runes[i+1]))) {
			out.WriteByte('_')
		}
		out.WriteRune(unicode.ToLower(r))
	}
	return out.String()
}

func secretKey(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(key, "_", ""))
	return strings.Contains(key, "password") || strings.Contains(key, "secretkey") || strings.Contains(key, "accesskey") || strings.Contains(key, "token") || key == "authorization" || key == "dockerconfigjson" || key == ".dockerconfigjson"
}

// Build a public projection, excluding internal platform payloads and credentials.
// Display placeholders never escape into machine-readable values.
func publicJSON(v reflect.Value, path string, warnings *[]string) any {
	if !v.IsValid() {
		return nil
	}
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			*warnings = append(*warnings, strings.TrimPrefix(path, ".")+": unavailable, inapplicable or not collected")
			return nil
		}
		v = v.Elem()
	}
	if v.Type() == reflect.TypeOf(time.Time{}) {
		t := v.Interface().(time.Time)
		if t.IsZero() {
			return nil
		}
		return t.In(time.FixedZone("UTC+8", 8*3600)).Format(time.RFC3339Nano)
	}
	if v.Type() == reflect.TypeOf(time.Duration(0)) {
		return map[string]any{"value": v.Int(), "unit": "ns"}
	}
	switch v.Kind() {
	case reflect.Struct:
		out := map[string]any{}
		for i := 0; i < v.NumField(); i++ {
			field := v.Type().Field(i)
			if field.PkgPath != "" || field.Name == "Raw" {
				continue
			}
			key := jsonKey(field.Name)
			if tag := strings.Split(field.Tag.Get("json"), ",")[0]; tag != "" {
				if tag == "-" {
					continue
				}
				key = tag
			}
			if secretKey(key) {
				out[key] = "***redacted***"
				continue
			}
			out[key] = publicJSON(v.Field(i), path+"."+key, warnings)
		}
		if evidence, exists := out["pod_evidence"]; exists {
			if previous, ok := out["pods"]; ok {
				out["pod_summary"] = previous
			}
			out["pods"] = nil
			if details, ok := evidence.(map[string]any); ok {
				if pods, ok := details["pods"].([]any); ok && len(pods) > 0 {
					out["pods"] = pods
				}
				delete(details, "pods")
			}
		}
		return out
	case reflect.Map:
		out := map[string]any{}
		iter := v.MapRange()
		for iter.Next() {
			key := fmt.Sprint(iter.Key().Interface())
			if secretKey(key) {
				out[key] = "***redacted***"
			} else {
				out[key] = publicJSON(iter.Value(), path+"."+key, warnings)
			}
		}
		return out
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() && (strings.HasSuffix(path, ".logs.current") || strings.HasSuffix(path, ".logs.previous")) {
			return nil
		}
		out := make([]any, 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			out = append(out, publicJSON(v.Index(i), fmt.Sprintf("%s[%d]", path, i), warnings))
		}
		return out
	case reflect.String:
		value := v.String()
		if value == "" || value == "-" {
			*warnings = append(*warnings, strings.TrimPrefix(path, ".")+": unavailable or not collected by this query")
			return nil
		}
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05"} {
			if t, err := time.ParseInLocation(layout, value, time.FixedZone("UTC+8", 8*3600)); err == nil {
				return t.In(time.FixedZone("UTC+8", 8*3600)).Format(time.RFC3339Nano)
			}
		}
		if quantity, ok := resourceJSON(path, value); ok {
			if object, ok := quantity.(map[string]any); ok {
				if unit, exists := object["unit"]; exists && unit == nil {
					*warnings = append(*warnings, strings.TrimPrefix(path, ".")+": source quantity did not declare a unit; raw value preserved")
				}
			}
			return quantity
		}
		return redactText(value)
	case reflect.Bool:
		return v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint()
	case reflect.Float32, reflect.Float64:
		return v.Float()
	}
	return nil
}

var quantityPattern = regexp.MustCompile(`^([+-]?[0-9]+(?:\.[0-9]+)?)([a-zA-Z]*)$`)

// Preserve the source quantity and its explicit unit without rescaling it.
func resourceJSON(path string, raw string) (any, bool) {
	key := path[strings.LastIndex(path, ".")+1:]
	unit := ""
	switch key {
	case "cpu", "cpu_usage":
		unit = "cores"
	case "memory", "memory_usage", "gpu_memory":
		unit = "source"
	case "accelerator", "device", "gpu_usage", "rdma", "rdma_allocated", "rdma_total", "capacity", "allocatable", "requested":
		unit = "count"
	default:
		return nil, false
	}
	parts := strings.Split(raw, "/")
	values := make([]any, 0, len(parts))
	for _, part := range parts {
		match := quantityPattern.FindStringSubmatch(strings.TrimSpace(part))
		if match == nil {
			return nil, false
		}
		number, err := strconv.ParseFloat(match[1], 64)
		if err != nil {
			return nil, false
		}
		suffix := unit
		if match[2] != "" {
			suffix = match[2]
			if suffix == "m" && unit == "cores" {
				suffix = "millicores"
			}
		}
		// A unitless legacy memory field has no reliable source unit.
		var declaredUnit any = suffix
		if suffix == "source" {
			declaredUnit = nil
		}
		values = append(values, map[string]any{"value": number, "unit": declaredUnit, "raw": part})
	}
	if len(values) == 1 {
		return values[0], true
	}
	if len(values) == 2 {
		return map[string]any{"allocated": values[0], "total": values[1], "raw": raw}, true
	}
	return nil, false
}
