// 日志查询服务单元测试（T1-17 / T4-16）。
package logquery

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// ========== Mock Loki 客户端 ==========

type fakeLokiClient struct {
	queryRangeData []LogEntry
	queryData      []LogEntry
	countResult    int
	groupedResult  map[string]int   // CountGrouped 固定返回
	groupedSeq     []map[string]int // 可选：按调用次序返回（groupedResult 为空时生效）
	groupErrSeq    []error          // 可选：按调用次序返回错误（优先于 groupedSeq）
	seriesResult   []map[string]string
	seriesErr      error

	lastQuery      string
	lastLimit      int
	lastGroupLabel string
	lastMatchers   []string
	countCalls     int
	groupedCalls   int
}

func (f *fakeLokiClient) QueryRange(_ context.Context, query string, _, _ time.Time, limit int, _ string) ([]LogEntry, error) {
	f.lastQuery = query
	f.lastLimit = limit
	return f.queryRangeData, nil
}

func (f *fakeLokiClient) Query(_ context.Context, query string, limit int) ([]LogEntry, error) {
	f.lastQuery = query
	f.lastLimit = limit
	return f.queryData, nil
}

func (f *fakeLokiClient) Count(_ context.Context, query string, _, _ time.Time) (int, error) {
	f.lastQuery = query
	f.countCalls++
	return f.countResult, nil
}

func (f *fakeLokiClient) CountGrouped(_ context.Context, query, groupLabel string, _, _ time.Time) (map[string]int, error) {
	f.lastQuery = query
	f.lastGroupLabel = groupLabel
	idx := f.groupedCalls
	f.groupedCalls++

	// 按调用次序注入错误/结果，便于模拟"第一次成功、第二次失败"
	if idx < len(f.groupErrSeq) && f.groupErrSeq[idx] != nil {
		return nil, f.groupErrSeq[idx]
	}
	if f.groupedResult != nil {
		return f.groupedResult, nil
	}
	if idx < len(f.groupedSeq) && f.groupedSeq[idx] != nil {
		return f.groupedSeq[idx], nil
	}
	return map[string]int{}, nil
}

func (f *fakeLokiClient) Series(_ context.Context, matchers []string, _, _ time.Time) ([]map[string]string, error) {
	f.lastMatchers = matchers
	if f.seriesErr != nil {
		return nil, f.seriesErr
	}
	return f.seriesResult, nil
}

// ========== LogQL 构建测试 ==========

func TestBuildLogQuery_SandboxOnly(t *testing.T) {
	svc := NewService(nil)

	req := LogQueryRequest{SandboxID: "sandbox-123"}
	query := svc.buildLogQuery(req)

	expected := `{sandbox_id="sandbox-123"}`
	if query != expected {
		t.Errorf("期望 %s, got %s", expected, query)
	}
}

func TestBuildLogQuery_WithService(t *testing.T) {
	svc := NewService(nil)

	req := LogQueryRequest{
		SandboxID: "sandbox-123",
		Service:   "order-service",
	}
	query := svc.buildLogQuery(req)

	if !containsStr(query, `sandbox_id="sandbox-123"`) {
		t.Errorf("应包含 sandbox_id, got %s", query)
	}
	if !containsStr(query, `service="order-service"`) {
		t.Errorf("应包含 service, got %s", query)
	}
}

func TestBuildLogQuery_WithLevel(t *testing.T) {
	svc := NewService(nil)

	req := LogQueryRequest{
		SandboxID: "sandbox-123",
		Level:     LevelError,
	}
	query := svc.buildLogQuery(req)

	if !containsStr(query, `level="ERROR"`) {
		t.Errorf("应包含 level=ERROR, got %s", query)
	}
}

func TestBuildLogQuery_LevelCaseInsensitive(t *testing.T) {
	svc := NewService(nil)

	// "error" 小写 + 前后空白，应归一为 ERROR
	req := LogQueryRequest{SandboxID: "s1", Level: LogLevel(" error ")}
	query := svc.buildLogQuery(req)
	if !containsStr(query, `level="ERROR"`) {
		t.Errorf("小写级别应归一为 ERROR, got %s", query)
	}
}

func TestBuildLogQuery_InvalidLevelDropped(t *testing.T) {
	svc := NewService(nil)

	// 非法级别必须被丢弃，不能拼进 LogQL
	req := LogQueryRequest{SandboxID: "s1", Level: LogLevel(`BOGUS"} |= "evil`)}
	query := svc.buildLogQuery(req)
	if containsStr(query, "BOGUS") || containsStr(query, "level=") {
		t.Errorf("非法级别应被忽略, got %s", query)
	}
}

