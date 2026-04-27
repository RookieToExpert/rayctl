package platform

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	defaultAPIBaseURL        = "https://management.d.pjlab.org.cn"
	defaultKubernetesBaseURL = "https://compute.d.pjlab.org.cn"
	defaultResourceGroup     = "default"
	defaultRegion            = "cn-pj-01"
	defaultPageLimit         = 100
	defaultConfigPath        = "/root/.rayctl/platform.json"
)

type VirtualClusterClient struct {
	accessKey         string
	secretKey         string
	baseURL           string
	kubernetesBaseURL string
	subscription      string
	resourceGroup     string
	region            string
	httpClient        *http.Client
}

type VirtualCluster struct {
	UID         string `json:"uid"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

type virtualClusterListResponse struct {
	VirtualClusters []VirtualCluster `json:"virtual_clusters"`
	NextPageToken   string           `json:"next_page_token"`
}

type config struct {
	AccessKey         string `json:"access_key"`
	SecretKey         string `json:"secret_key"`
	Subscription      string `json:"subscription_id"`
	BaseURL           string `json:"base_url"`
	KubernetesBaseURL string `json:"kubernetes_base_url"`
	ResourceGroup     string `json:"resource_group"`
	Region            string `json:"region"`
}

func NewVirtualClusterClientFromEnv() (*VirtualClusterClient, bool) {
	if client, ok := newVirtualClusterClientFromFile(defaultConfigPath); ok {
		return client, true
	}

	accessKey := strings.TrimSpace(os.Getenv("RAYCTL_PLATFORM_ACCESS_KEY"))
	secretKey := strings.TrimSpace(os.Getenv("RAYCTL_PLATFORM_SECRET_KEY"))
	subscription := strings.TrimSpace(os.Getenv("RAYCTL_PLATFORM_SUBSCRIPTION_ID"))
	if accessKey == "" || secretKey == "" || subscription == "" {
		return nil, false
	}

	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("RAYCTL_PLATFORM_BASE_URL")), "/")
	if baseURL == "" {
		baseURL = defaultAPIBaseURL
	}

	kubernetesBaseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("RAYCTL_PLATFORM_KUBERNETES_BASE_URL")), "/")
	if kubernetesBaseURL == "" {
		kubernetesBaseURL = defaultKubernetesBaseURL
	}

	resourceGroup := strings.TrimSpace(os.Getenv("RAYCTL_PLATFORM_RESOURCE_GROUP"))
	if resourceGroup == "" {
		resourceGroup = defaultResourceGroup
	}

	region := strings.TrimSpace(os.Getenv("RAYCTL_PLATFORM_REGION"))
	if region == "" {
		region = defaultRegion
	}

	return &VirtualClusterClient{
		accessKey:         accessKey,
		secretKey:         secretKey,
		baseURL:           baseURL,
		kubernetesBaseURL: kubernetesBaseURL,
		subscription:      subscription,
		resourceGroup:     resourceGroup,
		region:            region,
		httpClient:        &http.Client{Timeout: 10 * time.Second},
	}, true
}

func newVirtualClusterClientFromFile(configPath string) (*VirtualClusterClient, bool) {
	content, err := os.ReadFile(filepath.Clean(configPath))
	if err != nil {
		return nil, false
	}

	var cfg config
	if err := json.Unmarshal(content, &cfg); err != nil {
		return nil, false
	}

	accessKey := strings.TrimSpace(cfg.AccessKey)
	secretKey := strings.TrimSpace(cfg.SecretKey)
	subscription := strings.TrimSpace(cfg.Subscription)
	if accessKey == "" || secretKey == "" || subscription == "" {
		return nil, false
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultAPIBaseURL
	}

	kubernetesBaseURL := strings.TrimRight(strings.TrimSpace(cfg.KubernetesBaseURL), "/")
	if kubernetesBaseURL == "" {
		kubernetesBaseURL = defaultKubernetesBaseURL
	}

	resourceGroup := strings.TrimSpace(cfg.ResourceGroup)
	if resourceGroup == "" {
		resourceGroup = defaultResourceGroup
	}

	region := strings.TrimSpace(cfg.Region)
	if region == "" {
		region = defaultRegion
	}

	return &VirtualClusterClient{
		accessKey:         accessKey,
		secretKey:         secretKey,
		baseURL:           baseURL,
		kubernetesBaseURL: kubernetesBaseURL,
		subscription:      subscription,
		resourceGroup:     resourceGroup,
		region:            region,
		httpClient:        &http.Client{Timeout: 10 * time.Second},
	}, true
}

func (c *VirtualClusterClient) ResolveDisplayNames(ctx context.Context, uids []string) (map[string]string, error) {
	uniqueUIDs := make(map[string]struct{}, len(uids))
	for _, uid := range uids {
		uid = strings.TrimSpace(uid)
		if uid == "" {
			continue
		}
		uniqueUIDs[uid] = struct{}{}
	}
	if len(uniqueUIDs) == 0 {
		return map[string]string{}, nil
	}

	clusters, err := c.listVirtualClusters(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string, len(uniqueUIDs))
	for _, cluster := range clusters {
		if _, ok := uniqueUIDs[cluster.UID]; !ok {
			continue
		}
		result[cluster.UID] = firstNonEmpty(cluster.DisplayName, cluster.Name, cluster.UID)
	}

	return result, nil
}

func (c *VirtualClusterClient) ListVirtualClusters(ctx context.Context) ([]VirtualCluster, error) {
	return c.listVirtualClusters(ctx)
}

func (c *VirtualClusterClient) GetVolcanoJob(ctx context.Context, vclusterName string, namespace string, jobName string) (*unstructured.Unstructured, error) {
	reqURL := c.kubernetesResourceURL(vclusterName, fmt.Sprintf("/apis/batch.volcano.sh/v1alpha1/namespaces/%s/jobs/%s", namespace, jobName), nil)
	var obj unstructured.Unstructured
	if err := c.getJSON(ctx, reqURL, &obj); err != nil {
		return nil, err
	}
	return &obj, nil
}

func (c *VirtualClusterClient) GetPodGroup(ctx context.Context, vclusterName string, namespace string, podGroupName string) (*unstructured.Unstructured, error) {
	reqURL := c.kubernetesResourceURL(vclusterName, fmt.Sprintf("/apis/scheduling.volcano.sh/v1beta1/namespaces/%s/podgroups/%s", namespace, podGroupName), nil)
	var obj unstructured.Unstructured
	if err := c.getJSON(ctx, reqURL, &obj); err != nil {
		return nil, err
	}
	return &obj, nil
}

func (c *VirtualClusterClient) GetSecret(ctx context.Context, vclusterName string, namespace string, secretName string) (*corev1.Secret, error) {
	reqURL := c.kubernetesResourceURL(vclusterName, fmt.Sprintf("/api/v1/namespaces/%s/secrets/%s", namespace, secretName), nil)
	var secret corev1.Secret
	if err := c.getJSON(ctx, reqURL, &secret); err != nil {
		return nil, err
	}
	return &secret, nil
}

func (c *VirtualClusterClient) GetPersistentVolumeClaim(ctx context.Context, vclusterName string, namespace string, claimName string) (*corev1.PersistentVolumeClaim, error) {
	reqURL := c.kubernetesResourceURL(vclusterName, fmt.Sprintf("/api/v1/namespaces/%s/persistentvolumeclaims/%s", namespace, claimName), nil)
	var pvc corev1.PersistentVolumeClaim
	if err := c.getJSON(ctx, reqURL, &pvc); err != nil {
		return nil, err
	}
	return &pvc, nil
}

func (c *VirtualClusterClient) ListJobPods(ctx context.Context, vclusterName string, namespace string, jobName string) ([]corev1.Pod, error) {
	query := url.Values{}
	query.Set("labelSelector", fmt.Sprintf("volcano.sh/job-name=%s", jobName))
	query.Set("filter", fmt.Sprintf("namespace=%q", namespace))
	query.Set("order", "name asc")

	reqURL := c.kubernetesResourceURL(vclusterName, "/api/v1/pods", query)
	var podList corev1.PodList
	if err := c.getJSON(ctx, reqURL, &podList); err != nil {
		return nil, err
	}
	return podList.Items, nil
}

func (c *VirtualClusterClient) ListEvents(ctx context.Context, vclusterName string, namespace string, name string, kind string) ([]corev1.Event, error) {
	query := url.Values{}
	fieldSelector := []string{fmt.Sprintf("involvedObject.name=%s", name)}
	if strings.TrimSpace(kind) != "" {
		fieldSelector = append(fieldSelector, fmt.Sprintf("involvedObject.kind=%s", kind))
	}
	query.Set("fieldSelector", strings.Join(fieldSelector, ","))

	reqURL := c.kubernetesClusterURL(vclusterName, fmt.Sprintf("/api/v1/namespaces/%s/events", namespace), query)
	var eventList corev1.EventList
	if err := c.getJSON(ctx, reqURL, &eventList); err != nil {
		return nil, err
	}
	return eventList.Items, nil
}

func (c *VirtualClusterClient) GetPodLogs(ctx context.Context, vclusterName string, namespace string, podName string, tailLines int64) ([]string, error) {
	query := url.Values{}
	query.Set("tailLines", fmt.Sprintf("%d", tailLines))

	reqURL := c.kubernetesClusterURL(vclusterName, fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/log", namespace, podName), query)
	body, err := c.getText(ctx, reqURL)
	if err != nil {
		return nil, err
	}

	lines := make([]string, 0)
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		line = strings.TrimRight(line, "\n")
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return []string{"<empty log output>"}, nil
	}
	return lines, nil
}

func (c *VirtualClusterClient) listVirtualClusters(ctx context.Context) ([]VirtualCluster, error) {
	pageToken := ""
	clusters := make([]VirtualCluster, 0)

	for {
		reqURL := c.listURL(pageToken)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("build virtual cluster request: %w", err)
		}

		headers, err := c.authHeaders(reqURL, http.MethodGet)
		if err != nil {
			return nil, err
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request virtual clusters: %w", err)
		}

		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read virtual cluster response: %w", readErr)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("virtual cluster api returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		var payload virtualClusterListResponse
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("decode virtual cluster response: %w", err)
		}

		clusters = append(clusters, payload.VirtualClusters...)
		if strings.TrimSpace(payload.NextPageToken) == "" {
			break
		}
		pageToken = payload.NextPageToken
	}

	return clusters, nil
}

func (c *VirtualClusterClient) listURL(pageToken string) string {
	u, _ := url.Parse(c.baseURL)
	u.Path = fmt.Sprintf("/compute/ecp/v1/subscriptions/%s/resourceGroups/%s/regions/%s/virtualClusters", c.subscription, c.resourceGroup, c.region)

	query := u.Query()
	query.Set("limit", fmt.Sprintf("%d", defaultPageLimit))
	if strings.TrimSpace(pageToken) != "" {
		query.Set("page_token", pageToken)
	}
	u.RawQuery = query.Encode()
	return u.String()
}

func (c *VirtualClusterClient) authHeaders(reqURL string, method string) (map[string]string, error) {
	parsedURL, err := url.Parse(reqURL)
	if err != nil {
		return nil, fmt.Errorf("parse auth url: %w", err)
	}

	dateHeader := time.Now().UTC().Format(http.TimeFormat)
	requestTarget := parsedURL.Path
	if parsedURL.RawQuery != "" {
		requestTarget += "?" + parsedURL.RawQuery
	}

	signContent := fmt.Sprintf("date: %s\nhost: %s\n@request-target: %s %s", dateHeader, parsedURL.Host, strings.ToLower(method), requestTarget)

	mac := hmac.New(sha256.New, []byte(c.secretKey))
	_, _ = mac.Write([]byte(signContent))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return map[string]string{
		"Date":          dateHeader,
		"Host":          parsedURL.Host,
		"Accept":        "application/json",
		"Authorization": fmt.Sprintf(`hmac accesskey="%s", algorithm="hmac-sha256", headers="date host @request-target", signature="%s"`, c.accessKey, signature),
	}, nil
}

func (c *VirtualClusterClient) getJSON(ctx context.Context, reqURL string, out any) error {
	body, err := c.doRequest(ctx, reqURL)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response from %s: %w", reqURL, err)
	}
	return nil
}

func (c *VirtualClusterClient) getText(ctx context.Context, reqURL string) (string, error) {
	body, err := c.doRequest(ctx, reqURL)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (c *VirtualClusterClient) doRequest(ctx context.Context, reqURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	headers, err := c.authHeaders(reqURL, http.MethodGet)
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", reqURL, err)
	}

	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read response from %s: %w", reqURL, readErr)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request %s returned %d: %s", reqURL, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func (c *VirtualClusterClient) kubernetesResourceURL(vclusterName string, path string, query url.Values) string {
	u, _ := url.Parse(c.kubernetesBaseURL)
	u.Path = fmt.Sprintf("/ecp/v1/kubernetes/virtualClusters/%s%s", vclusterName, path)
	if query != nil {
		u.RawQuery = query.Encode()
	}
	return u.String()
}

func (c *VirtualClusterClient) kubernetesClusterURL(vclusterName string, path string, query url.Values) string {
	u, _ := url.Parse(c.kubernetesBaseURL)
	u.Path = fmt.Sprintf("/ecp/v1/clusters/%s%s", vclusterName, path)
	if query != nil {
		u.RawQuery = query.Encode()
	}
	return u.String()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
