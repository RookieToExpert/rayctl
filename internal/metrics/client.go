package metrics

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Client struct {
	baseURL   string
	username  string
	password  string
	http      *http.Client
	close     func()
	closeOnce sync.Once
}

type synchronizedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *synchronizedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

func NewClient(ctx context.Context, cfg EndpointConfig) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("metrics_base_url 未配置")
	}
	cleanup := func() {}
	if strings.HasPrefix(baseURL, "k8s://") {
		forwarded, closeForward, err := startPortForward(ctx, baseURL, cfg.Kubeconfig)
		if err != nil {
			return nil, err
		}
		baseURL = forwarded
		cleanup = closeForward
	}
	if !strings.HasSuffix(baseURL, "/prometheus/api/v1") {
		baseURL += "/prometheus/api/v1"
	}
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		baseURL:  baseURL,
		username: strings.TrimSpace(cfg.Username),
		password: cfg.Password,
		http:     &http.Client{Timeout: timeout},
		close:    cleanup,
	}, nil
}

func (c *Client) Close() {
	if c == nil {
		return
	}
	c.closeOnce.Do(c.close)
}

func (c *Client) Export(ctx context.Context, selector string, start time.Time, end time.Time) ([]RawSeries, error) {
	values := url.Values{}
	values.Add("match[]", selector)
	values.Set("start", strconv.FormatInt(start.UnixMilli(), 10))
	values.Set("end", strconv.FormatInt(end.UnixMilli(), 10))
	body, err := c.get(ctx, "/export", values)
	if err != nil {
		return nil, err
	}
	return decodeExport(body)
}

func (c *Client) QueryRange(ctx context.Context, query string, start time.Time, end time.Time, step time.Duration) ([]RawSeries, error) {
	values := url.Values{}
	values.Set("query", query)
	values.Set("start", formatSeconds(start))
	values.Set("end", formatSeconds(end))
	values.Set("step", formatDurationSeconds(step))
	body, err := c.get(ctx, "/query_range", values)
	if err != nil {
		return nil, err
	}
	return decodePrometheusResponse(body)
}

func (c *Client) Query(ctx context.Context, query string, at time.Time) ([]RawSeries, error) {
	values := url.Values{}
	values.Set("query", query)
	values.Set("time", formatSeconds(at))
	body, err := c.get(ctx, "/query", values)
	if err != nil {
		return nil, err
	}
	return decodePrometheusResponse(body)
}

func (c *Client) get(ctx context.Context, path string, values url.Values) ([]byte, error) {
	reqURL := c.baseURL + path + "?" + values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	if c.username != "" || c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query metrics: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 128<<20))
	if err != nil {
		return nil, fmt.Errorf("read metrics response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := strings.TrimSpace(string(body))
		if len(message) > 1000 {
			message = message[:1000] + "..."
		}
		return nil, fmt.Errorf("metrics request returned %d: %s", resp.StatusCode, message)
	}
	return body, nil
}

func decodeExport(body []byte) ([]RawSeries, error) {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 64*1024), 128<<20)
	series := make([]RawSeries, 0)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var raw struct {
			Metric     map[string]string `json:"metric"`
			Values     []json.RawMessage `json:"values"`
			Timestamps []int64           `json:"timestamps"`
		}
		if err := json.Unmarshal(line, &raw); err != nil {
			return nil, fmt.Errorf("decode metrics export line: %w", err)
		}
		values, err := parseRawValues(raw.Values)
		if err != nil {
			return nil, err
		}
		series = append(series, RawSeries{Metric: raw.Metric, Values: values, Timestamps: raw.Timestamps})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan metrics export: %w", err)
	}
	return series, nil
}

