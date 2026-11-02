// Prometheus 查询客户端封装。
//
// 封装 Prometheus HTTP API 的常用查询：
//   - QueryRange: 区间查询（用于图表展示）
//   - Query: 瞬时查询（用于状态判定）
//
// 使用接口抽象，便于测试 mock 和未来替换实现。
package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// PromClient Prometheus 查询客户端接口。
type PromClient interface {
	// QueryRange 区间查询。
	QueryRange(ctx context.Context, query string, start, end time.Time, stepSec int) ([]DataPoint, error)

	// Query 瞬时查询。
	Query(ctx context.Context, query string, at time.Time) (float64, error)
}

// HTTPPromClient 基于 HTTP 的 Prometheus 客户端。
type HTTPPromClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewHTTPPromClient 创建 HTTP Prometheus 客户端。
func NewHTTPPromClient(baseURL string) *HTTPPromClient {
	return &HTTPPromClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// promRangeResponse Prometheus query_range 响应结构。
type promRangeResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string          `json:"resultType"`
		Result     []promRangeItem `json:"result"`
	} `json:"data"`
	Error string `json:"error,omitempty"`
}

type promRangeItem struct {
	Metric map[string]string `json:"metric"`
	Values [][]interface{}   `json:"values"` // [timestamp, value_string]
}

// promInstantResponse Prometheus query 响应结构。
type promInstantResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string            `json:"resultType"`
		Result     []promInstantItem `json:"result"`
	} `json:"data"`
	Error string `json:"error,omitempty"`
}

type promInstantItem struct {
	Metric map[string]string `json:"metric"`
	Value  []interface{}     `json:"value"` // [timestamp, value_string]
}

// QueryRange 执行区间查询。
func (c *HTTPPromClient) QueryRange(ctx context.Context, query string, start, end time.Time, stepSec int) ([]DataPoint, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("start", fmt.Sprintf("%d", start.Unix()))
	params.Set("end", fmt.Sprintf("%d", end.Unix()))
	params.Set("step", strconv.Itoa(stepSec))

	reqURL := fmt.Sprintf("%s/api/v1/query_range?%s", c.baseURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 Prometheus 失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Prometheus 返回状态码 %d: %s", resp.StatusCode, string(body))
	}

	var result promRangeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if result.Status != "success" {
		return nil, fmt.Errorf("Prometheus 查询失败: %s", result.Error)
	}

	// 把多条时间序列的值合并（取均值）
	// 如果只有一条序列，直接返回
	if len(result.Data.Result) == 0 {
		return []DataPoint{}, nil
	}

	// 以第一条序列的时间戳为基准
	first := result.Data.Result[0]
	points := make([]DataPoint, 0, len(first.Values))

	for i, val := range first.Values {
		ts := int64(val[0].(float64) * 1000)
		v := parseFloat(val[1])

		// 如果有多条序列，取平均
		if len(result.Data.Result) > 1 {
			sum := v
			for j := 1; j < len(result.Data.Result); j++ {
				if i < len(result.Data.Result[j].Values) {
					sum += parseFloat(result.Data.Result[j].Values[i][1])
				}
			}
			v = sum / float64(len(result.Data.Result))
		}

		points = append(points, DataPoint{
			Timestamp: ts,
			Value:     v,
		})
	}

	return points, nil
}

// Query 执行瞬时查询。
func (c *HTTPPromClient) Query(ctx context.Context, query string, at time.Time) (float64, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("time", fmt.Sprintf("%d", at.Unix()))

	reqURL := fmt.Sprintf("%s/api/v1/query?%s", c.baseURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return 0, fmt.Errorf("构造请求失败: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("请求 Prometheus 失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("Prometheus 返回状态码 %d: %s", resp.StatusCode, string(body))
	}

	var result promInstantResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("解析响应失败: %w", err)
	}

	if result.Status != "success" {
		return 0, fmt.Errorf("Prometheus 查询失败: %s", result.Error)
	}

	if len(result.Data.Result) == 0 {
		return 0, nil
	}

	// 取第一条结果的值
	val := parseFloat(result.Data.Result[0].Value[1])
	return val, nil
}

// parseFloat 将 Prometheus 返回的字符串值转为 float64。
func parseFloat(v interface{}) float64 {
	switch val := v.(type) {
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	case float64:
		return val
	default:
		return 0
	}
}
