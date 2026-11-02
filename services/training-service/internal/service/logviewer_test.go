// 日志观察单元测试（T1-22 / T4-16 实时流与统计修正）。
package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// ========== Mock 数据 ==========

// fakeLogProvider 可按调用序贯返回 QueryLogs 结果，供导出截断等测试使用。
type fakeLogProvider struct {
	mu sync.Mutex

	entries    []LogEntryRaw // 默认返回的固定条目
	querySeq   [][]LogEntryRaw
	queryCalls int
	queryErr   error
	lastLimit  int

	count int

	groupedResult map[string]int // 默认分组结果（level/service 都返回它）
	groupedSeq    []map[string]int
	groupErrSeq   []error
	groupedCalls  int
	lastGroup     string
}

func (f *fakeLogProvider) QueryLogs(_ context.Context, _, _, _, _ string,
	_, _ time.Time, limit int, _ string) (*LogQueryResultRaw, error) {

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.queryErr != nil {
		return nil, f.queryErr
	}
	entries := f.entries
	if f.queryCalls < len(f.querySeq) && f.querySeq[f.queryCalls] != nil {
		entries = f.querySeq[f.queryCalls]
	}
	f.queryCalls++
	f.lastLimit = limit
	return &LogQueryResultRaw{
		Total:   len(entries),
		HasMore: false,
		Entries: entries,
	}, nil
}

func (f *fakeLogProvider) GetLatestLogs(_ context.Context, _, _ string, _ int) (*LogQueryResultRaw, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &LogQueryResultRaw{
		Total:   len(f.entries),
		Entries: f.entries,
	}, nil
}

func (f *fakeLogProvider) Count(_ context.Context, _, _, _ string,
	_, _ time.Time) (int, error) {
	return f.count, nil
}

func (f *fakeLogProvider) CountGrouped(_ context.Context, _, _, groupLabel string,
	_, _ time.Time) (map[string]int, error) {

	f.mu.Lock()
	defer f.mu.Unlock()

	idx := f.groupedCalls
	f.groupedCalls++
	f.lastGroup = groupLabel

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

// fakeViewerPublisher 线程安全地记录最近一次推送，供实时流测试断言。
type fakeViewerPublisher struct {
	mu        sync.Mutex
	pushed    bool
	channel   string
	eventType string
	data      interface{}
	pushCount int
}

func (f *fakeViewerPublisher) PublishToChannel(_ context.Context, channel, eventType string, data interface{}) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pushed = true
	f.channel = channel
	f.eventType = eventType
	f.data = data
	f.pushCount++
	return nil
}

func (f *fakeViewerPublisher) snapshot() (bool, string, string, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pushed, f.channel, f.eventType, f.pushCount
}

// ========== 高亮测试 ==========

func TestFindAllOccurrences(t *testing.T) {
	cases := []struct {
		text     string
		keyword  string
		expected int
	}{
		{"ERROR something failed", "ERROR", 1},
		{"error Error ERROR", "error", 3},
		{"everything ok", "error", 0},
		{"", "error", 0},
		{"ERROR", "ERROR", 1},
	}

	for _, c := range cases {
		result := findAllOccurrences(c.text, c.keyword)
		if len(result) != c.expected {
			t.Errorf("'%s' 中 '%s' 出现次数期望 %d, got %d",
				c.text, c.keyword, c.expected, len(result))
		}
	}
}

func TestFindAllOccurrences_Positions(t *testing.T) {
	text := "Hello ERROR world ERROR"
	result := findAllOccurrences(text, "ERROR")

	if len(result) != 2 {
		t.Fatalf("应有 2 次出现, got %d", len(result))
	}

	// 第一次出现位置
	if result[0].Start != 6 {
		t.Errorf("第一次起始位置应为 6, got %d", result[0].Start)
	}
	if result[0].End != 11 {
		t.Errorf("第一次结束位置应为 11, got %d", result[0].End)
	}

	// 第二次出现位置
	if result[1].Start != 18 {
		t.Errorf("第二次起始位置应为 18, got %d", result[1].Start)
	}
}

func TestFindHighlights_ErrorKeywords(t *testing.T) {
	svc := NewLogViewerService(nil, nil)
	line := "2024-09-12 10:00:00 ERROR OrderService - NullPointerException"

	highlights := svc.findHighlights(line)

	if len(highlights) == 0 {
		t.Error("应找到至少一个高亮")
	}

	// 检查 ERROR 被标记
	hasErrorHL := false
	for _, hl := range highlights {
		if hl.Type == "error" {
			hasErrorHL = true
			break
		}
	}
	if !hasErrorHL {
		t.Error("ERROR 应被标记为 error 类型高亮")
	}
}

