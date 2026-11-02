// Package service 中故障注入日志观察（T1-22 / T4-16 查询与实时流修正）。
//
// 在 T1-17 日志查询的基础上，提供训练场景下的日志观察功能：
//   - 关键词高亮（异常、告警关键词自动标记）
//   - 日志上下文（点击查看前后 N 条）
//   - 实时日志流（定时推送到前端）
//   - 日志统计（各级别数量、错误趋势）
package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// ========== 数据结构 ==========

// LogLine 单条日志（带高亮信息）。
type LogLine struct {
	Timestamp  int64             `json:"ts"`     // 毫秒时间戳
	Line       string            `json:"line"`   // 原始日志内容
	Service    string            `json:"svc"`    // 服务名
	Level      string            `json:"level"`  // 日志级别
	Pod        string            `json:"pod"`    // Pod名
	Highlights []HighlightRange  `json:"hl"`     // 高亮区间
	IsError    bool              `json:"is_err"` // 是否错误行
	Labels     map[string]string `json:"labels,omitempty"`
}

// HighlightRange 高亮区间。
type HighlightRange struct {
	Start int    `json:"s"` // 起始位置
	End   int    `json:"e"` // 结束位置
	Type  string `json:"t"` // 类型：error/warn/keyword
}

// LogQueryResult 日志查询结果。
type LogQueryResult struct {
	Total    int       `json:"total"`    // 返回条数
	HasMore  bool      `json:"has_more"` // 是否还有更多
	Lines    []LogLine `json:"lines"`    // 日志行
	Services []string  `json:"services"` // 涉及的服务
	Levels   []string  `json:"levels"`   // 涉及的级别
}

// LogContextResult 日志上下文结果。
type LogContextResult struct {
	CenterTimestamp int64     `json:"center_ts"` // 中心日志时间戳
	Before          []LogLine `json:"before"`    // 之前的日志（时间倒序）
	After           []LogLine `json:"after"`     // 之后的日志（时间正序）
	ContextSize     int       `json:"ctx_size"`  // 上下文条数
}

// LogStatsResult 日志统计结果。
type LogStatsResult struct {
	TotalCount int            `json:"total_count"` // 总日志数
	ErrorCount int            `json:"error_count"` // 错误数
	WarnCount  int            `json:"warn_count"`  // 警告数
	InfoCount  int            `json:"info_count"`  // 信息数
	ByService  map[string]int `json:"by_service"`  // 按服务统计
	TimeRange  string         `json:"time_range"`  // 时间范围
}

// LogExportResult 日志导出结果。
type LogExportResult struct {
	Filename  string `json:"filename"`  // 文件名
	Content   string `json:"content"`   // 内容（纯文本）
	Lines     int    `json:"lines"`     // 行数
	Size      int    `json:"size"`      // 大小（字节）
	Truncated bool   `json:"truncated"` // 是否因达到导出上限而截断
}

// ========== 依赖接口 ==========

// LogViewerLogProvider 日志数据提供者。
type LogViewerLogProvider interface {
	// QueryLogs 区间查询日志。
	QueryLogs(ctx context.Context, sandboxID, service string, level string, keyword string,
		start, end time.Time, limit int, direction string) (*LogQueryResultRaw, error)

	// GetLatestLogs 获取最新 N 条。
	GetLatestLogs(ctx context.Context, sandboxID, service string, limit int) (*LogQueryResultRaw, error)

	// Count 统计日志条数。
	Count(ctx context.Context, sandboxID, service string, level string,
		start, end time.Time) (int, error)

	// CountGrouped 按标签分组统计日志条数（groupLabel 仅支持 level/service）。
	//
	// T4-16：一次 instant 分组查询替代 4 次 Count。
	CountGrouped(ctx context.Context, sandboxID, service, groupLabel string,
		start, end time.Time) (map[string]int, error)
}

// LogQueryResultRaw 原始查询结果（从 logquery 包映射）。
type LogQueryResultRaw struct {
	Total   int
	HasMore bool
	Entries []LogEntryRaw
}

// LogEntryRaw 原始日志条目。
type LogEntryRaw struct {
	Timestamp int64
	Line      string
	Labels    map[string]string
}

