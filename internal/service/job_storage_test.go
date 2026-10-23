package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const pendingAFSUID = "019ae8de-e634-7edd-b189-801c6339d110"

func pendingAFSTestPVC() *corev1.PersistentVolumeClaim {
	storageClass := "quark-vcproxy-sc"
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pvc-9r8w4",
			Namespace: "vcluster-test",
			Annotations: map[string]string{
				"afs.endpoint":   "csi://" + pendingAFSUID,
				"afs.secretName": "afs1-secret",
			},
		},
		Spec:   corev1.PersistentVolumeClaimSpec{StorageClassName: &storageClass},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending},
	}
}

func TestResolveVolumeClaimRefPendingAFSFromPVC(t *testing.T) {
	for _, virtual := range []bool{true, false} {
		name := "host PVC"
		if virtual {
			name = "virtual PVC"
		}
		t.Run(name, func(t *testing.T) {
			pvc := pendingAFSTestPVC()
			var resourceQueries, secretQueries atomic.Int32
			platformClient := workloadTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.Contains(r.URL.Path, "/secrets/"):
					secretQueries.Add(1)
					http.Error(w, "AFS name lookup must not read credentials", http.StatusForbidden)
				case strings.HasSuffix(r.URL.Path, "/persistentvolumeclaims/"+pvc.Name):
					if err := json.NewEncoder(w).Encode(pvc); err != nil {
						t.Error(err)
					}
				case r.URL.Path == "/rmh/v1/resources:page":
					resourceQueries.Add(1)
					filter := r.URL.Query().Get("filter")
					if !strings.Contains(filter, pendingAFSUID) || !strings.Contains(filter, "storage.afs.") {
						t.Errorf("AFS resource filter = %q", filter)
					}
					fmt.Fprintf(w, `{"resources":[{"id":%q,"name":"afs-astro-results","display_name":"Astro results","resource_type":"storage.afs.v2.volume","rid":"/subscriptions/sub/resourceGroups/default/regions/cn-pj-01/virtualVolumes/afs-astro-results"}]}`, pendingAFSUID)
				default:
					t.Errorf("unexpected platform request %s", r.URL.Path)
					http.NotFound(w, r)
				}
			})
			clientset := fake.NewSimpleClientset(pvc)
			service := &JobService{clientset: clientset, vcClient: platformClient}
			identity := &jobIdentity{HostNamespace: pvc.Namespace}
			if virtual {
				identity.VClusterName = "vc-test"
				identity.Namespace = "default"
				identity.ProfileName = "test"
			}
			refs := service.resolveVolumeClaimRefs(context.Background(), identity, []VolumeClaimRef{{ClaimName: pvc.Name}})
			if len(refs) != 1 || refs[0].FrontendVolume != "afs-astro-results" || refs[0].Status != "Pending" {
				t.Fatalf("resolved PVC = %#v, want named AFS with Pending status", refs)
			}
			if refs[0].PVName != "" || refs[0].BackendPV != "" || refs[0].DisplayPV != "" {
				t.Fatalf("Pending PVC gained a PV binding: %#v", refs[0])
			}
			if resourceQueries.Load() != 1 || secretQueries.Load() != 0 {
				t.Fatalf("resource queries = %d, secret queries = %d", resourceQueries.Load(), secretQueries.Load())
			}
			assertNoPVCSecretReads(t, clientset)
		})
	}
}

func TestResolveVolumeClaimRefAFSLookupFailureDoesNotReadCredentials(t *testing.T) {
	for _, virtual := range []bool{true, false} {
		name := "host PVC"
		if virtual {
			name = "virtual PVC"
		}
		t.Run(name, func(t *testing.T) {
			pvc := pendingAFSTestPVC()
			// Both annotation aliases must remain AFS credentials even if RMH lookup fails.
			pvc.Annotations["secretName"] = "afs1-secret"
			var secretQueries atomic.Int32
			platformClient := workloadTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.Contains(r.URL.Path, "/secrets/"):
					secretQueries.Add(1)
					fmt.Fprint(w, `{"data":{"endpoint":"aHR0cHM6Ly9ub3QtYW9zcy5leGFtcGxlLnRlc3Q="}}`)
				case strings.HasSuffix(r.URL.Path, "/persistentvolumeclaims/"+pvc.Name):
					if err := json.NewEncoder(w).Encode(pvc); err != nil {
						t.Error(err)
					}
				case r.URL.Path == "/rmh/v1/resources:page":
					fmt.Fprint(w, `{"resources":[]}`)
				default:
					t.Errorf("unexpected platform request %s", r.URL.Path)
					http.NotFound(w, r)
				}
			})
			clientset := fake.NewSimpleClientset(pvc, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "afs1-secret", Namespace: pvc.Namespace},
				Data:       map[string][]byte{"endpoint": []byte("https://not-aoss.example.test")},
			})
			service := &JobService{clientset: clientset, vcClient: platformClient}
			identity := &jobIdentity{HostNamespace: pvc.Namespace}
			if virtual {
				identity.VClusterName = "vc-test"
				identity.Namespace = "default"
				identity.ProfileName = "test"
			}
			refs := service.resolveVolumeClaimRefs(context.Background(), identity, []VolumeClaimRef{{ClaimName: pvc.Name}})
			if len(refs) != 1 || refs[0].FrontendVolume != "" || refs[0].Status != "Pending" {
				t.Fatalf("unresolved AFS PVC = %#v, want no guessed endpoint and Pending status", refs)
			}
			if secretQueries.Load() != 0 {
				t.Fatalf("platform Secret reads = %d", secretQueries.Load())
			}
			assertNoPVCSecretReads(t, clientset)
		})
	}
}