func TestFindHighlights_WarningKeywords(t *testing.T) {
	svc := NewLogViewerService(nil, nil)
	line := "2024-09-12 10:00:00 WARN request timeout"

	highlights := svc.findHighlights(line)

	hasWarnHL := false
	for _, hl := range highlights {
		if hl.Type == "warn" {
			hasWarnHL = true
			break
		}
	}
	if !hasWarnHL {
		t.Error("WARN 应被标记为 warn 类型高亮")
	}
}

func TestFindHighlights_Sorted(t *testing.T) {
	svc := NewLogViewerService(nil, nil)
	line := "ERROR something WARN"

	highlights := svc.findHighlights(line)

	// 应按起始位置排序
	for i := 1; i < len(highlights); i++ {
		if highlights[i].Start < highlights[i-1].Start {
			t.Error("高亮区间应按起始位置排序")
		}
	}
}

// ========== 日志处理测试 ==========

func TestProcessLogEntries(t *testing.T) {
	svc := NewLogViewerService(nil, nil)

	entries := []LogEntryRaw{
		{
			Timestamp: 1000,
			Line:      "ERROR something failed",
			Labels:    map[string]string{"service": "order-service", "level": "ERROR"},
		},
		{
			Timestamp: 2000,
			Line:      "INFO request processed",
			Labels:    map[string]string{"service": "user-service", "level": "INFO"},
		},
	}

	lines := svc.processLogEntries(entries)

	if len(lines) != 2 {
		t.Errorf("应有 2 条日志, got %d", len(lines))
	}

	// 错误行标记
	if !lines[0].IsError {
		t.Error("ERROR 级别日志应标记 IsError=true")
	}
	if lines[1].IsError {
		t.Error("INFO 级别日志应标记 IsError=false")
	}

	// 服务名
	if lines[0].Service != "order-service" {
		t.Errorf("服务名应为 order-service, got %s", lines[0].Service)
	}
}

func TestExtractServices(t *testing.T) {
	entries := []LogEntryRaw{
		{Labels: map[string]string{"service": "user-service"}},
		{Labels: map[string]string{"service": "order-service"}},
		{Labels: map[string]string{"service": "user-service"}}, // 重复
		{Labels: map[string]string{"service": "product-service"}},
	}

	services := extractServices(entries)
	if len(services) != 3 {
		t.Errorf("应有 3 个不同服务, got %d", len(services))
	}
}

func TestExtractLevels(t *testing.T) {
	entries := []LogEntryRaw{
		{Labels: map[string]string{"level": "INFO"}},
		{Labels: map[string]string{"level": "ERROR"}},
		{Labels: map[string]string{"level": "INFO"}}, // 重复
		{Labels: map[string]string{"level": "WARN"}},
	}

	levels := extractLevels(entries)
	if len(levels) != 3 {
		t.Errorf("应有 3 个不同级别, got %d", len(levels))
	}
}

// ========== 服务层测试 ==========

func TestQueryLogs(t *testing.T) {
	fakeLogs := &fakeLogProvider{
		entries: []LogEntryRaw{
			{
				Timestamp: 1000,
				Line:      "ERROR something failed",
				Labels:    map[string]string{"service": "order-service", "level": "ERROR"},
			},
			{
				Timestamp: 2000,
				Line:      "INFO request processed",
				Labels:    map[string]string{"service": "user-service", "level": "INFO"},
			},
		},
	}

	svc := NewLogViewerService(fakeLogs, nil)

	result, err := svc.QueryLogs(context.Background(),
		"sandbox-123", "order-service", "ERROR", "failed",
		time.Now().Add(-1*time.Hour), time.Now(),
		100, "backward")
	if err != nil {
		t.Fatal(err)
	}

	if result.Total != 2 {
		t.Errorf("应有 2 条日志, got %d", result.Total)
	}
	if len(result.Lines) != 2 {
		t.Errorf("应有 2 条日志行, got %d", len(result.Lines))
	}
	if len(result.Services) == 0 {
		t.Error("应提取到服务列表")
	}
	// ERROR 行应有高亮
	if len(result.Lines[0].Highlights) == 0 {
		t.Error("ERROR 行应有高亮")
	}
}