// ErrMaxStreamsReached 实时流数量达到上限。
var ErrMaxStreamsReached = errors.New("实时日志流数量已达上限")

// ========== 默认配置 ==========

const (
	// defaultMaxStreams 单进程同时存活的实时流上限（按沙箱计）。
	defaultMaxStreams = 20
	// defaultMinInterval 前端轮询间隔下限，防止高频轮询打爆 Loki。
	defaultMinInterval = 2 * time.Second
	// defaultExportLimit 单次导出条数上限。
	defaultExportLimit = 5000
	// streamQueryLimit 单轮 tail 拉取条数。
	streamQueryLimit = 100
)

// ========== 高亮关键词配置 ==========

// HighlightConfig 高亮配置。
type HighlightConfig struct {
	ErrorKeywords   []string // 错误关键词
	WarningKeywords []string // 警告关键词
	CustomKeywords  []string // 自定义关键词
}

// defaultHighlightConfig 默认高亮配置。
var defaultHighlightConfig = HighlightConfig{
	ErrorKeywords: []string{
		"ERROR", "Error", "error",
		"Exception", "exception",
		"OutOfMemoryError", "NullPointerException", "SQLException",
		"FATAL", "fatal",
		"failed", "Failed", "FAILURE", "failure",
		"500", "502", "503", "504",
	},
	WarningKeywords: []string{
		"WARN", "Warn", "warn",
		"WARNING", "Warning", "warning",
		"timeout", "Timeout",
		"retry", "Retry",
		"401", "403", "404",
	},
}

// ========== 实时日志流管理 ==========

// logStream 单个沙箱的实时流状态。
//
// lastTs / seen 只在所属 tail goroutine 内访问，无需加锁；
// running/cancel 由 LogViewerService.mu 保护。
// wg 为每流独立：替换旧流时只需等待旧 goroutine 退出，
// 不能与 LogViewerService 中其他流共享同一个 WaitGroup，否则替换场景会相互阻塞甚至死锁。
type logStream struct {
	sandboxID string
	service   string
	cancel    context.CancelFunc
	running   bool
	lastTs    int64               // 已推送日志的最大毫秒时间戳
	seen      map[string]struct{} // 上一批日志的 (ts,line) 键，跨轮去重
	wg        sync.WaitGroup      // 仅跟踪本流的 tail goroutine
	tick      chan struct{}       // 手动触发通道：生产无人发送，测试可注入
	started   chan struct{}       // 首轮拉取完成后关闭，供测试同步首拉
	ack       chan struct{}       // 每轮拉取完成后的非阻塞信号（缓冲1，仅测试消费）
}

// streamKey 生成 (时间戳, 日志内容) 去重键。
func streamKey(ts int64, line string) string {
	return fmt.Sprintf("%d|%s", ts, line)
}

// ========== 日志观察服务 ==========

// LogViewerService 日志观察服务。
type LogViewerService struct {
	logs      LogViewerLogProvider
	publisher EventPublisher
	highlight HighlightConfig

	mu      sync.Mutex
	streams map[string]*logStream // sandboxID -> stream

	maxStreams  int
	minInterval time.Duration
	exportLimit int
}

// NewLogViewerService 创建日志观察服务。
func NewLogViewerService(logs LogViewerLogProvider, publisher EventPublisher) *LogViewerService {
	return &LogViewerService{
		logs:        logs,
		publisher:   publisher,
		highlight:   defaultHighlightConfig,
		streams:     make(map[string]*logStream),
		maxStreams:  defaultMaxStreams,
		minInterval: defaultMinInterval,
		exportLimit: defaultExportLimit,
	}
}

// SetHighlightConfig 设置高亮配置。
func (s *LogViewerService) SetHighlightConfig(cfg HighlightConfig) {
	s.highlight = cfg
}

// SetMaxStreams 配置同时存活的实时流上限（<=0 时忽略）。
func (s *LogViewerService) SetMaxStreams(n int) {
	if n > 0 {
		s.maxStreams = n
	}
}

// SetMinInterval 配置实时流轮询间隔下限（<=0 时忽略）。
func (s *LogViewerService) SetMinInterval(d time.Duration) {
	if d > 0 {
		s.minInterval = d
	}
}