func TestAFSResourceUIDFromPVCRejectsInvalidEndpoints(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		want     string
	}{
		{name: "valid CSI UID", endpoint: "csi://" + pendingAFSUID, want: pendingAFSUID},
		{name: "surrounding whitespace", endpoint: " csi://" + pendingAFSUID + " ", want: pendingAFSUID},
		{name: "absent"},
		{name: "bare UID", endpoint: pendingAFSUID},
		{name: "HTTP", endpoint: "https://" + pendingAFSUID},
		{name: "incomplete UID", endpoint: "csi://019ae8de-e634"},
		{name: "invalid UUID hex", endpoint: "csi://019ae8de-e634-7edd-b189-801c6339d11g"},
		{name: "UID followed by path", endpoint: "csi://" + pendingAFSUID + "/volume"},
	}
	if uid := afsResourceUIDFromPVC(nil); uid != "" {
		t.Fatalf("nil PVC UID = %q", uid)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pvc := pendingAFSTestPVC()
			pvc.Annotations["afs.endpoint"] = test.endpoint
			if got := afsResourceUIDFromPVC(pvc); got != test.want {
				t.Fatalf("AFS UID = %q, want %q", got, test.want)
			}
		})
	}
}

func TestResolveVolumeClaimRefInvalidAFSEndpointDoesNotQueryPlatform(t *testing.T) {
	platformClient := workloadTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("invalid AFS endpoint triggered platform request %s", r.URL.Path)
		http.NotFound(w, r)
	})
	for _, endpoint := range []string{pendingAFSUID, "https://afs.example.test", "csi://not-a-uuid", "csi://" + pendingAFSUID + "/volume"} {
		t.Run(endpoint, func(t *testing.T) {
			pvc := pendingAFSTestPVC()
			pvc.Annotations = map[string]string{"afs.endpoint": endpoint}
			service := &JobService{clientset: fake.NewSimpleClientset(pvc), vcClient: platformClient}
			refs := service.resolveVolumeClaimRefs(context.Background(), &jobIdentity{HostNamespace: pvc.Namespace}, []VolumeClaimRef{{ClaimName: pvc.Name}})
			if len(refs) != 1 || refs[0].FrontendVolume != "" || refs[0].Status != "Pending" {
				t.Fatalf("PVC with invalid AFS endpoint = %#v", refs)
			}
		})
	}
}

func TestResolveVolumeClaimRefBoundAFSKeepsPVFallback(t *testing.T) {
	const boundAFSUID = "019a90c2-2530-7e58-b47a-a86def77ad95"
	for _, endpoint := range []string{"", "csi://" + pendingAFSUID} {
		t.Run(endpoint, func(t *testing.T) {
			pvc := pendingAFSTestPVC()
			pvc.Name = "pvc-5wshr"
			pvc.Annotations["afs.endpoint"] = endpoint
			pvc.Spec.VolumeName = "host-pv"
			pvc.Status.Phase = corev1.ClaimBound
			platformClient := workloadTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/rmh/v1/resources:page" {
					t.Errorf("unexpected platform request %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				if strings.Contains(r.URL.Query().Get("filter"), boundAFSUID) {
					fmt.Fprintf(w, `{"resources":[{"id":%q,"name":"afs-a3-weight-share","resource_type":"storage.afs.v1.volume","rid":"/subscriptions/sub/resourceGroups/default/regions/cn-pj-01/virtualVolumes/afs-a3-weight-share"}]}`, boundAFSUID)
					return
				}
				fmt.Fprint(w, `{"resources":[]}`)
			})
			clientset := fake.NewSimpleClientset(pvc, &corev1.PersistentVolume{
				ObjectMeta: metav1.ObjectMeta{Name: "host-pv"},
				Spec: corev1.PersistentVolumeSpec{ClaimRef: &corev1.ObjectReference{
					Name: "afs-" + boundAFSUID, Namespace: "afs-host",
				}},
			})
			service := &JobService{clientset: clientset, vcClient: platformClient}
			refs := service.resolveVolumeClaimRefs(context.Background(), &jobIdentity{HostNamespace: pvc.Namespace}, []VolumeClaimRef{{ClaimName: pvc.Name}})
			if len(refs) != 1 || refs[0].FrontendVolume != "afs-a3-weight-share" || refs[0].Status != "Bound" || refs[0].BackendPV != "host-pv" || refs[0].HostPVCName != "afs-"+boundAFSUID {
				t.Fatalf("Bound PVC with PV fallback = %#v", refs)
			}
			assertNoPVCSecretReads(t, clientset)
		})
	}
}

func assertNoPVCSecretReads(t *testing.T, clientset *fake.Clientset) {
	t.Helper()
	for _, action := range clientset.Actions() {
		if action.GetResource().Resource == "secrets" {
			t.Fatalf("AFS name lookup accessed Secret: %s", action.GetVerb())
		}
	}
}
