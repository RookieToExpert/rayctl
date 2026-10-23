package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	nodeUsageSourceUID    = "019a0000-0000-7000-8000-000000000001"
	nodeUsageRecreatedUID = "019a0000-0000-7000-8000-000000000002"
	nodeUsageClusterUID   = "019a0000-0000-7000-8000-000000000003"
	nodeUsageProdLabel    = "node-role.compute.sensecore.cn/prod"
)

type nodeUsageHTTPFixture struct {
	mu              sync.Mutex
	nodeSelectors   []string
	clusterRequests int
}

func configureNodeUsageHTTPTest(t *testing.T, nodes []corev1.Node, failPlatform bool) *nodeUsageHTTPFixture {
	t.Helper()
	fixture := &nodeUsageHTTPFixture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/nodes":
			selectorText := r.URL.Query().Get("labelSelector")
			fixture.mu.Lock()
			fixture.nodeSelectors = append(fixture.nodeSelectors, selectorText)
			fixture.mu.Unlock()
			selector, err := labels.Parse(selectorText)
			if err != nil {
				t.Errorf("invalid node selector %q: %v", selectorText, err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			result := corev1.NodeList{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "NodeList"}}
			for _, node := range nodes {
				if selector.Matches(labels.Set(node.Labels)) {
					result.Items = append(result.Items, node)
				}
			}
			_ = json.NewEncoder(w).Encode(result)
		case "/compute/ssp/v1/subscriptions/-/resourceGroups/-/regions/-/clusters":
			fixture.mu.Lock()
			fixture.clusterRequests++
			fixture.mu.Unlock()
			if failPlatform {
				http.Error(w, "SSP inventory unavailable", http.StatusServiceUnavailable)
				return
			}
			fmt.Fprintf(w, `{"clusters":[{"uid":%q,"name":"ssp-primary","state":"RUNNING","properties":{"source":{"uid":%q,"name":"source-vc"}}}],"total_size":1}`, nodeUsageClusterUID, nodeUsageSourceUID)
		case "/compute/ecp/v1/subscriptions/-/resourceGroups/-/regions/-/virtualClusters":
			fmt.Fprintf(w, `{"virtual_clusters":[{"uid":%q,"name":"source-vc"},{"uid":%q,"name":"recreated-vc"}]}`, nodeUsageSourceUID, nodeUsageRecreatedUID)
		default:
			t.Errorf("unexpected node usage HTTP request: %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "platform.json")
	config := fmt.Sprintf(`{"current_profile":"test","profiles":{"test":{"access_key":"ak","secret_key":"sk","subscription_id":"sub","region":"cn-pj-03","base_url":%q,"kubernetes_base_url":%q}}}`, server.URL, server.URL)
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "kubeconfig")
	config = fmt.Sprintf("apiVersion: v1\nkind: Config\nclusters:\n- name: local\n  cluster:\n    server: %s\ncontexts:\n- name: local\n  context:\n    cluster: local\n    user: local\ncurrent-context: local\nusers:\n- name: local\n  user: {}\n", server.URL)
	if err := os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RAYCTL_PLATFORM_CONFIG", configPath)
	t.Setenv("RAYCTL_PLATFORM_ENVIRONMENT", "")
	oldKubeconfig, oldEnvironment := kubeconfig, targetEnvironment
	kubeconfig, targetEnvironment = path, "pt"
	t.Cleanup(func() { kubeconfig, targetEnvironment = oldKubeconfig, oldEnvironment })
	return fixture
}

func nodeUsageTestNode(name, role, vcUID string) corev1.Node {
	values := map[string]string{nodeUsageProdLabel: role}
	if vcUID != "" {
		values["resource.compute.sensecore.cn/vc-uid"] = vcUID
		// The recreated VC deliberately shares its old name. Its different UID
		// must prevent inheriting the old SSP cluster association.
		values["cluster.x-k8s.io/vcluster-name"] = "source-vc"
	}
	return corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: values}}
}

