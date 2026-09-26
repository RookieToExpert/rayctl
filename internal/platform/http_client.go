package platform

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

func (c *VirtualClusterClient) authHeadersForProfile(profile clientProfile, reqURL string, method string) (map[string]string, error) {
	return authHeadersForCredentials(profile.AccessKey, profile.SecretKey, reqURL, method)
}

func (c *VirtualClusterClient) authHeadersForCMSProfile(profile clientProfile, reqURL string, method string) (map[string]string, error) {
	return authHeadersForCredentials(profile.CMSAccessKey, profile.CMSSecretKey, reqURL, method)
}

func authHeadersForCredentials(accessKey string, secretKey string, reqURL string, method string) (map[string]string, error) {
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

	mac := hmac.New(sha256.New, []byte(secretKey))
	_, _ = mac.Write([]byte(signContent))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return map[string]string{
		"Date":          dateHeader,
		"Host":          parsedURL.Host,
		"Accept":        "application/json",
		"Authorization": fmt.Sprintf(`hmac accesskey="%s", algorithm="hmac-sha256", headers="date host @request-target", signature="%s"`, accessKey, signature),
	}, nil
}

func (c *VirtualClusterClient) getJSONWithProfile(ctx context.Context, profile clientProfile, reqURL string, out any) error {
	return c.getJSONWithAuth(ctx, profile, reqURL, false, "", out)
}

func (c *VirtualClusterClient) postJSONWithProfile(ctx context.Context, profile clientProfile, reqURL string, payload any, out any) error {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request for %s: %w", reqURL, err)
	}

	body, err := c.doRequestWithProfile(ctx, profile, http.MethodPost, reqURL, "application/json", bodyBytes)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response from %s: %w", reqURL, err)
	}
	return nil
}

func (c *VirtualClusterClient) postJSONWithBearerProfile(ctx context.Context, profile clientProfile, reqURL string, payload any, bearerToken string, out any) error {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request for %s: %w", reqURL, err)
	}

	body, err := c.doSignedRequest(ctx, profile, http.MethodPost, reqURL, "application/json", bodyBytes, false, bearerToken)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response from %s: %w", reqURL, err)
	}
	return nil
}

func (c *VirtualClusterClient) deleteWithBearerProfile(ctx context.Context, profile clientProfile, reqURL string, bearerToken string) error {
	_, err := c.doSignedRequest(ctx, profile, http.MethodDelete, reqURL, "", nil, false, bearerToken)
	return err
}

func (c *VirtualClusterClient) getTextWithProfile(ctx context.Context, profile clientProfile, reqURL string) (string, error) {
	body, err := c.doRequestWithProfile(ctx, profile, http.MethodGet, reqURL, "", nil)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (c *VirtualClusterClient) getJSONWithCMSProfile(ctx context.Context, profile clientProfile, reqURL string, out any) error {
	return c.getJSONWithAuth(ctx, profile, reqURL, true, "", out)
}

func (c *VirtualClusterClient) getJSONWithCMSProfileToken(ctx context.Context, profile clientProfile, reqURL string, bearerToken string, out any) error {
	return c.getJSONWithAuth(ctx, profile, reqURL, true, bearerToken, out)
}

func (c *VirtualClusterClient) getJSONWithBearerProfile(ctx context.Context, profile clientProfile, reqURL string, bearerToken string, out any) error {
	return c.getJSONWithAuth(ctx, profile, reqURL, false, bearerToken, out)
}

func (c *VirtualClusterClient) getJSONWithAuth(ctx context.Context, profile clientProfile, reqURL string, useCMS bool, bearerToken string, out any) error {
	body, err := c.doSignedRequest(ctx, profile, http.MethodGet, reqURL, "", nil, useCMS, bearerToken)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response from %s: %w", reqURL, err)
	}
	return nil
}

func (c *VirtualClusterClient) getTextWithCMSProfile(ctx context.Context, profile clientProfile, reqURL string, bearerToken string) (string, error) {
	body, err := c.doRequestWithCMSProfile(ctx, profile, http.MethodGet, reqURL, "", nil, bearerToken)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (c *VirtualClusterClient) doRequestWithProfile(ctx context.Context, profile clientProfile, method string, reqURL string, contentType string, requestBody []byte) ([]byte, error) {
	return c.doSignedRequest(ctx, profile, method, reqURL, contentType, requestBody, false, "")
}

func (c *VirtualClusterClient) doRequestWithCMSProfile(ctx context.Context, profile clientProfile, method string, reqURL string, contentType string, requestBody []byte, bearerToken ...string) ([]byte, error) {
	token := ""
	if len(bearerToken) > 0 {
		token = bearerToken[0]
	}
	return c.doSignedRequest(ctx, profile, method, reqURL, contentType, requestBody, true, token)
}

func (c *VirtualClusterClient) doSignedRequest(ctx context.Context, profile clientProfile, method string, reqURL string, contentType string, requestBody []byte, useCMS bool, bearerToken string, extraHeaders ...map[string]string) ([]byte, error) {
	var bodyReader io.Reader
	if len(requestBody) > 0 {
		bodyReader = bytes.NewReader(requestBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	headers := map[string]string{}
	if strings.TrimSpace(bearerToken) != "" {
		headers["Accept"] = "application/json"
		headers["Authorization"] = "Bearer " + strings.TrimSpace(bearerToken)
	} else {
		var err error
		if useCMS {
			headers, err = c.authHeadersForCMSProfile(profile, reqURL, method)
		} else {
			headers, err = c.authHeadersForProfile(profile, reqURL, method)
		}
		if err != nil {
			return nil, err
		}
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	for _, extra := range extraHeaders {
		for key, value := range extra {
			req.Header.Set(key, value)
		}
	}
	if strings.TrimSpace(contentType) != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := doPlatformRequest(c.httpClient, req)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", reqURL, err)
	}

	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read response from %s: %w", reqURL, readErr)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("request %s returned %d: %s", reqURL, resp.StatusCode, responseBodyExcerpt(body))
	}
	return body, nil
}

func responseBodyExcerpt(body []byte) string {
	const limit = 512
	if len(body) > limit {
		prefix := body[:limit]
		for len(prefix) > 0 && !utf8.Valid(prefix) {
			prefix = prefix[:len(prefix)-1]
		}
		return strings.TrimSpace(string(prefix)) + "..."
	}
	return strings.TrimSpace(string(body))
}