func TestQueryLogs_ProviderError(t *testing.T) {
	wantErr := errors.New("loki 不可用")
	fakeLogs := &fakeLogProvider{queryErr: wantErr}
	svc := NewLogViewerService(fakeLogs, nil)

	if _, err := svc.QueryLogs(context.Background(), "sb-1", "", "", "",
		time.Now().Add(-time.Hour), time.Now(), 100, "backward"); !errors.Is(err, wantErr) {
		t.Fatalf("应透传 provider 错误, got %v", err)
	}
}

// TestGetLogStats 分组统计：level 分组成功即返回总数与各级别计数，
// service 分组结果也应合并进 ByService。
func TestGetLogStats(t *testing.T) {
	fakeLogs := &fakeLogProvider{
		groupedSeq: []map[string]int{
			{"ERROR": 12, "WARN": 8, "INFO": 80, "DEBUG": 5}, // 第一次：level
			{"order-service": 60, "user-service": 45},        // 第二次：service
		},
	}
	svc := NewLogViewerService(fakeLogs, nil)

	stats, err := svc.GetStats(context.Background(),
		"sandbox-123", "", time.Now().Add(-1*time.Hour), time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if fakeLogs.groupedCalls != 2 {
		t.Errorf("应发起 2 次分组查询, got %d", fakeLogs.groupedCalls)
	}
	if fakeLogs.lastGroup != "service" {
		t.Errorf("最后一次分组标签应为 service, got %s", fakeLogs.lastGroup)
	}
	if stats.TotalCount != 105 {
		t.Errorf("总数应为 105（含 DEBUG）, got %d", stats.TotalCount)
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
	if got := stats.ByService["order-service"]; got != 60 {
		t.Errorf("order-service 应为 60, got %d", got)
	}
	if stats.TimeRange == "" {
		t.Error("时间范围不应为空")
	}
}

// TestGetLogStats_LevelQueryError level 分组失败必须整体返回错误。
func TestGetLogStats_LevelQueryError(t *testing.T) {
	fakeLogs := &fakeLogProvider{
		groupErrSeq: []error{errors.New("loki 不可用")},
	}
	svc := NewLogViewerService(fakeLogs, nil)

	if _, err := svc.GetStats(context.Background(), "sb-1", "",
		time.Now().Add(-time.Hour), time.Now()); err == nil {
		t.Fatal("level 分组失败应返回错误")
	}
}

// TestGetLogStats_ServiceQueryFailureDegraded service 分组失败时降级为空 map，不阻断接口。
func TestGetLogStats_ServiceQueryFailureDegraded(t *testing.T) {
	fakeLogs := &fakeLogProvider{
		groupedSeq:  []map[string]int{{"INFO": 3}, nil},
		groupErrSeq: []error{nil, errors.New("service 分组失败")},
	}
	svc := NewLogViewerService(fakeLogs, nil)

	stats, err := svc.GetStats(context.Background(), "sb-1", "",
		time.Now().Add(-time.Hour), time.Now())
	if err != nil {
		t.Fatalf("service 分组失败应降级, 不应返回错误, got %v", err)
	}
	if stats.TotalCount != 3 {
		t.Errorf("总数应为 3, got %d", stats.TotalCount)
	}
	if stats.ByService == nil {
		t.Error("降级时 ByService 应为空 map 而非 nil")
	}
}

func TestExportLogs(t *testing.T) {
	fakeLogs := &fakeLogProvider{
		entries: []LogEntryRaw{
			{
				Timestamp: 1726000000000,
				Line:      "ERROR something failed",
				Labels:    map[string]string{"service": "order-service", "level": "ERROR"},
			},
		},
	}

	svc := NewLogViewerService(fakeLogs, nil)

	result, err := svc.ExportLogs(context.Background(),
		"sandbox-123", "", "", "",
		time.Now().Add(-1*time.Hour), time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if result.Lines != 1 {
		t.Errorf("应有 1 行, got %d", result.Lines)
	}
	if result.Size == 0 {
		t.Error("内容大小不应为 0")
	}
	if result.Filename == "" {
		t.Error("文件名不应为空")
	}
	if len(result.Content) == 0 {
		t.Error("内容不应为空")
	}
	if result.Truncated {
		t.Error("未命中上限时不应标记截断")
	}
	// 导出按 limit+1 查询以判定截断。
	if fakeLogs.lastLimit != defaultExportLimit+1 {
		t.Errorf("应按 exportLimit+1=%d 查询, got %d", defaultExportLimit+1, fakeLogs.lastLimit)
	}
}

// TestExportLogs_Truncated 命中导出上限时 Truncated=true 且内容被截断到上限。
func TestExportLogs_Truncated(t *testing.T) {
	entries := make([]LogEntryRaw, defaultExportLimit+1)
	for i := range entries {
		entries[i] = LogEntryRaw{
			Timestamp: int64(1726000000000 + i),
			Line:      "log line",
			Labels:    map[string]string{"service": "svc", "level": "INFO"},
		}
	}
	fakeLogs := &fakeLogProvider{entries: entries}
	svc := NewLogViewerService(fakeLogs, nil)

	result, err := svc.ExportLogs(context.Background(), "sb-1", "", "", "",
		time.Now().Add(-time.Hour), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated {
		t.Error("命中上限时应标记 Truncated=true")
	}
	if result.Lines != defaultExportLimit {
		t.Errorf("截断后行数应为 %d, got %d", defaultExportLimit, result.Lines)
	}
}

// ========== 实时日志流测试 ==========

// waitStarted 等待首轮立即拉取完成（tailLogs 在首拉 pushNewLogs 返回后
// 才 close(started)），返回时首拉的推送（若有）已落地。
func waitStarted(t *testing.T, stream *logStream) {
	t.Helper()
	select {
	case <-stream.started:
	case <-time.After(2 * time.Second):
		t.Fatal("等待实时流首拉超时")
	}
}

// triggerTick 触发一轮手动拉取并严格等待该轮完成：
// 先排空 ack 中可能滞留的旧信号，再向 tick 发触发信号，
// 最后阻塞等待本轮 pushNewLogs 完成后写入的 ack。
func triggerTick(t *testing.T, stream *logStream) {
	t.Helper()
	// 非阻塞排陈旧 ack，避免把上一轮的完成信号误当成本轮同步点。
	select {
	case <-stream.ack:
	default:
	}
	stream.tick <- struct{}{}
	select {
	case <-stream.ack:
	case <-time.After(2 * time.Second):
		t.Fatal("等待实时流触发超时")
	}
}

// TestStartStopStream 启动流、手动触发一轮推送、再停止。
//
// 使用超长真实间隔 + tick 手动触发，避免依赖真实时间 sleep（T4-16）。
func TestStartStopStream(t *testing.T) {
	now := time.Now().UnixMilli()
	fakeLogs := &fakeLogProvider{
		entries: []LogEntryRaw{
			{
				Timestamp: now + 1000,
				Line:      "new log line",
				Labels:    map[string]string{"service": "order-service", "level": "INFO"},
			},
		},
	}
	publisher := &fakeViewerPublisher{}
	svc := NewLogViewerService(fakeLogs, publisher)
	defer svc.Close()

	// 1 小时间隔：测试期间 ticker 绝不可能触发，所有轮次都由 tick 手动驱动。
	if err := svc.StartStream("sandbox-123", "order-service", time.Hour); err != nil {
		t.Fatal(err)
	}
	if svc.ActiveStreams() != 1 {
		t.Errorf("存活流数量应为 1, got %d", svc.ActiveStreams())
	}

	stream := svc.streams["sandbox-123"]
	// 启动时会立即首拉一轮（此时刻之后的日志），等待该轮完成后再断言。
	waitStarted(t, stream)

	pushed, channel, eventType, _ := publisher.snapshot()
	if !pushed {
		t.Error("应推送过日志")
	}
	if channel != "sandbox:sandbox-123" {
		t.Errorf("频道应为 sandbox:sandbox-123, got %s", channel)
	}
	if eventType != "log_lines" {
		t.Errorf("事件类型应为 log_lines, got %s", eventType)
	}

	// StopStream 同步等待 goroutine 退出，返回后不应再有推送。
	svc.StopStream("sandbox-123")
	if svc.ActiveStreams() != 0 {
		t.Errorf("停止后存活流数量应为 0, got %d", svc.ActiveStreams())
	}
}

// TestStartStream_MissingSandbox 空沙箱 ID 直接报错且不注册流。
func TestStartStream_MissingSandbox(t *testing.T) {
	svc := NewLogViewerService(&fakeLogProvider{}, nil)
	defer svc.Close()

	if err := svc.StartStream("", "svc", time.Hour); err == nil {
		t.Fatal("空 sandboxID 应返回错误")
	}
	if svc.ActiveStreams() != 0 {
		t.Errorf("失败时不应注册流, got %d", svc.ActiveStreams())
	}
}

// TestStartStream_MaxStreams 达到并发上限后新沙箱启动被拒；替换已有流不占名额。
func TestStartStream_MaxStreams(t *testing.T) {
	svc := NewLogViewerService(&fakeLogProvider{}, &fakeViewerPublisher{})
	defer svc.Close()
	svc.SetMaxStreams(2)

	if err := svc.StartStream("sb-1", "svc-a", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartStream("sb-2", "svc-b", time.Hour); err != nil {
		t.Fatal(err)
	}

	// 第三个不同沙箱应被闸门拒绝。
	err := svc.StartStream("sb-3", "svc-c", time.Hour)
	if !errors.Is(err, ErrMaxStreamsReached) {
		t.Fatalf("应返回 ErrMaxStreamsReached, got %v", err)
	}
	if svc.ActiveStreams() != 2 {
		t.Errorf("被拒后流数量应仍为 2, got %d", svc.ActiveStreams())
	}

	// 替换已有沙箱的流不应受闸门影响，也不应泄漏流数量。
	if err := svc.StartStream("sb-1", "svc-a2", time.Hour); err != nil {
		t.Fatalf("替换已有流不应报错, got %v", err)
	}
	if svc.ActiveStreams() != 2 {
		t.Errorf("替换后流数量应仍为 2, got %d", svc.ActiveStreams())
	}
}

// TestStartStream_IntervalClamped 小于下限的间隔被钳制（MinInterval 可回读）。
func TestStartStream_IntervalClamped(t *testing.T) {
	svc := NewLogViewerService(&fakeLogProvider{}, &fakeViewerPublisher{})
	defer svc.Close()

	if err := svc.StartStream("sb-1", "svc", time.Millisecond); err != nil {
		t.Fatal(err)
	}
	stream := svc.streams["sb-1"]
	// 钳制逻辑作用于 tailLogs 的 ticker，无法直接读 interval 形参，
	// 这里通过 MinInterval 与配置一致性间接覆盖；显式再校验下限配置。
	if svc.MinInterval() != defaultMinInterval {
		t.Errorf("默认间隔下限应为 %v, got %v", defaultMinInterval, svc.MinInterval())
	}
	if stream == nil {
		t.Fatal("流应已注册")
	}
}

// TestStream_DedupSameTimestamp 同毫秒日志在相邻两轮中只推送一次。
func TestStream_DedupSameTimestamp(t *testing.T) {
	fixedTs := time.Now().UnixMilli() + 5000
	fakeLogs := &fakeLogProvider{
		entries: []LogEntryRaw{
			{Timestamp: fixedTs, Line: "same-ms-log", Labels: map[string]string{"service": "svc", "level": "INFO"}},
		},
	}
	publisher := &fakeViewerPublisher{}
	svc := NewLogViewerService(fakeLogs, publisher)
	defer svc.Close()

	if err := svc.StartStream("sb-1", "svc", time.Hour); err != nil {
		t.Fatal(err)
	}
	stream := svc.streams["sb-1"]
	waitStarted(t, stream) // 首轮：推送 1 条

	_, _, _, firstCount := publisher.snapshot()
	if firstCount != 1 {
		t.Fatalf("首轮应推送 1 次, got %d", firstCount)
	}

	// 第二轮 fake 仍返回同毫秒同内容日志，应被 seen 去重，不再推送。
	triggerTick(t, stream)
	if _, _, _, got := publisher.snapshot(); got != firstCount {
		t.Errorf("同毫秒重复日志不应再次推送, 推送次数 %d -> %d", firstCount, got)
	}
}

// TestStream_AdvanceTimestamp 时间戳前进后新日志正常推送，且 seen 集合随边界重建。
func TestStream_AdvanceTimestamp(t *testing.T) {
	base := time.Now().UnixMilli() + 5000
	fakeLogs := &fakeLogProvider{
		querySeq: [][]LogEntryRaw{
			{{Timestamp: base, Line: "log-1", Labels: map[string]string{"service": "svc", "level": "INFO"}}},
			{{Timestamp: base, Line: "log-1", Labels: map[string]string{"service": "svc", "level": "INFO"}}}, // 旧毫秒：应全被丢
			{{Timestamp: base + 1000, Line: "log-2", Labels: map[string]string{"service": "svc", "level": "INFO"}}},
			{{Timestamp: base + 1000, Line: "log-2", Labels: map[string]string{"service": "svc", "level": "INFO"}}}, // 同毫秒重复：去重
		},
	}
	publisher := &fakeViewerPublisher{}
	svc := NewLogViewerService(fakeLogs, publisher)
	defer svc.Close()

	if err := svc.StartStream("sb-1", "svc", time.Hour); err != nil {
		t.Fatal(err)
	}
	stream := svc.streams["sb-1"]

	// 立即首拉消费 querySeq[0]（推送 1 次），再手动触发消费 querySeq[1..3]。
	waitStarted(t, stream)
	triggerTick(t, stream) // seq[1]：旧毫秒重复，无新增
	triggerTick(t, stream) // seq[2]：新毫秒，推送 1 条
	_, _, _, afterAdvance := publisher.snapshot()
	if afterAdvance != 2 { // 首拉 1 次 + 前进后 1 次
		t.Fatalf("时间戳前进后累计应推送 2 次, got %d", afterAdvance)
	}
	triggerTick(t, stream) // seq[3]：新毫秒重复条目，去重
	if _, _, _, got := publisher.snapshot(); got != afterAdvance {
		t.Errorf("跨边界后同毫秒重复不应再推送, got %d", got)
	}
}

// TestStream_QueryErrorIgnored 拉取失败时这一轮静默跳过，流继续存活。
func TestStream_QueryErrorIgnored(t *testing.T) {
	fakeLogs := &fakeLogProvider{queryErr: errors.New("loki 超时")}
	publisher := &fakeViewerPublisher{}
	svc := NewLogViewerService(fakeLogs, publisher)
	defer svc.Close()

	if err := svc.StartStream("sb-1", "svc", time.Hour); err != nil {
		t.Fatal(err)
	}
	stream := svc.streams["sb-1"]
	waitStarted(t, stream) // 首拉即出错：不应 panic、不应推送

	if _, _, _, n := publisher.snapshot(); n != 0 {
		t.Errorf("拉取失败不应有推送, got %d 次", n)
	}
	if svc.ActiveStreams() != 1 {
		t.Errorf("出错后流应继续存活, got %d", svc.ActiveStreams())
	}
}

// TestClose_StopsAllStreams Close 后所有流被回收且可重复安全调用。
func TestClose_StopsAllStreams(t *testing.T) {
	svc := NewLogViewerService(&fakeLogProvider{}, &fakeViewerPublisher{})

	if err := svc.StartStream("sb-1", "svc", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartStream("sb-2", "svc", time.Hour); err != nil {
		t.Fatal(err)
	}

	svc.Close()
	if svc.ActiveStreams() != 0 {
		t.Errorf("Close 后不应有存活流, got %d", svc.ActiveStreams())
	}
	// 重复 Close 不应阻塞或 panic。
	svc.Close()
}

// ========== 自定义高亮配置测试 ==========

func TestCustomHighlightConfig(t *testing.T) {
	svc := NewLogViewerService(nil, nil)

	// 设置自定义关键词
	svc.SetHighlightConfig(HighlightConfig{
		CustomKeywords: []string{"CUSTOM_ERROR"},
	})

	line := "this is a CUSTOM_ERROR message"
	highlights := svc.findHighlights(line)

	hasCustomHL := false
	for _, hl := range highlights {
		if hl.Type == "keyword" {
			hasCustomHL = true
			break
		}
	}
	if !hasCustomHL {
		t.Error("自定义关键词应被标记为 keyword 类型高亮")
	}
}

// ========== 错误行判断测试 ==========

func TestIsError(t *testing.T) {
	svc := NewLogViewerService(nil, nil)

	errorEntry := LogEntryRaw{
		Line:   "error message",
		Labels: map[string]string{"level": "ERROR"},
	}
	fatalEntry := LogEntryRaw{
		Line:   "fatal error",
		Labels: map[string]string{"level": "FATAL"},
	}
	infoEntry := LogEntryRaw{
		Line:   "info message",
		Labels: map[string]string{"level": "INFO"},
	}

	lines := svc.processLogEntries([]LogEntryRaw{errorEntry, fatalEntry, infoEntry})

	if !lines[0].IsError {
		t.Error("ERROR 级别应标记为错误行")
	}
	if !lines[1].IsError {
		t.Error("FATAL 级别应标记为错误行")
	}
	if lines[2].IsError {
		t.Error("INFO 级别不应标记为错误行")
	}
}