func TestBuildLogQuery_WithKeyword(t *testing.T) {
	svc := NewService(nil)

	req := LogQueryRequest{
		SandboxID: "sandbox-123",
		Keyword:   "NullPointerException",
	}
	query := svc.buildLogQuery(req)

	expected := `{sandbox_id="sandbox-123"} |= "NullPointerException"`
	if query != expected {
		t.Errorf("期望 %s, got %s", expected, query)
	}
}

func TestBuildLogQuery_EscapeKeyword(t *testing.T) {
	svc := NewService(nil)

	// 同时含反斜杠和双引号：必须先转义反斜杠再转义引号
	req := LogQueryRequest{SandboxID: "s1", Keyword: `a"b\c`}
	query := svc.buildLogQuery(req)

	expected := `{sandbox_id="s1"} |= "a\"b\\c"`
	if query != expected {
		t.Errorf("期望 %s, got %s", expected, query)
	}
}

func TestBuildLogQuery_EscapeLabelValue(t *testing.T) {
	svc := NewService(nil)

	req := LogQueryRequest{
		SandboxID: `s"1\x`,
		Service:   `svc"a\b`,
	}
	query := svc.buildLogQuery(req)

	if !containsStr(query, `sandbox_id="s\"1\\x"`) {
		t.Errorf("sandbox_id 未正确转义, got %s", query)
	}
	if !containsStr(query, `service="svc\"a\\b"`) {
		t.Errorf("service 未正确转义, got %s", query)
	}
}

func TestBuildLogQuery_FullFilter(t *testing.T) {
	svc := NewService(nil)

	req := LogQueryRequest{
		SandboxID: "sandbox-123",
		Service:   "user-service",
		Level:     LevelError,
		Keyword:   "timeout",
	}
	query := svc.buildLogQuery(req)

	if !containsStr(query, `sandbox_id="sandbox-123"`) {
		t.Error("缺少 sandbox_id")
	}
	if !containsStr(query, `service="user-service"`) {
		t.Error("缺少 service")
	}
	if !containsStr(query, `level="ERROR"`) {
		t.Error("缺少 level")
	}
	if !containsStr(query, `|= "timeout"`) {
		t.Error("缺少关键词过滤")
	}
}

// ========== 服务层测试 ==========