func decodePrometheusResponse(body []byte) ([]RawSeries, error) {
	var response struct {
		Status    string `json:"status"`
		ErrorType string `json:"errorType"`
		Error     string `json:"error"`
		Data      struct {
			ResultType string          `json:"resultType"`
			Result     json.RawMessage `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode prometheus response: %w", err)
	}
	if response.Status != "success" {
		return nil, fmt.Errorf("prometheus %s: %s", response.ErrorType, response.Error)
	}
	switch response.Data.ResultType {
	case "matrix":
		var rows []struct {
			Metric map[string]string   `json:"metric"`
			Values [][]json.RawMessage `json:"values"`
		}
		if err := json.Unmarshal(response.Data.Result, &rows); err != nil {
			return nil, err
		}
		result := make([]RawSeries, 0, len(rows))
		for _, row := range rows {
			item := RawSeries{Metric: row.Metric}
			for _, pair := range row.Values {
				if len(pair) != 2 {
					continue
				}
				ts, err := parseJSONFloat(pair[0])
				if err != nil {
					continue
				}
				value, err := parseJSONFloat(pair[1])
				if err != nil {
					continue
				}
				item.Timestamps = append(item.Timestamps, int64(ts*1000))
				item.Values = append(item.Values, value)
			}
			result = append(result, item)
		}
		return result, nil
	case "vector":
		var rows []struct {
			Metric map[string]string `json:"metric"`
			Value  []json.RawMessage `json:"value"`
		}
		if err := json.Unmarshal(response.Data.Result, &rows); err != nil {
			return nil, err
		}
		result := make([]RawSeries, 0, len(rows))
		for _, row := range rows {
			if len(row.Value) != 2 {
				continue
			}
			ts, err1 := parseJSONFloat(row.Value[0])
			value, err2 := parseJSONFloat(row.Value[1])
			if err1 == nil && err2 == nil {
				result = append(result, RawSeries{Metric: row.Metric, Values: []float64{value}, Timestamps: []int64{int64(ts * 1000)}})
			}
		}
		return result, nil
	case "scalar":
		var pair []json.RawMessage
		if err := json.Unmarshal(response.Data.Result, &pair); err != nil || len(pair) != 2 {
			return nil, fmt.Errorf("decode prometheus scalar result")
		}
		ts, err1 := parseJSONFloat(pair[0])
		value, err2 := parseJSONFloat(pair[1])
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("decode prometheus scalar value")
		}
		return []RawSeries{{Metric: map[string]string{}, Values: []float64{value}, Timestamps: []int64{int64(ts * 1000)}}}, nil
	default:
		return nil, fmt.Errorf("unsupported prometheus result type %q", response.Data.ResultType)
	}
}

func parseRawValues(raw []json.RawMessage) ([]float64, error) {
	values := make([]float64, 0, len(raw))
	for _, item := range raw {
		value, err := parseJSONFloat(item)
		if err != nil {
			return nil, fmt.Errorf("decode metric value: %w", err)
		}
		values = append(values, value)
	}
	return values, nil
}

func parseJSONFloat(raw json.RawMessage) (float64, error) {
	var number float64
	if err := json.Unmarshal(raw, &number); err == nil {
		return number, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, err
	}
	return strconv.ParseFloat(text, 64)
}

func formatSeconds(value time.Time) string {
	return strconv.FormatFloat(float64(value.UnixNano())/1e9, 'f', 3, 64)
}

func formatDurationSeconds(value time.Duration) string {
	return strconv.FormatFloat(value.Seconds(), 'f', -1, 64)
}

func startPortForward(ctx context.Context, rawURL string, kubeconfig string) (string, func(), error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", nil, fmt.Errorf("parse metrics k8s endpoint: %w", err)
	}
	namespace := strings.TrimSpace(parsed.Host)
	servicePort := strings.TrimPrefix(strings.TrimSpace(parsed.Path), "/")
	parts := strings.Split(servicePort, ":")
	if namespace == "" || len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
		return "", nil, fmt.Errorf("invalid metrics k8s endpoint %q; expected k8s://namespace/service:port", rawURL)
	}
	remotePort, err := strconv.Atoi(parts[1])
	if err != nil || remotePort <= 0 {
		return "", nil, fmt.Errorf("invalid metrics service port in %q", rawURL)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, fmt.Errorf("reserve metrics port: %w", err)
	}
	localPort := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	forwardCtx, cancel := context.WithCancel(ctx)
	args := []string{}
	if strings.TrimSpace(kubeconfig) != "" {
		args = append(args, "--kubeconfig", kubeconfig)
	}
	args = append(args, "port-forward", "-n", namespace, "service/"+parts[0], fmt.Sprintf("%d:%d", localPort, remotePort), "--address=127.0.0.1")
	cmd := exec.CommandContext(forwardCtx, "kubectl", args...)
	var output synchronizedBuffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		cancel()
		return "", nil, fmt.Errorf("start metrics port-forward: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			cancel()
			return "", nil, fmt.Errorf("metrics port-forward stopped: %v: %s", err, strings.TrimSpace(output.String()))
		case <-deadline.C:
			cancel()
			return "", nil, fmt.Errorf("metrics port-forward timeout: %s", strings.TrimSpace(output.String()))
		case <-ticker.C:
			if strings.Contains(output.String(), "Forwarding from") {
				cleanup := func() {
					cancel()
					select {
					case <-done:
					case <-time.After(time.Second):
						if cmd.Process != nil {
							_ = cmd.Process.Kill()
						}
					}
				}
				return fmt.Sprintf("http://127.0.0.1:%d", localPort), cleanup, nil
			}
		}
	}
}
