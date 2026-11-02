// Package logquery 日志查询服务层（T1-17 / T4-16 查询优化）。
//
// 封装 Loki 查询，提供业务友好的日志查询接口。
package logquery

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// LogLevel 日志级别。
type LogLevel string

const (
	LevelDebug   LogLevel = "DEBUG"
	LevelInfo    LogLevel = "INFO"
	LevelWarning LogLevel = "WARN"
	LevelError   LogLevel = "ERROR"
	LevelFatal   LogLevel = "FATAL"
)

// QueryDirection 查询方向。
type QueryDirection string

const (
	DirectionBackward QueryDirection = "backward" // 从新到旧（默认）
	DirectionForward  QueryDirection = "forward"  // 从旧到新
)

const (
	// defaultLimit 未指定 limit 时的默认返回条数。
	defaultLimit = 200
	// maxLimitCap limit 硬顶的兜底值，防止调用方一次拉取过多拖垮 Loki。
	maxLimitCap = 1000
)

// LogQueryRequest 日志查询请求。
type LogQueryRequest struct {
	SandboxID string         // 沙箱 ID（必填）
	Service   string         // 服务名（可选，空=所有服务）
	Level     LogLevel       // 日志级别（可选，空=所有级别，非法值将被忽略）
	Keyword   string         // 关键词过滤（可选）
	StartTime time.Time      // 开始时间
	EndTime   time.Time      // 结束时间
	Limit     int            // 返回条数上限，默认 200，硬顶 1000（可经 SetMaxLimit 配置）
	Direction QueryDirection // 查询方向，默认 backward
	Cursor    string         // 上一页返回的游标 NextCursor，首页不传
}

// LogQueryResult 日志查询结果。
type LogQueryResult struct {
	Total      int        `json:"total"`       // 实际返回条数
	HasMore    bool       `json:"has_more"`    // 是否还有更多
	Entries    []LogEntry `json:"entries"`     // 日志条目
	Services   []string   `json:"services"`    // 涉及的服务列表
	NextCursor string     `json:"next_cursor"` // 下一页游标，has_more=true 时有值
}

// LogStats 日志统计信息。
type LogStats struct {
	TotalCount int            `json:"total_count"` // 总日志数
	ErrorCount int            `json:"error_count"` // ERROR 级别数量
	WarnCount  int            `json:"warn_count"`  // WARN 级别数量
	InfoCount  int            `json:"info_count"`  // INFO 级别数量
	ByService  map[string]int `json:"by_service"`  // 按服务统计
	TimeRange  string         `json:"time_range"`  // 时间范围描述
}

// Service 日志查询服务。
type Service struct {
	loki         LokiClient
	defaultLimit int
	maxLimit     int
}

// NewService 创建日志查询服务。
func NewService(loki LokiClient) *Service {
	return &Service{
		loki:         loki,
		defaultLimit: defaultLimit,
		maxLimit:     maxLimitCap,
	}
}

// SetMaxLimit 配置单次查询返回条数硬顶（<=0 时忽略）。
//
// 供装配层注入 observability.log_max_limit。
func (s *Service) SetMaxLimit(n int) {
	if n > 0 {
		s.maxLimit = n
	}
}

