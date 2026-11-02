// Package logquery Loki 日志查询客户端（T1-17 / T4-16 查询修正）。
package logquery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// LogEntry 单条日志条目。
type LogEntry struct {
	Timestamp int64             `json:"timestamp"` // 毫秒时间戳
	Line      string            `json:"line"`      // 日志内容
	Labels    map[string]string `json:"labels"`    // 标签（service/level/pod 等）
}

// LogStream Loki 返回的日志流。
type LogStream struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"` // [[nanotime, line], ...]
}

// LokiQueryResult Loki 查询结果（streams 型）。
type LokiQueryResult struct {
	ResultType string      `json:"resultType"`
	Streams    []LogStream `json:"result"`
}

// LokiVectorSample 向量结果中的单条样本：[纳秒时间戳, 值字符串]。
type LokiVectorSample []string

// LokiVector 向量结果中的单个序列。
type LokiVector struct {
	Metric map[string]string  `json:"metric"`
	Values []LokiVectorSample `json:"value"` // 向量查询只有一个样本
}

// LokiVectorResult Loki 向量查询结果（instant query 返回）。
type LokiVectorResult struct {
	ResultType string       `json:"resultType"`
	Vectors    []LokiVector `json:"result"`
}

// LokiQueryResponse Loki 日志流查询响应。
type LokiQueryResponse struct {
	Status string          `json:"status"`
	Data   LokiQueryResult `json:"data"`
}

// LokiVectorResponse Loki 向量查询响应。
type LokiVectorResponse struct {
	Status string           `json:"status"`
	Data   LokiVectorResult `json:"data"`
}

// LokiSeriesResponse Loki /series 查询响应。
type LokiSeriesResponse struct {
	Status string              `json:"status"`
	Data   []map[string]string `json:"data"`
}

// LokiClient Loki 查询客户端接口。
type LokiClient interface {
	// QueryRange 区间查询日志。
	//
	// query: LogQL 查询表达式，如 {sandbox_id="xxx", service="order-service"}
	// start: 开始时间
	// end:   结束时间
	// limit: 返回条数上限
	// direction: "backward"（从新到旧，默认）或 "forward"（从旧到新）
	QueryRange(ctx context.Context, query string, start, end time.Time, limit int, direction string) ([]LogEntry, error)

	// Query 瞬时查询（最新 N 条）。
	Query(ctx context.Context, query string, limit int) ([]LogEntry, error)

	// Count 统计指定时间范围内的日志条数（T4-16：instant 查询，单次求值，不再重复计数）。
	Count(ctx context.Context, query string, start, end time.Time) (int, error)

	// CountGrouped 统计指定时间范围内按某标签分组的日志条数（T4-16：一次 instant 查询替代多次 Count）。
	//
	// 例如 groupLabel="level" 返回 {"ERROR":12,"INFO":500}。
	// groupLabel 必须在 labelGroupWhitelist 白名单内，防止注入。
	CountGrouped(ctx context.Context, query, groupLabel string, start, end time.Time) (map[string]int, error)

	// Series 查询指定时间窗口内出现过的标签组合（T4-16：/series 端点，替代"拉日志提服务名"）。
	Series(ctx context.Context, matchers []string, start, end time.Time) ([]map[string]string, error)
}

// 允许按分组统计的标签白名单（防止把用户输入拼进聚合表达式）。
var labelGroupWhitelist = map[string]bool{
	"level":   true,
	"service": true,
}

// HTTPLokiClient 基于 HTTP 的 Loki 客户端。
type HTTPLokiClient struct {
	baseURL    string
	orgID      string // 可选 X-Scope-OrgID（多租户 Loki 场景），空则不发送
	httpClient *http.Client
}

