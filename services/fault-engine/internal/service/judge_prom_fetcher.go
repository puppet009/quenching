// judge_prom_fetcher.go 生产环境指标获取通道：Prometheus 瞬时查询（PromQL /api/v1/query）。
//
// 判定器（judge_evaluator.go）按 "service|metric" 对齐样本，因此本抓取器对每个逻辑指标产出：
//   - 每个服务标签值一条 JudgeMetricSample（多序列取均值）；
//   - 一条 Service="" 的整体样本（全部序列取均值），供未指定 service 的阈值使用。
//
// 逻辑指标名 -> PromQL 的映射由配置注入（metric_queries），模板支持 {{namespace}}
// 占位；未配置映射时退化为 `metric{namespace="ns"}` 的裸指标查询。
// 写法对齐 training-service/internal/pkg/metrics/prom_client.go。
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// JudgePromFetcherConfig Prometheus 抓取配置。
type JudgePromFetcherConfig struct {
	// BaseURL Prometheus 根地址，如 http://prometheus-k8s.monitoring:9090。
	BaseURL string
	// MetricQueries 逻辑指标名 -> PromQL（含 {{namespace}} 占位）。
	MetricQueries map[string]string
	// ServiceLabels 从序列标签里识别服务名时按序尝试的标签键。
	ServiceLabels []string
}

// JudgePromFetcher 基于 HTTP 的 JudgeMetricFetcher 实现。
type JudgePromFetcher struct {
	baseURL       string
	queries       map[string]string
	serviceLabels []string
	httpClient    *http.Client
}

// 编译期断言：JudgePromFetcher 满足 JudgeMetricFetcher。
var _ JudgeMetricFetcher = (*JudgePromFetcher)(nil)

// NewJudgePromFetcher 创建抓取器并补默认值。
func NewJudgePromFetcher(cfg JudgePromFetcherConfig) *JudgePromFetcher {
	labels := cfg.ServiceLabels
	if len(labels) == 0 {
		labels = []string{"service", "app", "container"}
	}
	return &JudgePromFetcher{
		baseURL:       strings.TrimRight(cfg.BaseURL, "/"),
		queries:       cfg.MetricQueries,
		serviceLabels: labels,
		httpClient:    &http.Client{Timeout: 10 * time.Second},
	}
}

// promInstantResponse Prometheus query 响应。
type promInstantResponse struct {
	Status string `json:"status"`
	Data   struct {
		Result []promInstantSeries `json:"result"`
	} `json:"data"`
	Error string `json:"error,omitempty"`
}

// promInstantSeries 同时兼容 vector（Value）与 matrix（Values）。
type promInstantSeries struct {
	Metric map[string]string `json:"metric"`
	Value  []interface{}     `json:"value,omitempty"`
	Values [][]interface{}   `json:"values,omitempty"`
}

// Fetch 拉取一批指标的当前值。单指标查询失败只返回错误，由 supervisor fail-closed
// 跳过本轮（缺数据不误判恢复）。
func (f *JudgePromFetcher) Fetch(ctx context.Context, namespace string, metrics []string) ([]JudgeMetricSample, error) {
	if f.baseURL == "" {
		return nil, fmt.Errorf("Prometheus 地址未配置")
	}

	samples := make([]JudgeMetricSample, 0, len(metrics))
	seen := make(map[string]struct{}, len(metrics))
	for _, name := range metrics {
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}

		query := f.buildQuery(name, namespace)
		series, err := f.query(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("查询指标 %s 失败: %w", name, err)
		}

		// 按服务标签分组：service -> 该服务所有序列的值（多 Pod 取均值）。
		byService := make(map[string][]float64)
		var overall []float64
		for _, s := range series {
			v, ok := latestValue(s)
			if !ok {
				continue
			}
			overall = append(overall, v)
			svc := f.serviceName(s.Metric)
			byService[svc] = append(byService[svc], v)
		}
		for svc, vals := range byService {
			samples = append(samples, JudgeMetricSample{Name: name, Service: svc, Value: avg(vals)})
		}
		// 整体样本：未配 service 的阈值（key="|name"）靠它对齐。
		if len(overall) > 0 {
			if _, hasEmpty := byService[""]; !hasEmpty {
				samples = append(samples, JudgeMetricSample{Name: name, Value: avg(overall)})
			}
		}
	}
	return samples, nil
}

// buildQuery 逻辑指标名 -> PromQL。
func (f *JudgePromFetcher) buildQuery(name, namespace string) string {
	if q, ok := f.queries[name]; ok {
		return strings.ReplaceAll(q, "{{namespace}}", namespace)
	}
	if namespace == "" {
		return name
	}
	return fmt.Sprintf(`%s{namespace="%s"}`, name, namespace)
}

// query 执行一次瞬时查询并解析序列。
func (f *JudgePromFetcher) query(ctx context.Context, promQL string) ([]promInstantSeries, error) {
	params := url.Values{}
	params.Set("query", promQL)
	params.Set("time", strconv.FormatInt(time.Now().Unix(), 10))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		f.baseURL+"/api/v1/query?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}

	resp, err := f.httpClient.Do(req)
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

	var out promInstantResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if out.Status != "success" {
		return nil, fmt.Errorf("Prometheus 查询失败: %s", out.Error)
	}
	return out.Data.Result, nil
}

// serviceName 按优先级从序列标签提取服务名，取不到返回空串。
func (f *JudgePromFetcher) serviceName(labels map[string]string) string {
	for _, key := range f.serviceLabels {
		if v := labels[key]; v != "" {
			return v
		}
	}
	return ""
}

// latestValue 取序列最新值，兼容 vector 的 Value 与 matrix 的 Values。
func latestValue(s promInstantSeries) (float64, bool) {
	if len(s.Value) == 2 {
		return parsePromValue(s.Value[1])
	}
	if n := len(s.Values); n > 0 {
		last := s.Values[n-1]
		if len(last) == 2 {
			return parsePromValue(last[1])
		}
	}
	return 0, false
}

func parsePromValue(v interface{}) (float64, bool) {
	str, ok := v.(string)
	if !ok {
		return 0, false
	}
	f, err := strconv.ParseFloat(str, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func avg(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}
