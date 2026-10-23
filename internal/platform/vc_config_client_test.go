package platform

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const configTestUID = "019d4c85-01c5-7963-b7f7-15f849a934f3"

func TestVCConfigClientReadsHCOperationAPI(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Method != http.MethodGet {
			t.Errorf("unexpected mutation: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case vcOperationProxy + "/virtualClusterConfig":
			fmt.Fprint(w, `{"available_control_plane_flavors":[{"name":"small","replicas":1,"max_node_count":30}]}`)
		case vcOperationProxy + "/virtualClusters/" + configTestUID:
			fmt.Fprintf(w, `{"uid":%q,"name":"vc-demo","control_plane_flavor":{"name":"medium","replicas":3,"max_node_count":150}}`, configTestUID)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	kube, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	client := NewVCConfigClient(kube.CoreV1().RESTClient())
	flavors, err := client.Flavors(context.Background())
	if err != nil || len(flavors) != 1 || flavors[0].MaxNodeCount != 30 {
		t.Fatalf("flavors=%+v err=%v", flavors, err)
	}
	object, err := client.Get(context.Background(), configTestUID)
	if err != nil || object.ControlPlaneFlavor.Name != "medium" || len(paths) != 2 {
		t.Fatalf("object=%+v paths=%v err=%v", object, paths, err)
	}
	if _, err := client.Get(context.Background(), "../../unsafe"); err == nil || len(paths) != 2 {
		t.Fatalf("invalid UID reached server: %v %v", paths, err)
	}
}

func TestVCConfigClientPreservesPermissionAndIdentityErrors(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusOK} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				fmt.Fprint(w, `{"uid":"different","message":"forbidden"}`)
			}))
			defer server.Close()
			kube, _ := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
			if object, err := NewVCConfigClient(kube.CoreV1().RESTClient()).Get(context.Background(), configTestUID); err == nil || object != nil {
				t.Fatalf("object=%+v err=%v", object, err)
			}
		})
	}
}