func TestService_QueryLogs_Basic(t *testing.T) {
	fake := &fakeLokiClient{
		queryRangeData: []LogEntry{
			{Timestamp: 2000, Line: "ERROR something failed", Labels: map[string]string{"service": "order-service", "level": "ERROR"}},
			{Timestamp: 1000, Line: "INFO request processed", Labels: map[string]string{"service": "user-service", "level": "INFO"}},
		},
	}

	svc := NewService(fake)

	result, err := svc.QueryLogs(context.Background(), LogQueryRequest{
		SandboxID: "sandbox-123",
		Limit:     10,
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Total != 2 {
		t.Errorf("应有 2 条日志, got %d", result.Total)
	}
	if result.HasMore {
		t.Error("不应有更多")
	}
	// 时间倒序，2000 在前
	if result.Entries[0].Timestamp != 2000 {
		t.Errorf("第一条时间戳应为 2000, got %d", result.Entries[0].Timestamp)
	}
}

func TestService_QueryLogs_LimitClamp(t *testing.T) {
	fake := &fakeLokiClient{}
	svc := NewService(fake)

	// 超过硬顶的 limit 应被压到 1000，实际查询 Loki 时多查一条
	_, err := svc.QueryLogs(context.Background(), LogQueryRequest{
		SandboxID: "s1",
		Limit:     50000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if fake.lastLimit != maxLimitCap+1 {
		t.Errorf("超限 limit 应被硬顶为 %d（Loki 实查 %d）, got %d",
			maxLimitCap, maxLimitCap+1, fake.lastLimit)
	}
}

func TestService_QueryLogs_SetMaxLimit(t *testing.T) {
	fake := &fakeLokiClient{}
	svc := NewService(fake)
	svc.SetMaxLimit(50)

	_, _ = svc.QueryLogs(context.Background(), LogQueryRequest{SandboxID: "s1", Limit: 100})
	if fake.lastLimit != 51 {
		t.Errorf("自定义硬顶后实查应为 51, got %d", fake.lastLimit)
	}

	// 非正数配置应被忽略
	svc.SetMaxLimit(0)
	_, _ = svc.QueryLogs(context.Background(), LogQueryRequest{SandboxID: "s1", Limit: 100})
	if fake.lastLimit != 51 {
		t.Errorf("SetMaxLimit(0) 不应改变硬顶, got %d", fake.lastLimit)
	}
}

func TestService_QueryLogs_HasMore(t *testing.T) {
	// 生成 5 条数据，limit=3，应有更多
	entries := make([]LogEntry, 5)
	for i := 0; i < 5; i++ {
		entries[i] = LogEntry{
			Timestamp: int64(5000 - i*1000),
			Line:      fmt.Sprintf("log line %d", i),
			Labels:    map[string]string{"service": "test-service"},
		}
	}

	fake := &fakeLokiClient{queryRangeData: entries}
	svc := NewService(fake)

	result, err := svc.QueryLogs(context.Background(), LogQueryRequest{
		SandboxID: "sandbox-123",
		Limit:     3,
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Total != 3 {
		t.Errorf("应返回 3 条, got %d", result.Total)
	}
	if !result.HasMore {
		t.Error("应有更多")
	}
	// 查询时应该多查 1 条（limit + 1）
	if fake.lastLimit != 4 {
		t.Errorf("实际查询 limit 应为 4, got %d", fake.lastLimit)
	}
	// 末条为 backward 序列第 3 条：时间戳 3000，游标可解码还原
	ts, line, ok := decodeCursor(result.NextCursor)
	if !ok {
		t.Fatal("next_cursor 应可解码")
	}
	if ts != 3000 || line != "log line 2" {
		t.Errorf("游标边界应为 (3000, 'log line 2'), got (%d, %q)", ts, line)
	}
}

func TestService_QueryLogs_CursorBackward(t *testing.T) {
	// 模拟 Loki 按 end=游标毫秒 返回的数据：包含游标同毫秒 3 条（须剔除）
	// 与更旧的 2 条。
	cursor := encodeCursor(3000, "b")
	fake := &fakeLokiClient{
		queryRangeData: []LogEntry{
			// backward 顺序（新→旧）
			{Timestamp: 3000, Line: "c"}, // 同毫秒且比游标新 → 剔除
			{Timestamp: 3000, Line: "b"}, // 游标本身 → 剔除
			{Timestamp: 3000, Line: "a"}, // 同毫秒但比游标旧 → 保留
			{Timestamp: 2000, Line: "x"},
			{Timestamp: 1000, Line: "y"},
		},
	}
	svc := NewService(fake)

	result, err := svc.QueryLogs(context.Background(), LogQueryRequest{
		SandboxID: "s1",
		Limit:     10,
		Cursor:    cursor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 3 {
		t.Fatalf("应剔除游标及其之后条目，保留 3 条, got %d", result.Total)
	}
	if result.Entries[0].Timestamp != 3000 || result.Entries[0].Line != "a" {
		t.Errorf("首条应为 (3000,a), got (%d,%s)", result.Entries[0].Timestamp, result.Entries[0].Line)
	}
}

func TestService_QueryLogs_InvalidCursorIgnored(t *testing.T) {
	fake := &fakeLokiClient{
		queryRangeData: []LogEntry{
			{Timestamp: 1000, Line: "only", Labels: map[string]string{"service": "s"}},
		},
	}
	svc := NewService(fake)

	result, err := svc.QueryLogs(context.Background(), LogQueryRequest{
		SandboxID: "s1",
		Limit:     10,
		Cursor:    "@@@not-base64@@@", // '@' 不在 base64url 字符表中，必定解码失败
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 {
		t.Errorf("非法游标应按首页处理，返回 1 条, got %d", result.Total)
	}
}

func TestService_QueryLogs_MissingSandboxID(t *testing.T) {
	svc := NewService(nil)

	_, err := svc.QueryLogs(context.Background(), LogQueryRequest{
		SandboxID: "",
	})
	if err == nil {
		t.Error("缺少 sandbox_id 应返回错误")
	}
}

func TestService_GetLatestLogs(t *testing.T) {
	fake := &fakeLokiClient{
		queryRangeData: []LogEntry{
			{Timestamp: 3000, Line: "latest log", Labels: map[string]string{"service": "user-service"}},
		},
	}

	svc := NewService(fake)

	result, err := svc.GetLatestLogs(context.Background(), "sandbox-123", "user-service", 10)
	if err != nil {
		t.Fatal(err)
	}

	if result.Total != 1 {
		t.Errorf("应有 1 条, got %d", result.Total)
	}
	if !containsStr(fake.lastQuery, `sandbox_id="sandbox-123"`) {
		t.Errorf("查询应包含 sandbox_id, got %s", fake.lastQuery)
	}
}

func TestService_GetLogStats(t *testing.T) {
	fake := &fakeLokiClient{
		groupedResult: map[string]int{
			"ERROR": 12,
			"WARN":  8,
			"INFO":  80,
			"DEBUG": 5,
		},
	}
	svc := NewService(fake)

	stats, err := svc.GetLogStats(context.Background(), "sandbox-123",
		time.Now().Add(-1*time.Hour), time.Now())
	if err != nil {
		t.Fatal(err)
	}

	// 一次 level 分组 + 一次 service 分组
	if fake.groupedCalls != 2 {
		t.Errorf("应发 2 次分组查询（level+service）, got %d", fake.groupedCalls)
	}
	if fake.lastGroupLabel != "service" {
		t.Errorf("第二次分组应为 service, got %s", fake.lastGroupLabel)
	}
	// 总数为全部级别之和（含 DEBUG）
	if stats.TotalCount != 105 {
		t.Errorf("总数应为 105, got %d", stats.TotalCount)
	}
	if stats.ErrorCount != 12 {
		t.Errorf("错误数应为 12, got %d", stats.ErrorCount)
	}
	if stats.WarnCount != 8 {
		t.Errorf("警告数应为 8, got %d", stats.WarnCount)
	}
	if stats.InfoCount != 80 {
		t.Errorf("信息数应为 80, got %d", stats.InfoCount)
	}
	if stats.TimeRange == "" {
		t.Error("时间范围不应为空")
	}
}

func TestService_GetLogStats_LevelQueryError(t *testing.T) {
	fake := &fakeLokiClient{groupErrSeq: []error{fmt.Errorf("loki down")}}
	svc := NewService(fake)

	_, err := svc.GetLogStats(context.Background(), "s1", time.Now().Add(-time.Hour), time.Now())
	if err == nil {
		t.Error("level 分组查询失败应返回错误")
	}
}

func TestService_GetLogStats_ServiceQueryFailureDegraded(t *testing.T) {
	// 第一次（level）正常，第二次（service）失败：统计接口应降级而非整体报错。
	fake := &fakeLokiClient{
		groupedSeq:  []map[string]int{{"INFO": 3}, nil},
		groupErrSeq: []error{nil, fmt.Errorf("loki down")},
	}
	svc := NewService(fake)

	stats, err := svc.GetLogStats(context.Background(), "s1", time.Now().Add(-time.Hour), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// service 分组失败时 ByService 保持空 map，级别统计仍可用
	if stats.TotalCount != 3 {
		t.Errorf("级别统计应仍可用, got %d", stats.TotalCount)
	}
	if stats.ByService == nil {
		t.Error("ByService 应初始化为空 map")
	}
}

func TestService_TailLogs(t *testing.T) {
	fake := &fakeLokiClient{
		queryRangeData: []LogEntry{
			{Timestamp: 1000, Line: "log 1", Labels: map[string]string{"service": "order-service"}},
			{Timestamp: 2000, Line: "log 2", Labels: map[string]string{"service": "order-service"}},
			{Timestamp: 3000, Line: "log 3", Labels: map[string]string{"service": "order-service"}},
		},
	}

	svc := NewService(fake)

	since := time.UnixMilli(500)
	result, err := svc.TailLogs(context.Background(), "sandbox-123", "order-service", since, 50)
	if err != nil {
		t.Fatal(err)
	}

	if result.Total != 3 {
		t.Errorf("应有 3 条, got %d", result.Total)
	}
}

// ========== ListServices 测试 ==========

func TestService_ListServices(t *testing.T) {
	fake := &fakeLokiClient{
		seriesResult: []map[string]string{
			{"sandbox_id": "s1", "service": "user-service"},
			{"sandbox_id": "s1", "service": "order-service"},
			{"sandbox_id": "s1", "service": "user-service"}, // 重复
			{"sandbox_id": "s1"},                            // 无 service 标签
		},
	}
	svc := NewService(fake)

	services, err := svc.ListServices(context.Background(), "s1",
		time.Now().Add(-time.Hour), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(services) != 2 {
		t.Fatalf("应去重得到 2 个服务, got %d (%v)", len(services), services)
	}
	// 应排序：order-service 在前
	if services[0] != "order-service" || services[1] != "user-service" {
		t.Errorf("服务应按名排序, got %v", services)
	}
	if len(fake.lastMatchers) != 1 || !containsStr(fake.lastMatchers[0], `sandbox_id="s1"`) {
		t.Errorf("应以 sandbox_id 为 matcher, got %v", fake.lastMatchers)
	}
}

func TestService_ListServices_MissingSandboxID(t *testing.T) {
	svc := NewService(&fakeLokiClient{})
	if _, err := svc.ListServices(context.Background(), "", time.Time{}, time.Time{}); err == nil {
		t.Error("缺少 sandbox_id 应返回错误")
	}
}

// ========== 工具函数测试 ==========

func TestExtractServices(t *testing.T) {
	entries := []LogEntry{
		{Labels: map[string]string{"service": "user-service"}},
		{Labels: map[string]string{"service": "order-service"}},
		{Labels: map[string]string{"service": "user-service"}}, // 重复
		{Labels: map[string]string{"service": "product-service"}},
		{Labels: map[string]string{"other": "value"}}, // 没有 service
	}

	services := extractServices(entries)

	if len(services) != 3 {
		t.Errorf("应有 3 个不同服务, got %d", len(services))
	}
}

func TestNormalizeLevel(t *testing.T) {
	cases := []struct {
		in   LogLevel
		want LogLevel
	}{
		{LevelError, LevelError},
		{LogLevel("error"), LevelError},
		{LogLevel(" warn "), LevelWarning},
		{LogLevel("Info"), LevelInfo},
		{LogLevel("fatal"), LevelFatal},
		{LogLevel("debug"), LevelDebug},
		{LogLevel(""), ""},
		{LogLevel("TRACE"), ""}, // 白名单外
	}
	for _, c := range cases {
		if got := normalizeLevel(c.in); got != c.want {
			t.Errorf("normalizeLevel(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}

func TestEscapeLabelValue(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`plain`, `plain`},
		{`a"b`, `a\"b`},
		{`a\b`, `a\\b`},
		// 关键顺序：反斜杠先转义，原引号转义产生的反斜杠不会被二次处理
		{`a"b\c`, `a\"b\\c`},
	}
	for _, c := range cases {
		if got := escapeLabelValue(c.in); got != c.want {
			t.Errorf("escapeLabelValue(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}

func TestCompareEntry(t *testing.T) {
	if compareEntry(1, "a", 2, "z") != -1 {
		t.Error("(1,a) 应早于 (2,z)")
	}
	if compareEntry(2, "a", 1, "z") != 1 {
		t.Error("(2,a) 应晚于 (1,z)")
	}
	if compareEntry(1, "a", 1, "a") != 0 {
		t.Error("相同键应相等")
	}
	if compareEntry(1, "a", 1, "b") != -1 {
		t.Error("同毫秒 a 应早于 b")
	}
}

func TestCursorRoundTrip(t *testing.T) {
	cases := []struct {
		ts   int64
		line string
	}{
		{1700000000000, "simple line"},
		{42, "包含中文与|管道符"},
		{0, ""},
	}
	for _, c := range cases {
		cursor := encodeCursor(c.ts, c.line)
		ts, line, ok := decodeCursor(cursor)
		if !ok {
			t.Fatalf("游标应可解码: %s", cursor)
		}
		if ts != c.ts || line != c.line {
			t.Errorf("往返不一致: got (%d,%q), want (%d,%q)", ts, line, c.ts, c.line)
		}
	}
}

func TestExactRangeLabel(t *testing.T) {
	cases := []struct {
		duration time.Duration
		expected string
	}{
		{2 * time.Hour, "7200s"},
		{30 * time.Minute, "1800s"},
		{90 * time.Second, "90s"},
		{30 * time.Second, "30s"},
		{500 * time.Millisecond, "1s"},  // 不足 1 秒兜底 1s
		{1500 * time.Millisecond, "2s"}, // 向上取整
	}

	for _, c := range cases {
		result := exactRangeLabel(c.duration)
		if result != c.expected {
			t.Errorf("exactRangeLabel(%v) = %s, 期望 %s", c.duration, result, c.expected)
		}
	}
}

// ========== 辅助函数 ==========

func containsStr(s, substr string) bool {
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