// SetExportLimit 配置单次导出条数上限（<=0 时忽略）。
func (s *LogViewerService) SetExportLimit(n int) {
	if n > 0 {
		s.exportLimit = n
	}
}

// MinInterval 返回当前轮询间隔下限，供 handler 回传钳制后的实际间隔。
func (s *LogViewerService) MinInterval() time.Duration {
	return s.minInterval
}

// QueryLogs 查询日志（带高亮）。
func (s *LogViewerService) QueryLogs(ctx context.Context, sandboxID, service, level, keyword string,
	start, end time.Time, limit int, direction string) (*LogQueryResult, error) {

	raw, err := s.logs.QueryLogs(ctx, sandboxID, service, level, keyword, start, end, limit, direction)
	if err != nil {
		return nil, err
	}

	lines := s.processLogEntries(raw.Entries)
	services := extractServices(raw.Entries)
	levels := extractLevels(raw.Entries)

	return &LogQueryResult{
		Total:    raw.Total,
		HasMore:  raw.HasMore,
		Lines:    lines,
		Services: services,
		Levels:   levels,
	}, nil
}

// GetLogContext 获取指定时间戳的日志上下文。
//
// 返回该时间点前后各 size 条日志。
func (s *LogViewerService) GetLogContext(ctx context.Context, sandboxID, service string,
	timestamp int64, size int) (*LogContextResult, error) {

	centerTime := time.UnixMilli(timestamp)
	window := 30 * time.Minute // 前后各 30 分钟窗口

	// 查之前的日志（时间倒序，取最新的 size 条）
	beforeResult, err := s.logs.QueryLogs(ctx, sandboxID, service, "", "",
		centerTime.Add(-window), centerTime, size, "backward")
	if err != nil {
		return nil, err
	}

	// 查之后的日志（时间正序，取最旧的 size 条）
	afterResult, err := s.logs.QueryLogs(ctx, sandboxID, service, "", "",
		centerTime, centerTime.Add(window), size, "forward")
	if err != nil {
		return nil, err
	}

	return &LogContextResult{
		CenterTimestamp: timestamp,
		Before:          s.processLogEntries(beforeResult.Entries),
		After:           s.processLogEntries(afterResult.Entries),
		ContextSize:     size,
	}, nil
}

// GetStats 获取日志统计。
//
// T4-16：旧实现并发发 4 次 Count。这里改为一次 level 分组 instant 查询；
// service 分组作为增强信息，失败时降级为空 map，不影响级别统计。
func (s *LogViewerService) GetStats(ctx context.Context, sandboxID, service string,
	start, end time.Time) (*LogStatsResult, error) {

	if start.IsZero() {
		start = time.Now().Add(-1 * time.Hour)
	}
	if end.IsZero() {
		end = time.Now()
	}

	stats := &LogStatsResult{
		ByService: make(map[string]int),
		TimeRange: start.Format("15:04:05") + " ~ " + end.Format("15:04:05"),
	}

	byLevel, err := s.logs.CountGrouped(ctx, sandboxID, service, "level", start, end)
	if err != nil {
		return nil, fmt.Errorf("统计日志级别失败: %w", err)
	}
	for level, n := range byLevel {
		stats.TotalCount += n
		switch strings.ToUpper(strings.TrimSpace(level)) {
		case "ERROR":
			stats.ErrorCount += n
		case "WARN":
			stats.WarnCount += n
		case "INFO":
			stats.InfoCount += n
		}
	}

	// 按服务分组失败时降级（保留空 map），不阻断统计接口。
	if byService, err := s.logs.CountGrouped(ctx, sandboxID, service, "service", start, end); err == nil {
		stats.ByService = byService
	}

	return stats, nil
}