// QueryLogs 分页查询日志。
//
// 支持按服务、级别、关键词、时间范围过滤，以及基于 (时间戳, 日志内容)
// 的不透明游标翻页（T4-16）。
func (s *Service) QueryLogs(ctx context.Context, req LogQueryRequest) (*LogQueryResult, error) {
	if req.SandboxID == "" {
		return nil, fmt.Errorf("sandbox_id 不能为空")
	}

	// 设置默认值
	now := time.Now()
	if req.StartTime.IsZero() {
		req.StartTime = now.Add(-1 * time.Hour) // 默认 1 小时
	}
	if req.EndTime.IsZero() {
		req.EndTime = now
	}
	if req.Limit <= 0 {
		req.Limit = s.defaultLimit
	}
	if req.Limit > s.maxLimit {
		req.Limit = s.maxLimit
	}
	if req.Direction != DirectionForward {
		req.Direction = DirectionBackward
	}

	// 游标翻页：用上一页边界 (ts, line) 收紧时间窗口，
	// 同毫秒条目靠 (ts, line) 比较在服务层剔除。
	backward := req.Direction == DirectionBackward
	if req.Cursor != "" {
		if cursorTs, _, ok := decodeCursor(req.Cursor); ok {
			cursorTime := time.UnixMilli(cursorTs)
			if backward && req.EndTime.After(cursorTime) {
				req.EndTime = cursorTime
			}
			if !backward && req.StartTime.Before(cursorTime) {
				req.StartTime = cursorTime
			}
		}
	}

	// 构建 LogQL 查询表达式
	query := s.buildLogQuery(req)

	// 多查一条，判断是否还有更多
	queryLimit := req.Limit + 1

	entries, err := s.loki.QueryRange(ctx, query, req.StartTime, req.EndTime, queryLimit, string(req.Direction))
	if err != nil {
		return nil, fmt.Errorf("查询 Loki 失败: %w", err)
	}

	// 剔除游标边界及之前的条目（LogQL 无法按日志内容做严格比较）。
	if req.Cursor != "" {
		if cursorTs, cursorLine, ok := decodeCursor(req.Cursor); ok {
			filtered := entries[:0]
			for _, e := range entries {
				cmp := compareEntry(e.Timestamp, e.Line, cursorTs, cursorLine)
				if backward && cmp < 0 {
					filtered = append(filtered, e)
				}
				if !backward && cmp > 0 {
					filtered = append(filtered, e)
				}
			}
			entries = filtered
		}
	}

	// 判断是否有更多
	hasMore := len(entries) > req.Limit
	nextCursor := ""
	if hasMore {
		entries = entries[:req.Limit]
		last := entries[req.Limit-1]
		nextCursor = encodeCursor(last.Timestamp, last.Line)
	}

	// 提取服务列表
	services := extractServices(entries)

	return &LogQueryResult{
		Total:      len(entries),
		HasMore:    hasMore,
		Entries:    entries,
		Services:   services,
		NextCursor: nextCursor,
	}, nil
}

// GetLatestLogs 获取最新的 N 条日志。
//
// 用于日志面板刚打开时的初始加载。
func (s *Service) GetLatestLogs(ctx context.Context, sandboxID, service string, limit int) (*LogQueryResult, error) {
	return s.QueryLogs(ctx, LogQueryRequest{
		SandboxID: sandboxID,
		Service:   service,
		EndTime:   time.Now(),
		Limit:     limit,
		Direction: DirectionBackward,
	})
}

// GetLogStats 获取日志统计信息。
//
// T4-16：旧实现并发发 4 次 count_over_time 查询（且旧 Count 有按步点重复
// 计数的缺陷）。这里改为一次 sum by (level) 分组 instant 查询取回全部级别，
// 再发一次 sum by (service) 填充 ByService（旧代码该字段从未赋值）。
func (s *Service) GetLogStats(ctx context.Context, sandboxID string, start, end time.Time) (*LogStats, error) {
	if sandboxID == "" {
		return nil, fmt.Errorf("sandbox_id 不能为空")
	}
	if start.IsZero() {
		start = time.Now().Add(-1 * time.Hour)
	}
	if end.IsZero() {
		end = time.Now()
	}

	selector := fmt.Sprintf(`{sandbox_id="%s"}`, escapeLabelValue(sandboxID))

	byLevel, err := s.loki.CountGrouped(ctx, selector, "level", start, end)
	if err != nil {
		return nil, fmt.Errorf("统计日志级别失败: %w", err)
	}

	stats := &LogStats{
		ByService: make(map[string]int),
		TimeRange: fmt.Sprintf("%s ~ %s",
			start.Format("15:04:05"),
			end.Format("15:04:05")),
	}

	// Loki 中 level 标签大小写以采集端配置为准，这里按大写归一后映射，
	// DEBUG/FATAL 等其余级别计入总数但没有独立字段（与旧接口语义一致）。
	for level, n := range byLevel {
		stats.TotalCount += n
		switch strings.ToUpper(strings.TrimSpace(level)) {
		case string(LevelError):
			stats.ErrorCount += n
		case string(LevelWarning):
			stats.WarnCount += n
		case string(LevelInfo):
			stats.InfoCount += n
		}
	}

	// 按服务分组属于增强信息，失败时不影响级别统计接口可用性。
	if byService, err := s.loki.CountGrouped(ctx, selector, "service", start, end); err == nil {
		stats.ByService = byService
	}

	return stats, nil
}

