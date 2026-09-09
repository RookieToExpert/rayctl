package platform

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type AFSCreateRequest struct {
	Name         string
	DisplayName  string
	CapacityGB   int64
	StorageClass string
	Zone         string
	Years        int
	AutoRenew    bool
}

type AFSCreatePlan struct {
	Name          string         `json:"name"`
	DisplayName   string         `json:"display_name,omitempty"`
	CapacityGB    int64          `json:"capacity_gb"`
	StorageClass  string         `json:"storage_class"`
	Zone          string         `json:"zone"`
	Region        string         `json:"region"`
	Years         int            `json:"years"`
	AutoRenew     bool           `json:"auto_renew"`
	SKU           string         `json:"sku_id"`
	SKUName       string         `json:"sku_name"`
	ProfileName   string         `json:"profile_name"`
	Subscription  string         `json:"subscription_id"`
	ResourceGroup string         `json:"resource_group"`
	RequestURL    string         `json:"request_url"`
	Payload       map[string]any `json:"payload"`

	profile clientProfile
}

type AFSCreateResult struct {
	Name          string `json:"name"`
	UID           string `json:"uid"`
	State         string `json:"state"`
	OrderID       string `json:"order_id"`
	OrderState    string `json:"order_state,omitempty"`
	Amount        string `json:"amount,omitempty"`
	PaymentResult string `json:"payment_result"`
	ProfileName   string `json:"profile_name"`
	Warning       string `json:"warning,omitempty"`
}

type afsSPUListResponse struct {
	SPUs []struct {
		ID string `json:"id"`
	} `json:"spus"`
}

type afsSKUListResponse struct {
	SKUs []afsSKU `json:"skus"`
}

type afsSKU struct {
	ID           string   `json:"id"`
	DisplayName  string   `json:"display_name"`
	Regions      []string `json:"regions"`
	Zones        []string `json:"available_zones"`
	PaymentModel string   `json:"payment_model"`
	BillingModel string   `json:"billing_model"`
	Status       string   `json:"status"`
	Attributes   []struct {
		PropertyName string `json:"property_name"`
		Value        string `json:"value"`
	} `json:"attributes"`
}

type afsCreateResponse struct {
	Name      string `json:"name"`
	UID       string `json:"uid"`
	State     string `json:"state"`
	OrderInfo struct {
		OrderID string `json:"order_id"`
	} `json:"order_info"`
}

type afsOrderResponse struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Amount struct {
		CurrencyCode string `json:"currency_code"`
		Units        string `json:"units"`
		Nanos        int64  `json:"nanos"`
	} `json:"amount"`
}

type afsPaymentResponse struct {
	OrderID     string `json:"order_id"`
	Interactive bool   `json:"interactive"`
}

type afsResourceGroupListResponse struct {
	ResourceGroups []struct {
		Name             string `json:"name"`
		RID              string `json:"rid"`
		SubscriptionName string `json:"subscription_name"`
		SubscriptionRID  string `json:"subscription_rid"`
	} `json:"resource_groups"`
}