// ExportLogs 导出日志（纯文本格式）。
//
// T4-16：导出条数上限可配（默认 5000），实际命中上限时 Truncated=true。
func (s *LogViewerService) ExportLogs(ctx context.Context, sandboxID, service, level, keyword string,
	start, end time.Time) (*LogExportResult, error) {

	limit := s.exportLimit

	// 多查一条判断是否截断
	raw, err := s.logs.QueryLogs(ctx, sandboxID, service, level, keyword,
		start, end, limit+1, "forward")
	if err != nil {
		return nil, err
	}

	truncated := len(raw.Entries) > limit
	if truncated {
		raw.Entries = raw.Entries[:limit]
	}

	// 组装纯文本
	var sb strings.Builder
	for _, entry := range raw.Entries {
		t := time.UnixMilli(entry.Timestamp).Format("2006-01-02 15:04:05.000")
		svc := entry.Labels["service"]
		level := entry.Labels["level"]
		sb.WriteString(t)
		sb.WriteString(" [")
		sb.WriteString(svc)
		sb.WriteString("] [")
		sb.WriteString(level)
		sb.WriteString("] ")
		sb.WriteString(entry.Line)
		sb.WriteString("\n")
	}

	content := sb.String()
	filename := "logs_" + sandboxID + "_" + start.Format("20060102_150405") + ".log"

	return &LogExportResult{
		Filename:  filename,
		Content:   content,
		Lines:     len(raw.Entries),
		Size:      len(content),
		Truncated: truncated,
	}, nil
}

// ========== 实时日志流 ==========

// StartStream 启动实时日志流。
//
// T4-16 修复：旧实现把 HTTP 请求的 ctx 直接传给 tail goroutine，响应一返回
// ctx 即被取消，tail 立刻退出，实时流从未真正工作。现在流的生命周期与请求
// 解耦——内部以 context.Background 派生，按沙箱维护 cancel，由 StopStream/
// Close 显式回收；同时限制全局并发流数量并钳制轮询间隔下限。
//
// 返回 ErrMaxStreamsReached 表示并发流已达上限，调用方应提示用户稍后再试。
func (s *LogViewerService) StartStream(sandboxID, service string, interval time.Duration) error {
	if sandboxID == "" {
		return fmt.Errorf("sandbox_id 不能为空")
	}
	if interval < s.minInterval {
		interval = s.minInterval
	}

	ctx, cancel := context.WithCancel(context.Background())
	stream := &logStream{
		sandboxID: sandboxID,
		service:   service,
		cancel:    cancel,
		running:   true,
		// 从"当前时刻"开始推，只推送启动之后产生的新日志，
		// 零值会让首拉从 1970 年取最旧的一批历史日志。
		lastTs: time.Now().UnixMilli(),
		seen:   make(map[string]struct{}),
		// 缓冲 1 的手动触发通道：生产侧无人发送，永不触发；
		// 同包测试可向其发送信号立即触发一轮拉取，避免依赖真实时间 sleep。
		tick:    make(chan struct{}, 1),
		started: make(chan struct{}),
		// 缓冲 1 的轮次完成信号：tailLogs 每轮拉取后非阻塞写入，
		// 供测试与"手动触发的一轮"严格同步；生产侧无人消费也不会阻塞。
		ack: make(chan struct{}, 1),
	}

	// 注册新区间：替换旧流（等待旧 goroutine 退出）+ 闸门校验都在锁内判定，
	// 但等待旧 goroutine 退出放在锁外，避免与 tailLogs 退出时取锁形成竞争。
	var old *logStream
	s.mu.Lock()
	if existing, ok := s.streams[sandboxID]; ok && existing.running {
		old = existing
	}
	// 新流替换旧流不占用额外名额；非替换场景受 maxStreams 限制。
	if old == nil && len(s.streams) >= s.maxStreams {
		s.mu.Unlock()
		cancel()
		return ErrMaxStreamsReached
	}
	if old != nil {
		old.running = false
		old.cancel()
		delete(s.streams, sandboxID)
	}
	s.streams[sandboxID] = stream
	s.mu.Unlock()

	// 等待旧流 goroutine 退出后再启动新流，确保同一沙箱不会有两个拉取循环并发。
	if old != nil {
		old.wg.Wait()
	}

	// Add 在闸门通过、即将启动 goroutine 前完成，避免被拒流在废弃对象上遗留计数。
	stream.wg.Add(1)
	go s.tailLogs(ctx, stream, interval)
	return nil
}