// TailLogs 实时日志（基于轮询的 tail 模拟）。
//
// 返回指定时间点之后的新日志。
// 前端可以用 WebSocket 或轮询调用此接口实现实时日志流。
func (s *Service) TailLogs(ctx context.Context, sandboxID, service string, since time.Time, limit int) (*LogQueryResult, error) {
	return s.QueryLogs(ctx, LogQueryRequest{
		SandboxID: sandboxID,
		Service:   service,
		StartTime: since,
		EndTime:   time.Now(),
		Limit:     limit,
		Direction: DirectionForward, // 从旧到新，便于前端追加
	})
}

// Count 统计指定时间范围内的日志条数（精确计数，不受 limit 截断影响）。
//
// service/level 为空时不过滤对应维度。供 logviewer/recovery 适配器复用。
func (s *Service) Count(ctx context.Context, sandboxID, service string, level LogLevel, start, end time.Time) (int, error) {
	if sandboxID == "" {
		return 0, fmt.Errorf("sandbox_id 不能为空")
	}
	if start.IsZero() {
		start = time.Now().Add(-1 * time.Hour)
	}
	if end.IsZero() {
		end = time.Now()
	}

	query := s.buildLogQuery(LogQueryRequest{
		SandboxID: sandboxID,
		Service:   service,
		Level:     level,
		StartTime: start,
		EndTime:   end,
	})
	return s.loki.Count(ctx, query, start, end)
}

// CountKeyword 统计指定时间范围内包含关键词的日志条数。
//
// 供告警日志关键词型规则（service.AlertLogSource）使用。
func (s *Service) CountKeyword(ctx context.Context, sandboxID, service, keyword string, level LogLevel, start, end time.Time) (int, error) {
	if sandboxID == "" {
		return 0, fmt.Errorf("sandbox_id 不能为空")
	}
	if keyword == "" {
		return 0, fmt.Errorf("keyword 不能为空")
	}
	if start.IsZero() {
		start = time.Now().Add(-1 * time.Hour)
	}
	if end.IsZero() {
		end = time.Now()
	}

	query := s.buildLogQuery(LogQueryRequest{
		SandboxID: sandboxID,
		Service:   service,
		Level:     level,
		Keyword:   keyword,
		StartTime: start,
		EndTime:   end,
	})
	return s.loki.Count(ctx, query, start, end)
}

// ListServices 返回时间窗口内该沙箱出现过日志的服务名列表（T4-16）。
//
// 走 Loki /series 端点按标签查询，替代旧 services 接口"拉 50 条日志再提
// service 名"的做法（漏服务、浪费带宽）。
func (s *Service) ListServices(ctx context.Context, sandboxID string, start, end time.Time) ([]string, error) {
	if sandboxID == "" {
		return nil, fmt.Errorf("sandbox_id 不能为空")
	}
	if start.IsZero() {
		start = time.Now().Add(-1 * time.Hour)
	}
	if end.IsZero() {
		end = time.Now()
	}

	matcher := fmt.Sprintf(`{sandbox_id="%s"}`, escapeLabelValue(sandboxID))
	seriesSet, err := s.loki.Series(ctx, []string{matcher}, start, end)
	if err != nil {
		return nil, fmt.Errorf("查询 Loki series 失败: %w", err)
	}

	seen := make(map[string]struct{})
	services := make([]string, 0)
	for _, labels := range seriesSet {
		svc := labels["service"]
		if svc == "" {
			continue
		}
		if _, ok := seen[svc]; ok {
			continue
		}
		seen[svc] = struct{}{}
		services = append(services, svc)
	}
	sort.Strings(services)
	return services, nil
}

// CountGroupedByService 按服务分组统计日志条数。
//
// 供 logviewer 适配器（LogProvider.CountGrouped）转发使用，
// 与 GetLogStats 内部实现同源，避免适配器另造查询。
func (s *Service) CountGroupedByService(ctx context.Context, sandboxID string, start, end time.Time) (map[string]int, error) {
	if sandboxID == "" {
		return nil, fmt.Errorf("sandbox_id 不能为空")
	}
	if start.IsZero() {
		start = time.Now().Add(-1 * time.Hour)
	}
	if end.IsZero() {
		end = time.Now()
	}
	selector := fmt.Sprintf(`{sandbox_id="%s"}`, escapeLabelValue(sandboxID))
	return s.loki.CountGrouped(ctx, selector, "service", start, end)
}