func (c *VirtualClusterClient) PrepareAFSCreate(ctx context.Context, request AFSCreateRequest) (*AFSCreatePlan, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("current platform profile not found")
	}
	if strings.TrimSpace(profile.Subscription) == "" {
		resolved, err := c.resolveAFSCreateSubscription(ctx, profile)
		if err != nil {
			return nil, err
		}
		profile.Subscription = resolved
	}
	request.Name = strings.TrimSpace(request.Name)
	request.StorageClass = strings.ToUpper(strings.TrimSpace(request.StorageClass))
	request.Zone = strings.TrimSpace(request.Zone)
	if request.Zone == "" {
		request.Zone = strings.TrimSpace(profile.Region) + "a"
	}
	if request.CapacityGB <= 0 {
		return nil, fmt.Errorf("AFS capacity must be greater than zero")
	}
	if request.Years <= 0 {
		return nil, fmt.Errorf("billing years must be greater than zero")
	}

	financeURL := financeBaseURLFromManagement(profile.BaseURL)
	spuURL, _ := url.Parse(financeURL)
	spuURL.Path = "/boss/catalog/v1/spus"
	query := spuURL.Query()
	query.Set("filter", `backend_catalog_code="AFSV2" AND cloud_type="CT_PUBLIC"`)
	spuURL.RawQuery = query.Encode()
	var spus afsSPUListResponse
	if err := c.getJSONWithProfile(ctx, profile, spuURL.String(), &spus); err != nil {
		return nil, fmt.Errorf("query AFS product catalog: %w", err)
	}
	if len(spus.SPUs) == 0 || strings.TrimSpace(spus.SPUs[0].ID) == "" {
		return nil, fmt.Errorf("no AFSV2 product is available in profile %s", profile.Name)
	}

	skuURL, _ := url.Parse(financeURL)
	skuURL.Path = "/boss/catalog/v1/spus/" + url.PathEscape(spus.SPUs[0].ID) + "/skus"
	query = skuURL.Query()
	query.Set("have_attributes", "true")
	skuURL.RawQuery = query.Encode()
	var skus afsSKUListResponse
	if err := c.getJSONWithProfile(ctx, profile, skuURL.String(), &skus); err != nil {
		return nil, fmt.Errorf("query AFS SKU catalog: %w", err)
	}
	sku, err := selectAFSSKU(skus.SKUs, request.StorageClass, profile.Region, request.Zone)
	if err != nil {
		return nil, err
	}

	createURL, _ := url.Parse(profile.BaseURL)
	createURL.Path = fmt.Sprintf(
		"/storage/afs/v2/subscriptions/%s/resourceGroups/%s/zones/%s/virtualVolumes/%s",
		url.PathEscape(profile.Subscription), url.PathEscape(profile.ResourceGroup), url.PathEscape(request.Zone), url.PathEscape(request.Name),
	)
	start := time.Now().In(time.FixedZone("UTC+8", 8*60*60)).Format(time.RFC3339)
	payload := map[string]any{
		"display_name":  request.DisplayName,
		"name":          request.Name,
		"resource_type": "storage.afs.v2.volume",
		"zone":          request.Zone,
		"sku_id":        sku.ID,
		"properties": map[string]any{
			"resources":     map[string]any{"billing_items": map[string]any{"capacity": request.CapacityGB, "capacity_unit": "GB"}},
			"storage_class": request.StorageClass,
		},
		"order_info": map[string]any{
			"billing_cycle_number": fmt.Sprintf("%d", request.Years),
			"auto_renew":           request.AutoRenew,
			"currency_code":        "CNY",
			"payment_channel":      "PC_UNSPECIFIED",
			"note":                 "",
			"order_type":           "OT_ORIGINAL",
			"start_time":           start,
			"payment_model":        "PM_PREPAID",
			"billing_model":        "BM_YEARLY",
		},
	}
	return &AFSCreatePlan{
		Name: request.Name, DisplayName: request.DisplayName, CapacityGB: request.CapacityGB,
		StorageClass: request.StorageClass, Zone: request.Zone, Region: profile.Region,
		Years: request.Years, AutoRenew: request.AutoRenew, SKU: sku.ID, SKUName: sku.DisplayName,
		ProfileName: profile.Name, Subscription: profile.Subscription, ResourceGroup: profile.ResourceGroup,
		RequestURL: createURL.String(), Payload: payload, profile: profile,
	}, nil
}

func (c *VirtualClusterClient) resolveAFSCreateSubscription(ctx context.Context, profile clientProfile) (string, error) {
	requestURL, _ := url.Parse(profile.BaseURL)
	requestURL.Path = "/rmh/v1/resourceGroups"
	var response afsResourceGroupListResponse
	if err := c.getJSONWithProfile(ctx, profile, requestURL.String(), &response); err != nil {
		return "", fmt.Errorf("resolve subscription for AFS creation: %w", err)
	}
	for _, group := range response.ResourceGroups {
		if !strings.EqualFold(strings.TrimSpace(group.Name), strings.TrimSpace(profile.ResourceGroup)) {
			continue
		}
		if subscription := firstNonEmpty(
			strings.TrimSpace(group.SubscriptionName),
			ridSegment(group.SubscriptionRID, "subscriptions"),
			ridSegment(group.RID, "subscriptions"),
		); subscription != "" {
			return subscription, nil
		}
	}
	return "", fmt.Errorf("cannot determine subscription id for resource group %q in profile %s", profile.ResourceGroup, profile.Name)
}