type nodeUsageCommandJSON struct {
	Items []struct {
		Name           string `json:"name"`
		Usage          string `json:"usage"`
		ProdRole       string `json:"prod_role"`
		ClusterUID     string `json:"cluster_uid"`
		SSPClusterName string `json:"ssp_cluster_name"`
		SSPClusterUID  string `json:"ssp_cluster_uid"`
	} `json:"items"`
	Summary struct {
		Total int `json:"total"`
	} `json:"summary"`
	Metadata []struct {
		Selector string `json:"selector"`
	} `json:"metadata"`
}

func executeNodeUsageCommand(t *testing.T, args ...string) (nodeUsageCommandJSON, string, error) {
	t.Helper()
	command := newNodeListCmd()
	command.SilenceErrors, command.SilenceUsage = true, true
	command.Flags().StringP("output", "o", "table", "")
	installJSONCommands(command)
	var out, errOut bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&errOut)
	command.SetArgs(append(append([]string{}, args...), "-o", "json"))
	err := command.Execute()
	var payload nodeUsageCommandJSON
	if err == nil {
		if decodeErr := json.Unmarshal(out.Bytes(), &payload); decodeErr != nil {
			t.Fatalf("decode node list JSON: %v: %s", decodeErr, out.String())
		}
	}
	return payload, errOut.String(), err
}

func TestNodeListUsageCommandUsesSSPSourceVCAndPreservesRawSelector(t *testing.T) {
	fixture := configureNodeUsageHTTPTest(t, []corev1.Node{
		nodeUsageTestNode("ssp-a", "ecp-private", nodeUsageSourceUID),
		nodeUsageTestNode("ssp-b", "ecp-private", nodeUsageSourceUID),
		nodeUsageTestNode("ecp", "ecp-private", nodeUsageRecreatedUID),
		nodeUsageTestNode("ecs", "ecs", ""),
	}, false)
	for _, tc := range []struct {
		name, target, selector string
		wantNames              []string
	}{
		{name: "default", wantNames: []string{"ssp-a", "ssp-b", "ecp", "ecs"}},
		{name: "SSP alias", target: "ssp", wantNames: []string{"ssp-a", "ssp-b"}},
		{name: "ECP alias", target: "ecp", wantNames: []string{"ecp"}},
		{name: "ECS alias", target: "ecs", wantNames: []string{"ecs"}},
		{name: "raw selector", target: nodeUsageProdLabel + "=ecp-private", selector: nodeUsageProdLabel + "=ecp-private", wantNames: []string{"ssp-a", "ssp-b", "ecp"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var args []string
			if tc.target != "" {
				args = append(args, tc.target)
			}
			payload, warnings, err := executeNodeUsageCommand(t, args...)
			if err != nil || warnings != "" {
				t.Fatalf("node list error=%v warnings=%s", err, warnings)
			}
			if len(payload.Items) != len(tc.wantNames) || payload.Summary.Total != len(tc.wantNames) {
				t.Fatalf("items=%d total=%d, want %v", len(payload.Items), payload.Summary.Total, tc.wantNames)
			}
			for index, item := range payload.Items {
				if item.Name != tc.wantNames[index] {
					t.Fatalf("item %d = %s, want %s", index, item.Name, tc.wantNames[index])
				}
				switch item.Name {
				case "ssp-a", "ssp-b":
					if item.Usage != "SSP" || item.ProdRole != "ecp-private" || item.ClusterUID != nodeUsageSourceUID || item.SSPClusterName != "ssp-primary" || item.SSPClusterUID != nodeUsageClusterUID {
						t.Fatalf("SSP business usage or raw role missing: %+v", item)
					}
				case "ecp":
					if item.Usage != "ECP" || item.ProdRole != "ecp-private" || item.SSPClusterName != "" || item.SSPClusterUID != "" {
						t.Fatalf("recreated VC inherited stale SSP usage: %+v", item)
					}
				case "ecs":
					if item.Usage != "ECS" || item.ProdRole != "ecs" {
						t.Fatalf("ECS usage = %+v", item)
					}
				}
			}
			fixture.mu.Lock()
			selector := fixture.nodeSelectors[len(fixture.nodeSelectors)-1]
			clusterRequests := fixture.clusterRequests
			fixture.mu.Unlock()
			if selector != tc.selector || clusterRequests == 0 {
				t.Fatalf("Kubernetes selector=%q, want %q; SSP requests=%d", selector, tc.selector, clusterRequests)
			}
			wantDescription := tc.selector
			if tc.target == "ssp" || tc.target == "ecp" || tc.target == "ecs" {
				wantDescription = "usage=" + strings.ToUpper(tc.target)
			}
			if len(payload.Metadata) != 1 || payload.Metadata[0].Selector != wantDescription {
				t.Fatalf("selector metadata=%+v, want %q", payload.Metadata, wantDescription)
			}
		})
	}
}