// StopStream 停止单个沙箱的实时日志流，并等待其 goroutine 退出。
//
// 同步等待是为了杜绝"刚 Stop 又立刻 Start"时新旧两个拉取循环短暂并发、
// 向同一频道重复推送的窗口。
func (s *LogViewerService) StopStream(sandboxID string) {
	s.mu.Lock()
	stream := s.streams[sandboxID]
	if stream != nil {
		stream.running = false
		delete(s.streams, sandboxID)
	}
	s.mu.Unlock()

	if stream != nil {
		stream.cancel()
		stream.wg.Wait()
	}
}

// Close 停止全部实时流并等待 goroutine 退出，供进程优雅关停调用。
func (s *LogViewerService) Close() {
	s.mu.Lock()
	streams := make([]*logStream, 0, len(s.streams))
	for _, stream := range s.streams {
		stream.running = false
		streams = append(streams, stream)
	}
	s.streams = make(map[string]*logStream)
	s.mu.Unlock()

	// 先全部发取消信号，再逐流等待，避免串行等待放大关停耗时。
	for _, stream := range streams {
		stream.cancel()
	}
	for _, stream := range streams {
		stream.wg.Wait()
	}
}

// ActiveStreams 返回当前存活的实时流数量（主要用于测试/观测）。
func (s *LogViewerService) ActiveStreams() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.streams)
}

// tailLogs 定时拉取新日志并推送，直到 ctx 取消。
func (s *LogViewerService) tailLogs(ctx context.Context, stream *logStream, interval time.Duration) {
	defer stream.wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// 启动后立即拉一次，覆盖"启动请求发出到首轮 tick 之间"产生的日志。
	// started 在首拉完成后关闭，waitStarted 返回即保证首拉的推送已落地。
	s.pushNewLogs(ctx, stream)
	close(stream.started)

	for {
		select {
		case <-ticker.C:
			s.pushNewLogs(ctx, stream)
			s.signalAck(stream)
		case <-stream.tick:
			s.pushNewLogs(ctx, stream)
			s.signalAck(stream)
		case <-ctx.Done():
			return
		}
	}
}

// signalAck 非阻塞通知一轮拉取已完成（供测试同步；缓冲满时说明上一轮信号
// 尚未被消费，直接丢弃即可，测试在触发新一轮前会先排空旧信号）。
func (s *LogViewerService) signalAck(stream *logStream) {
	select {
	case stream.ack <- struct{}{}:
	default:
	}
}

// pushNewLogs 拉取新日志并推送到事件总线。
//
// 去重策略（仅本 goroutine 访问 stream 状态，无需加锁）：
// 以 (毫秒时间戳, 日志内容) 为键，seen 保存最近一批拉取结果的键集合，
// 解决"同毫秒日志在 since=lastTs 的下一轮被重复推送"问题；
// 当出现比 lastTs 更新的时间戳时，跨毫秒边界，旧键集合整批淘汰。
func (s *LogViewerService) pushNewLogs(ctx context.Context, stream *logStream) {
	if s.publisher == nil {
		return
	}

	since := time.UnixMilli(stream.lastTs)
	now := time.Now()

	// 拉取新日志（从旧到新，便于前端追加）
	raw, err := s.logs.QueryLogs(ctx, stream.sandboxID, stream.service, "", "",
		since, now, streamQueryLimit, "forward")
	if err != nil {
		return
	}
	if len(raw.Entries) == 0 {
		return
	}

	// 过滤掉已推送过的同毫秒条目
	fresh := make([]LogEntryRaw, 0, len(raw.Entries))
	maxTs := stream.lastTs
	for _, entry := range raw.Entries {
		if entry.Timestamp == stream.lastTs {
			if _, dup := stream.seen[streamKey(entry.Timestamp, entry.Line)]; dup {
				continue
			}
		}
		fresh = append(fresh, entry)
		if entry.Timestamp > maxTs {
			maxTs = entry.Timestamp
		}
	}

	if len(fresh) == 0 {
		return
	}

	// 跨毫秒边界后重建去重集合，只保留最新一批的键（集合有界）。
	if maxTs != stream.lastTs {
		stream.seen = make(map[string]struct{}, len(fresh))
		stream.lastTs = maxTs
	}
	for _, entry := range fresh {
		if entry.Timestamp == stream.lastTs {
			stream.seen[streamKey(entry.Timestamp, entry.Line)] = struct{}{}
		}
	}

	// 处理高亮并推送
	lines := s.processLogEntries(fresh)
	channel := "sandbox:" + stream.sandboxID
	_ = s.publisher.PublishToChannel(ctx, channel, "log_lines", map[string]interface{}{
		"sandbox_id": stream.sandboxID,
		"service":    stream.service,
		"lines":      lines,
		"count":      len(lines),
	})
}