func (c *VirtualClusterClient) CreateAFS(ctx context.Context, plan *AFSCreatePlan) (*AFSCreateResult, error) {
	if plan == nil || strings.TrimSpace(plan.RequestURL) == "" {
		return nil, fmt.Errorf("prepared AFS creation plan is required")
	}
	var created afsCreateResponse
	if err := c.postJSONWithProfile(ctx, plan.profile, plan.RequestURL, plan.Payload, &created); err != nil {
		return nil, fmt.Errorf("create AFS %s: %w", plan.Name, err)
	}
	result := &AFSCreateResult{
		Name: firstNonEmpty(created.Name, plan.Name), UID: created.UID, State: created.State,
		OrderID: created.OrderInfo.OrderID, ProfileName: plan.ProfileName, PaymentResult: "pending",
	}
	if strings.TrimSpace(result.OrderID) == "" {
		return result, fmt.Errorf("AFS %s was created but the response did not contain an order id", result.Name)
	}

	financeURL := financeBaseURLFromManagement(plan.profile.BaseURL)
	orderURL, _ := url.Parse(financeURL)
	orderURL.Path = "/boss/subscription/v1/orders/" + url.PathEscape(result.OrderID)
	var order afsOrderResponse
	if err := c.getJSONWithProfile(ctx, plan.profile, orderURL.String(), &order); err != nil {
		result.Warning = "order lookup failed: " + err.Error()
	} else {
		result.OrderState = order.State
		result.Amount = formatAFSOrderAmount(order.Amount.CurrencyCode, order.Amount.Units, order.Amount.Nanos)
	}

	payURL, _ := url.Parse(financeURL)
	payURL.Path = "/boss/subscription/v1/orders/" + url.PathEscape(result.OrderID) + ":pay"
	paymentPayload := map[string]any{
		"name":            "orders/" + result.OrderID,
		"payment_channel": "PC_BALANCE",
		"ba_vouchers":     []any{},
		"sku_contracts":   []any{},
	}
	var payment afsPaymentResponse
	if err := c.postJSONWithProfile(ctx, plan.profile, payURL.String(), paymentPayload, &payment); err != nil {
		return result, fmt.Errorf("AFS %s was created with order %s, but payment failed: %w", result.Name, result.OrderID, err)
	}
	result.PaymentResult = "submitted"
	if payment.Interactive {
		result.PaymentResult = "interactive payment required"
	}
	return result, nil
}

func selectAFSSKU(skus []afsSKU, storageClass, region, zone string) (*afsSKU, error) {
	for i := range skus {
		sku := &skus[i]
		if sku.Status != "" && !strings.EqualFold(sku.Status, "ST_RELEASE") {
			continue
		}
		if sku.PaymentModel != "" && !strings.EqualFold(sku.PaymentModel, "PM_PREPAID") {
			continue
		}
		if sku.BillingModel != "" && !strings.EqualFold(sku.BillingModel, "BM_YEARLY") {
			continue
		}
		classMatches := false
		for _, attribute := range sku.Attributes {
			if strings.EqualFold(attribute.PropertyName, "storage_class") && strings.EqualFold(attribute.Value, storageClass) {
				classMatches = true
				break
			}
		}
		if !classMatches || !containsFold(sku.Regions, region) || !containsFold(sku.Zones, zone) {
			continue
		}
		return sku, nil
	}
	return nil, fmt.Errorf("no released yearly prepaid AFS SKU matches class %s in %s/%s", storageClass, region, zone)
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}

func financeBaseURLFromManagement(baseURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Host == "" {
		return "https://finance.d.pjlab.org.cn"
	}
	switch {
	case strings.HasPrefix(parsed.Host, "management-cloud."):
		parsed.Host = "finance-cloud." + strings.TrimPrefix(parsed.Host, "management-cloud.")
	case strings.HasPrefix(parsed.Host, "management."):
		parsed.Host = "finance." + strings.TrimPrefix(parsed.Host, "management.")
	default:
		parsed.Host = "finance." + parsed.Host
	}
	parsed.Path, parsed.RawQuery, parsed.Fragment = "", "", ""
	return strings.TrimRight(parsed.String(), "/")
}

func formatAFSOrderAmount(currency, units string, nanos int64) string {
	if strings.TrimSpace(currency) == "" && strings.TrimSpace(units) == "" && nanos == 0 {
		return ""
	}
	amount := strings.TrimSpace(units)
	if amount == "" {
		amount = "0"
	}
	if nanos != 0 {
		fraction := strings.TrimRight(fmt.Sprintf("%09d", nanos), "0")
		amount += "." + fraction
	}
	return strings.TrimSpace(strings.TrimSpace(currency) + " " + amount)
}
