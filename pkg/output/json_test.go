package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"rayctl/internal/podevidence"
	"rayctl/internal/service"
)

func TestJSONKeepsLongValuesAndRedactsCredentials(t *testing.T) {
	name := strings.Repeat("long-image/", 40)
	warnings := []string{}
	value := publicJSON(reflect.ValueOf(struct {
		UID       string
		Image     string
		Password  string
		Raw       map[string]any
		Message   string
		Missing   *string
		CreatedAt time.Time
	}{
		UID: "unchanged-uid", Image: name, Password: "secret-value", Raw: map[string]any{"credential": "raw-secret"}, Message: "registry password: hidden", CreatedAt: time.Date(2026, 9, 4, 1, 2, 3, 0, time.UTC),
	}), "", &warnings)
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, secret := range []string{"secret-value", "raw-secret", "hidden"} {
		if strings.Contains(text, secret) {
			t.Fatalf("credential escaped: %s", secret)
		}
	}
	if !strings.Contains(text, name) || !strings.Contains(text, "2026-09-04T09:02:03+08:00") || !strings.Contains(text, `"missing":null`) || len(warnings) == 0 {
		t.Fatal(text, warnings)
	}
}

func TestJSONManyIsOneDocumentAndReportsPartialFailure(t *testing.T) {
	session := BeginJSON()
	defer EndJSON()
	PrintSSPAIDDetail(&service.SSPAIDGetResult{Name: "a"}, false, false)
	PrintSSPAIDDetail(&service.SSPAIDGetResult{Name: "b"}, false, false)
	var buffer bytes.Buffer
	if err := session.Write(&buffer, true, 3, errors.New("missing c")); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Items   []map[string]any
		Summary struct {
			Returned int
			Complete bool
		}
		Errors []string
	}
	if err := json.Unmarshal(buffer.Bytes(), &payload); err != nil {
		t.Fatal(err, buffer.String())
	}
	if len(payload.Items) != 2 || payload.Summary.Returned != 2 || payload.Summary.Complete || len(payload.Errors) != 1 {
		t.Fatal(buffer.String())
	}
}

func TestJSONListPreservesLimitMetadata(t *testing.T) {
	session := BeginJSON()
	defer EndJSON()
	PrintNodeList([]service.NodeListItem{{Name: "a"}}, "x=y", 12, 1, 1, 1, true, false)
	var buffer bytes.Buffer
	if err := session.Write(&buffer, true, 0, nil); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(buffer.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload["items"].([]any)) != 1 || payload["summary"].(map[string]any)["total"] != float64(12) {
		t.Fatal(buffer.String())
	}
}

func TestJSONResourceUnits(t *testing.T) {
	value, ok := resourceJSON(".cpu", "500m")
	if !ok || value.(map[string]any)["unit"] != "millicores" || value.(map[string]any)["value"] != 500.0 {
		t.Fatal(value)
	}
	value, ok = resourceJSON(".memory_usage", "8Gi/16Gi")
	if !ok || value.(map[string]any)["allocated"].(map[string]any)["value"] != 8.0 {
		t.Fatal(value)
	}
}

func TestJSONPodEvidenceUsesTopLevelPodsWithoutDuplicatingLogs(t *testing.T) {
	session := BeginJSON()
	defer EndJSON()
	PrintSSPAIDDetail(&service.SSPAIDGetResult{Name: "a", PodEvidence: &podevidence.Result{Pods: []podevidence.Pod{{Name: "p", Containers: []podevidence.Container{{Name: "main", Logs: podevidence.Logs{Current: []string{"unique-log-line"}}}}}}, Total: 1}}, true, false)
	var buffer bytes.Buffer
	if err := session.Write(&buffer, false, 1, nil); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(buffer.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload["pods"].([]any)) != 1 || strings.Count(buffer.String(), "unique-log-line") != 1 {
		t.Fatal(buffer.String())
	}
}