func TestNodeListUsageCommandFiltersBeforeDefaultDisplayLimit(t *testing.T) {
	nodes := make([]corev1.Node, 0, 107)
	for index := range 100 {
		nodes = append(nodes, nodeUsageTestNode(fmt.Sprintf("ecp-%03d", index), "ecp-private", nodeUsageRecreatedUID))
	}
	for index := range 7 {
		nodes = append(nodes, nodeUsageTestNode(fmt.Sprintf("ssp-%03d", index), "ecp-private", nodeUsageSourceUID))
	}
	configureNodeUsageHTTPTest(t, nodes, false)
	for _, tc := range []struct {
		args       []string
		items, all int
	}{
		{items: 100, all: 107},
		{args: []string{"-A"}, items: 107, all: 107},
		{args: []string{"ssp"}, items: 7, all: 7},
	} {
		payload, warnings, err := executeNodeUsageCommand(t, tc.args...)
		if err != nil || warnings != "" || len(payload.Items) != tc.items || payload.Summary.Total != tc.all {
			t.Fatalf("args=%v items=%d total=%d error=%v warnings=%s", tc.args, len(payload.Items), payload.Summary.Total, err, warnings)
		}
		if len(tc.args) == 1 && tc.args[0] == "ssp" {
			for _, item := range payload.Items {
				if item.Usage != "SSP" || item.ClusterUID != nodeUsageSourceUID {
					t.Fatalf("wrong SSP source membership after pagination: %+v", item)
				}
			}
		}
	}
}

func TestNodeListUsageCommandReportsPlatformFailure(t *testing.T) {
	fixture := configureNodeUsageHTTPTest(t, []corev1.Node{
		nodeUsageTestNode("mapped-when-available", "ecp-private", nodeUsageSourceUID),
		nodeUsageTestNode("ecs", "ecs", ""),
	}, true)
	payload, warnings, err := executeNodeUsageCommand(t)
	if err != nil || len(payload.Items) != 2 || payload.Items[0].Usage != "ECP" || !strings.Contains(warnings, "SSP inventory unavailable") {
		t.Fatalf("default fallback items=%+v error=%v warnings=%s", payload.Items, err, warnings)
	}
	for _, target := range []string{"ecp", "ecs", "ssp"} {
		_, _, err := executeNodeUsageCommand(t, target)
		if err == nil || !strings.Contains(err.Error(), "resolve node usage") || !strings.Contains(err.Error(), "SSP inventory unavailable") {
			t.Fatalf("%s alias concealed failed SSP inventory: %v", target, err)
		}
	}
	fixture.mu.Lock()
	requests := fixture.clusterRequests
	fixture.mu.Unlock()
	if requests < 4 {
		t.Fatalf("SSP cluster requests=%d, want at least one per command (4)", requests)
	}
}
