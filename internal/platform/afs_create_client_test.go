package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestPrepareAndCreateAFS(t *testing.T) {
	requests := make([]string, 0, 5)
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.Method+" "+r.URL.Host+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "GET /rmh/v1/resourceGroups":
			return jsonHTTPResponse(r, `{"resource_groups":[{"name":"default","subscription_name":"sub-id"}]}`), nil
		case "GET /boss/catalog/v1/spus":
			return jsonHTTPResponse(r, `{"spus":[{"id":"spu-afs"}]}`), nil
		case "GET /boss/catalog/v1/spus/spu-afs/skus":
			return jsonHTTPResponse(r, `{"skus":[{"id":"sku-oceanstor","display_name":"OceanStor - 包年","regions":["cn-pj-01"],"available_zones":["cn-pj-01a"],"payment_model":"PM_PREPAID","billing_model":"BM_YEARLY","status":"ST_RELEASE","attributes":[{"property_name":"storage_class","value":"OCEANSTOR"}]}]}`), nil
		case "POST /storage/afs/v2/subscriptions/sub-id/resourceGroups/default/zones/cn-pj-01a/virtualVolumes/afs-test":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				return nil, err
			}
			properties := payload["properties"].(map[string]any)
			resources := properties["resources"].(map[string]any)
			billingItems := resources["billing_items"].(map[string]any)
			if billingItems["capacity"] != float64(1024) || properties["storage_class"] != "OCEANSTOR" {
				t.Errorf("unexpected create payload: %#v", payload)
			}
			return jsonHTTPResponse(r, `{"name":"afs-test","uid":"afs-uid","state":"CREATING","order_info":{"order_id":"order-1"}}`), nil
		case "GET /boss/subscription/v1/orders/order-1":
			return jsonHTTPResponse(r, `{"id":"order-1","state":"OS_PENDING","amount":{"currency_code":"CNY","units":"0","nanos":0}}`), nil
		case "POST /boss/subscription/v1/orders/order-1:pay":
			return jsonHTTPResponse(r, `{"order_id":"orders/order-1","interactive":false}`), nil
		default:
			return nil, fmt.Errorf("unexpected request %s %s", r.Method, r.URL.String())
		}
	})}
	profile := clientProfile{
		Name: "ailabdev", AccessKey: "ak", SecretKey: "sk",
		BaseURL:       "https://management.d.pjlab.org.cn",
		ResourceGroup: "default", Region: "cn-pj-01",
	}
	client := &VirtualClusterClient{currentProfile: profile.Name, profiles: map[string]clientProfile{profile.Name: profile}, httpClient: httpClient}

	plan, err := client.PrepareAFSCreate(context.Background(), AFSCreateRequest{
		Name: "afs-test", CapacityGB: 1024, StorageClass: "OCEANSTOR", Years: 10, AutoRenew: true,
	})
	if err != nil {
		t.Fatalf("PrepareAFSCreate() error = %v", err)
	}
	if plan.Zone != "cn-pj-01a" || plan.SKU != "sku-oceanstor" {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	result, err := client.CreateAFS(context.Background(), plan)
	if err != nil {
		t.Fatalf("CreateAFS() error = %v", err)
	}
	if result.UID != "afs-uid" || result.OrderID != "order-1" || result.PaymentResult != "submitted" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if len(requests) != 6 {
		t.Fatalf("requests = %d, want 6: %#v", len(requests), requests)
	}
}