// ========== 内部方法 ==========

// processLogEntries 处理日志条目（添加高亮、标记错误行）。
func (s *LogViewerService) processLogEntries(entries []LogEntryRaw) []LogLine {
	lines := make([]LogLine, 0, len(entries))

	for _, entry := range entries {
		line := LogLine{
			Timestamp: entry.Timestamp,
			Line:      entry.Line,
			Service:   entry.Labels["service"],
			Level:     entry.Labels["level"],
			Pod:       entry.Labels["pod"],
			Labels:    entry.Labels,
		}

		// 高亮处理
		highlights := s.findHighlights(entry.Line)
		line.Highlights = highlights

		// 标记错误行
		if entry.Labels["level"] == "ERROR" || entry.Labels["level"] == "FATAL" {
			line.IsError = true
		}

		lines = append(lines, line)
	}

	return lines
}

// findHighlights 查找日志中的高亮关键词。
func (s *LogViewerService) findHighlights(line string) []HighlightRange {
	var highlights []HighlightRange

	// 错误关键词
	for _, kw := range s.highlight.ErrorKeywords {
		if ranges := findAllOccurrences(line, kw); len(ranges) > 0 {
			for _, r := range ranges {
				highlights = append(highlights, HighlightRange{
					Start: r.Start,
					End:   r.End,
					Type:  "error",
				})
			}
		}
	}

	// 警告关键词
	for _, kw := range s.highlight.WarningKeywords {
		if ranges := findAllOccurrences(line, kw); len(ranges) > 0 {
			for _, r := range ranges {
				highlights = append(highlights, HighlightRange{
					Start: r.Start,
					End:   r.End,
					Type:  "warn",
				})
			}
		}
	}

	// 自定义关键词
	for _, kw := range s.highlight.CustomKeywords {
		if ranges := findAllOccurrences(line, kw); len(ranges) > 0 {
			for _, r := range ranges {
				highlights = append(highlights, HighlightRange{
					Start: r.Start,
					End:   r.End,
					Type:  "keyword",
				})
			}
		}
	}

	// 按起始位置排序
	sort.Slice(highlights, func(i, j int) bool {
		return highlights[i].Start < highlights[j].Start
	})

	return highlights
}

// occurrence 关键词出现位置。
type occurrence struct {
	Start int
	End   int
}

// findAllOccurrences 查找关键词在文本中的所有出现位置（大小写不敏感）。
func findAllOccurrences(text, keyword string) []occurrence {
	if keyword == "" {
		return nil
	}

	textLower := strings.ToLower(text)
	kwLower := strings.ToLower(keyword)

	var results []occurrence
	start := 0
	for {
		idx := strings.Index(textLower[start:], kwLower)
		if idx == -1 {
			break
		}
		results = append(results, occurrence{
			Start: start + idx,
			End:   start + idx + len(keyword),
		})
		start += idx + len(keyword)
	}

	return results
}

// extractServices 提取服务列表（去重）。
func extractServices(entries []LogEntryRaw) []string {
	seen := make(map[string]bool)
	var services []string
	for _, e := range entries {
		svc := e.Labels["service"]
		if svc != "" && !seen[svc] {
			seen[svc] = true
			services = append(services, svc)
		}
	}
	return services
}

// extractLevels 提取级别列表（去重）。
func extractLevels(entries []LogEntryRaw) []string {
	seen := make(map[string]bool)
	var levels []string
	for _, e := range entries {
		lvl := e.Labels["level"]
		if lvl != "" && !seen[lvl] {
			seen[lvl] = true
			levels = append(levels, lvl)
		}
	}
	return levels
}