// CountGroupedByLevel 按级别分组统计日志条数。
//
// 供 logviewer 适配器（LogProvider.CountGrouped）转发使用。
func (s *Service) CountGroupedByLevel(ctx context.Context, sandboxID, service string, start, end time.Time) (map[string]int, error) {
	if sandboxID == "" {
		return nil, fmt.Errorf("sandbox_id 不能为空")
	}
	if start.IsZero() {
		start = time.Now().Add(-1 * time.Hour)
	}
	if end.IsZero() {
		end = time.Now()
	}
	selector := s.buildLogQuery(LogQueryRequest{
		SandboxID: sandboxID,
		Service:   service,
		StartTime: start,
		EndTime:   end,
	})
	return s.loki.CountGrouped(ctx, selector, "level", start, end)
}

// buildLogQuery 构建 LogQL 查询表达式。
//
// 格式：{sandbox_id="xxx", service="yyy", level="ZZZ"} |= "keyword"
//
// T4-16 安全加固：所有标签值与关键词统一经 escapeLabelValue 转义；
// level 走固定白名单归一，非法值直接丢弃，避免把用户输入注入 LogQL。
func (s *Service) buildLogQuery(req LogQueryRequest) string {
	var labels []string

	// 必填：sandbox_id
	labels = append(labels, fmt.Sprintf(`sandbox_id="%s"`, escapeLabelValue(req.SandboxID)))

	// 可选：service
	if req.Service != "" {
		labels = append(labels, fmt.Sprintf(`service="%s"`, escapeLabelValue(req.Service)))
	}

	// 可选：level（白名单归一，非法值忽略）
	if level := normalizeLevel(req.Level); level != "" {
		labels = append(labels, fmt.Sprintf(`level="%s"`, level))
	}

	query := fmt.Sprintf("{%s}", strings.Join(labels, ", "))

	// 可选：关键词过滤
	if req.Keyword != "" {
		query += fmt.Sprintf(` |= "%s"`, escapeLabelValue(req.Keyword))
	}

	return query
}

// normalizeLevel 把任意大小写/空白的级别输入归一到合法常量，
// 不在白名单内的级别返回空串（调用方据此跳过 level 过滤条件）。
func normalizeLevel(level LogLevel) LogLevel {
	switch strings.ToUpper(strings.TrimSpace(string(level))) {
	case string(LevelDebug):
		return LevelDebug
	case string(LevelInfo):
		return LevelInfo
	case string(LevelWarning):
		return LevelWarning
	case string(LevelError):
		return LevelError
	case string(LevelFatal):
		return LevelFatal
	default:
		return ""
	}
}

// escapeLabelValue 转义 LogQL 双引号字符串中的特殊字符。
//
// 必须先转义反斜杠再转义引号，否则二次转义会破坏前者。
func escapeLabelValue(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	return v
}

// compareEntry 比较两个 (毫秒时间戳, 日志内容) 游标键。
//
// 返回 -1 / 0 / 1，分别表示 a 早于 / 等于 / 晚于 b。
func compareEntry(tsA int64, lineA string, tsB int64, lineB string) int {
	if tsA != tsB {
		if tsA < tsB {
			return -1
		}
		return 1
	}
	return strings.Compare(lineA, lineB)
}

// encodeCursor 把翻页边界编码为不透明游标（base64 RawURL，可安全放进 query string）。
func encodeCursor(ts int64, line string) string {
	raw := strconv.FormatInt(ts, 10) + "|" + line
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeCursor 解析游标，格式非法时返回 ok=false，调用方按首页处理。
func decodeCursor(cursor string) (ts int64, line string, ok bool) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, "", false
	}
	idx := strings.IndexByte(string(raw), '|')
	if idx < 0 {
		return 0, "", false
	}
	ts, err = strconv.ParseInt(string(raw[:idx]), 10, 64)
	if err != nil {
		return 0, "", false
	}
	return ts, string(raw[idx+1:]), true
}

// extractServices 从日志条目中提取涉及的服务列表（去重）。
func extractServices(entries []LogEntry) []string {
	seen := make(map[string]bool)
	var services []string

	for _, entry := range entries {
		svc := entry.Labels["service"]
		if svc != "" && !seen[svc] {
			seen[svc] = true
			services = append(services, svc)
		}
	}

	return services
}