// NewHTTPLokiClient 创建 Loki HTTP 客户端。
func NewHTTPLokiClient(baseURL string) *HTTPLokiClient {
	return &HTTPLokiClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// WithOrgID 注入多租户头 X-Scope-OrgID，返回自身便于链式装配。
func (c *HTTPLokiClient) WithOrgID(orgID string) *HTTPLokiClient {
	c.orgID = orgID
	return c
}

// setOrgID 按需补充多租户头。
func (c *HTTPLokiClient) setOrgID(req *http.Request) {
	if c.orgID != "" {
		req.Header.Set("X-Scope-OrgID", c.orgID)
	}
}

// QueryRange 区间查询日志。
func (c *HTTPLokiClient) QueryRange(ctx context.Context, query string, start, end time.Time, limit int, direction string) ([]LogEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	if direction == "" {
		direction = "backward"
	}

	params := url.Values{}
	params.Set("query", query)
	params.Set("start", fmt.Sprintf("%d", start.UnixNano()))
	params.Set("end", fmt.Sprintf("%d", end.UnixNano()))
	params.Set("limit", strconv.Itoa(limit))
	params.Set("direction", direction)

	reqURL := fmt.Sprintf("%s/loki/api/v1/query_range?%s", c.baseURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	c.setOrgID(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Loki 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Loki 返回非 200: %d", resp.StatusCode)
	}

	var result LokiQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析 Loki 响应失败: %w", err)
	}

	if result.Status != "success" {
		return nil, fmt.Errorf("Loki 查询失败: status=%s", result.Status)
	}

	return convertStreamsToEntries(result.Data.Streams, direction), nil
}

// Query 瞬时查询（最新 N 条）。
func (c *HTTPLokiClient) Query(ctx context.Context, query string, limit int) ([]LogEntry, error) {
	if limit <= 0 {
		limit = 100
	}

	params := url.Values{}
	params.Set("query", query)
	params.Set("limit", strconv.Itoa(limit))

	reqURL := fmt.Sprintf("%s/loki/api/v1/query?%s", c.baseURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	c.setOrgID(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Loki 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Loki 返回非 200: %d", resp.StatusCode)
	}

	var result LokiQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析 Loki 响应失败: %w", err)
	}

	if result.Status != "success" {
		return nil, fmt.Errorf("Loki 查询失败: status=%s", result.Status)
	}

	return convertStreamsToEntries(result.Data.Streams, "backward"), nil
}

// Count 统计指定时间范围内的日志条数。
//
// T4-16 修正：旧实现用 query_range + 固定 step=1h，整窗口 range vector 在每个步点
// 重复求值，N 个步点会把同一批日志累计 N 倍。改为 instant query：time=end、
// range=end-start 精确向上取整到秒，只在 end 处求值一次。
func (c *HTTPLokiClient) Count(ctx context.Context, query string, start, end time.Time) (int, error) {
	countQuery := fmt.Sprintf("count_over_time(%s[%s])", query, exactRangeLabel(end.Sub(start)))

	params := url.Values{}
	params.Set("query", countQuery)
	params.Set("time", fmt.Sprintf("%d", end.UnixNano()))

	resp, err := c.doVector(ctx, params)
	if err != nil {
		return 0, err
	}

	// sum 不分组时只有一条序列；count_over_time 不 sum 时每 stream 一条，两种都做累加兜底。
	total := 0
	for _, vec := range resp.Data.Vectors {
		if len(vec.Values) > 0 {
			if n, err := strconv.Atoi(strings.TrimSpace(vec.Values[0][1])); err == nil {
				total += n
			}
		}
	}
	return total, nil
}

// CountGrouped 统计指定时间范围内按标签分组的日志条数。
//
// 一次 instant 查询：sum by (<label>)(count_over_time({...}[range]))，time=end。
func (c *HTTPLokiClient) CountGrouped(ctx context.Context, query, groupLabel string, start, end time.Time) (map[string]int, error) {
	if !labelGroupWhitelist[groupLabel] {
		return nil, fmt.Errorf("不支持的分组标签: %s", groupLabel)
	}

	countQuery := fmt.Sprintf("sum by (%s)(count_over_time(%s[%s]))",
		groupLabel, query, exactRangeLabel(end.Sub(start)))

	params := url.Values{}
	params.Set("query", countQuery)
	params.Set("time", fmt.Sprintf("%d", end.UnixNano()))

	resp, err := c.doVector(ctx, params)
	if err != nil {
		return nil, err
	}

	out := make(map[string]int)
	for _, vec := range resp.Data.Vectors {
		if len(vec.Values) == 0 {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(vec.Values[0][1]))
		if err != nil {
			continue
		}
		out[vec.Metric[groupLabel]] += n
	}
	return out, nil
}

// Series 查询时间窗口内出现过的标签组合。
func (c *HTTPLokiClient) Series(ctx context.Context, matchers []string, start, end time.Time) ([]map[string]string, error) {
	params := url.Values{}
	for _, m := range matchers {
		params.Add("match[]", m)
	}
	params.Set("start", fmt.Sprintf("%d", start.UnixNano()))
	params.Set("end", fmt.Sprintf("%d", end.UnixNano()))

	reqURL := fmt.Sprintf("%s/loki/api/v1/series?%s", c.baseURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	c.setOrgID(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Loki 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Loki 返回非 200: %d", resp.StatusCode)
	}

	var result LokiSeriesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析 Loki 响应失败: %w", err)
	}
	if result.Status != "success" {
		return nil, fmt.Errorf("Loki series 查询失败: status=%s", result.Status)
	}
	return result.Data, nil
}

// doVector 执行 instant 向量查询并返回解析后的响应。
func (c *HTTPLokiClient) doVector(ctx context.Context, params url.Values) (*LokiVectorResponse, error) {
	reqURL := fmt.Sprintf("%s/loki/api/v1/query?%s", c.baseURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	c.setOrgID(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Loki 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Loki 返回非 200: %d", resp.StatusCode)
	}

	var result LokiVectorResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析 Loki 响应失败: %w", err)
	}
	if result.Status != "success" {
		return nil, fmt.Errorf("Loki 查询失败: status=%s", result.Status)
	}
	return &result, nil
}

// convertStreamsToEntries 将 Loki 的 streams 格式转为扁平的日志条目列表。
//
// 注意：同一 stream 的 labels 内容只读共享同一份 map，避免为每条日志重复分配；
// 调用方不得修改返回条目上的 Labels。
func convertStreamsToEntries(streams []LogStream, direction string) []LogEntry {
	var entries []LogEntry

	for _, stream := range streams {
		labels := make(map[string]string, len(stream.Stream))
		for k, v := range stream.Stream {
			labels[k] = v
		}

		for _, v := range stream.Values {
			if len(v) < 2 {
				continue
			}

			// Loki 的时间戳是纳秒字符串
			nanoTs, err := strconv.ParseInt(v[0], 10, 64)
			if err != nil {
				continue
			}

			entries = append(entries, LogEntry{
				Timestamp: nanoTs / 1_000_000, // 纳秒 → 毫秒
				Line:      v[1],
				Labels:    labels,
			})
		}
	}

	// 与查询方向保持一致：backward 新的在前（倒序），forward 旧的在前（正序）。
	sortEntries(entries, direction)

	return entries
}

// sortEntries 按时间戳排序，同毫秒用日志内容做稳定 tie-breaker，
// 保证游标翻页边界确定（T4-16 替代旧 O(n²) 冒泡）。
func sortEntries(entries []LogEntry, direction string) {
	less := func(i, j int) bool {
		if entries[i].Timestamp != entries[j].Timestamp {
			return entries[i].Timestamp > entries[j].Timestamp
		}
		return entries[i].Line > entries[j].Line
	}
	if direction == "forward" {
		less = func(i, j int) bool {
			if entries[i].Timestamp != entries[j].Timestamp {
				return entries[i].Timestamp < entries[j].Timestamp
			}
			return entries[i].Line < entries[j].Line
		}
	}
	sort.Slice(entries, less)
}

// exactRangeLabel 格式化 duration 为 Loki range 标签。
//
// T4-16 修正：旧 durationLabel 对不足 1 分钟的窗口直接返回 "1m"（欠计），
// 对小时/分钟一律向下取整（短于实际窗口时漏数）。这里统一向上取整到秒，
// 保证 range 不短于真实窗口：统计只可能包含窗口内数据，且不会因取整缩短漏计。
func exactRangeLabel(d time.Duration) string {
	if d <= time.Second {
		return "1s"
	}
	secs := (d + time.Second - 1) / time.Second
	return fmt.Sprintf("%ds", int64(secs))
}
