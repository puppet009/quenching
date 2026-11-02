// review_worker.go 复盘异步任务（RabbitMQ 消费者），随 internal/worker 野包
// 归位并入 service 层（g4）。
//
// 架构：
//
//	producer → review.exchange → review.generate 队列 → worker → 调 AI → 回写 DB
//	                                        ↓ (失败超限 NACK)
//	                                  review.dlx → review.generate.dlq (死信)
//
// 重试策略：
//   - 消费失败 → DB retry_count + 1
//   - 未达最大重试次数 → 当前消费 goroutine 线性退避后 NACK + requeue
//   - 达最大重试次数 → NACK + 不重入队 → 进死信队列 → DB 标记 failed
//
// T4-16：并发消费（WorkerConfig.Concurrency 个 goroutine 共用 channel，Qos 预取
// 同值）；每个 goroutine 持有独立生成器实例；退避只阻塞当前 goroutine，
// 不再队头阻塞整条队列；单任务有独立超时 ctx，不受进程停止信号中途打断。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"

	"github.com/quenching/review-service/internal/model"
)

// 队列/交换机常量。
const (
	ExchangeName   = "review.exchange"
	DLExchangeName = "review.dlx"
	QueueName      = "review.generate"
	DLQueueName    = "review.generate.dlq"
	RoutingKey     = "review.generate"
	MaxRetryCount  = 3 // 最大重试次数（含首次共 4 次）
)

// ReviewTask 复盘生成任务消息。
type ReviewTask struct {
	ReviewID  int64  `json:"review_id"`
	SessionID int64  `json:"session_id"`
	UserID    int64  `json:"user_id"`
	ProjectID int64  `json:"project_id"`
	TraceID   string `json:"trace_id"`
}

// Marshal 序列化。
func (t *ReviewTask) Marshal() ([]byte, error) {
	return json.Marshal(t)
}

// Unmarshal 反序列化。
func (t *ReviewTask) Unmarshal(data []byte) error {
	return json.Unmarshal(data, t)
}

// ReviewTaskProducer 复盘任务生产者。
type ReviewTaskProducer struct {
	conn     *amqp.Connection
	ch       *amqp.Channel
	exchange string
}

// NewReviewTaskProducer 创建生产者并声明交换机/队列。
func NewReviewTaskProducer(url string) (*ReviewTaskProducer, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("连接 RabbitMQ 失败: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("打开 channel 失败: %w", err)
	}

	// 声明死信交换机
	if err := ch.ExchangeDeclare(DLExchangeName, "direct", true, false, false, false, nil); err != nil {
		conn.Close()
		return nil, fmt.Errorf("声明死信交换机失败: %w", err)
	}

	// 声明死信队列
	if _, err := ch.QueueDeclare(DLQueueName, true, false, false, false, nil); err != nil {
		conn.Close()
		return nil, fmt.Errorf("声明死信队列失败: %w", err)
	}
	if err := ch.QueueBind(DLQueueName, RoutingKey, DLExchangeName, false, nil); err != nil {
		conn.Close()
		return nil, fmt.Errorf("绑定死信队列失败: %w", err)
	}

	// 声明主交换机
	if err := ch.ExchangeDeclare(ExchangeName, "topic", true, false, false, false, nil); err != nil {
		conn.Close()
		return nil, fmt.Errorf("声明主交换机失败: %w", err)
	}

	// 声明主队列（带死信配置）
	_, err = ch.QueueDeclare(QueueName, true, false, false, false, amqp.Table{
		"x-dead-letter-exchange":    DLExchangeName,
		"x-dead-letter-routing-key": RoutingKey,
	})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("声明主队列失败: %w", err)
	}

	// 绑定主队列
	if err := ch.QueueBind(QueueName, RoutingKey, ExchangeName, false, nil); err != nil {
		conn.Close()
		return nil, fmt.Errorf("绑定主队列失败: %w", err)
	}

	return &ReviewTaskProducer{conn: conn, ch: ch, exchange: ExchangeName}, nil
}

// Publish 投递复盘任务。
func (p *ReviewTaskProducer) Publish(ctx context.Context, task *ReviewTask) error {
	body, err := task.Marshal()
	if err != nil {
		return fmt.Errorf("序列化任务失败: %w", err)
	}

	return p.ch.PublishWithContext(ctx, p.exchange, RoutingKey, false, false,
		amqp.Publishing{
			ContentType:  "application/json",
			Body:         body,
			DeliveryMode: amqp.Persistent, // 持久化
			Timestamp:    time.Now(),
		})
}

// Close 关闭连接。
func (p *ReviewTaskProducer) Close() {
	if p.ch != nil {
		_ = p.ch.Close()
	}
	if p.conn != nil {
		_ = p.conn.Close()
	}
}

// ---------- Consumer ----------

// ReviewGenerator 复盘生成器接口（实际调用 AI 服务的部分）。
// 抽象为接口便于单测 mock，以及未来切换 gRPC/HTTP 调用方式。
type ReviewGenerator interface {
	// Generate 生成复盘。返回标题、摘要、总体评价、技术分、协作分、耗时（毫秒）、错误。
	Generate(ctx context.Context, task *ReviewTask) (title, summary, overall string, techScore, collabScore float64, durationMs int, err error)
}

// MethodSectionsGenerator 方法论复盘扩展端口（T3-1，可选能力）。
//
// 七返回值 Generate 只能携带根因（summary）/时间线（overall）两章文本，
// 路径对比第三章没有返回槽位。方法级生成器额外实现本接口：Generate 成功后
// worker 经 MethodSections() 取出含路径对比的三章，映射为 level=method 的
// review_sections。未实现本接口的生成器（基础生成器/D2 fake）行为完全不变。
//
// 调用契约：每个消费 goroutine 使用独立生成器实例（见 GeneratorFactory），
// 在每次 Generate 成功后立即调用；方法返回该实例最近一次成功生成的章节，
// 失败生成后应返回 nil。T4-16 起支持多 goroutine 并发消费。
type MethodSectionsGenerator interface {
	MethodSections() *model.ReviewSections
}

// SectionCollector 确定性附加章节的采集与生成端口（T3-13 坑点章节，M5 修改记录章节）。
// 每个实现负责一类不经过 AI 的确定性章节：取数 + 规则生成 Markdown。
// 无内容时返回 nil, nil（不生成章节）；采集失败由 worker 仅告警不阻断主流程。
type SectionCollector interface {
	// CollectAndBuild 采集会话数据并生成一个附加复盘章节。
	// 无内容时返回 nil, nil（不生成章节）。
	CollectAndBuild(ctx context.Context, reviewID, sessionID int64) (*model.ReviewSection, error)
}

// WorkerConfig worker 消费参数（T4-16：并发消费 + 独立任务超时）。
type WorkerConfig struct {
	// Concurrency 消费并发度：同时处理多少条复盘任务。Qos prefetch 取同值，
	// 每条在途消息占用一个预取额度。默认 1（保持旧行为），生产配置 4。
	Concurrency int
	// TaskTimeout 单任务总超时（取数 + LLM + 落库），独立于进程生命周期，
	// 默认 340s，应略大于 ai.call_timeout（310s），让 gRPC 超时先触发。
	TaskTimeout time.Duration
	// BackoffBase 重试退避基数：第 n 次重试等待 n*BackoffBase。
	// 退避只阻塞当前消费 goroutine，不再队头阻塞整条队列。
	BackoffBase time.Duration
}

func (c WorkerConfig) withDefaults() WorkerConfig {
	if c.Concurrency <= 0 {
		c.Concurrency = 1
	}
	if c.TaskTimeout <= 0 {
		c.TaskTimeout = 340 * time.Second
	}
	if c.BackoffBase <= 0 {
		c.BackoffBase = 5 * time.Second
	}
	return c
}

// GeneratorFactory 为每个消费 goroutine 创建独立生成器实例。
//
// MethodReviewGenerator 内部缓存“最近一次成功生成的六章”（实例级 last 字段，
// 七返回值签名锁定的变通端口），多 goroutine 共享同一实例会串章节，
// 因此并发消费时必须一个消费者一个实例。生成器的其余依赖（gRPC 连接、
// 输入仓储、模板提供器）均并发安全，由工厂闭包共享。
type GeneratorFactory func() ReviewGenerator

// ReviewWorker 复盘任务消费者。
type ReviewWorker struct {
	cfg          WorkerConfig
	conn         *amqp.Connection
	ch           *amqp.Channel
	repo         ReviewTaskRepo
	newGenerator GeneratorFactory
	collectors   []SectionCollector
	growth       *GrowthPipeline
	logger       *zap.Logger
}

func (w *ReviewWorker) WithGrowthPipeline(p *GrowthPipeline) *ReviewWorker { w.growth = p; return w }

// ErrGeneratedSectionsEmpty 生成成功但章节为空（不应发生：schema 校验已前置拦截），
// 统一按失败重试处理，避免空报告落库。
var ErrGeneratedSectionsEmpty = errors.New("review sections are empty")

// ReviewTaskRepo Worker 需要的仓储接口。
// 与 review_service.go 中面向业务用例的 ReviewRepo（7 方法）职责不同：
// 本端口仅含异步生成回写所需的 6 个方法，repository.ReviewStore 同时满足两者。
type ReviewTaskRepo interface {
	GetByID(ctx context.Context, id int64) (*model.Review, error)
	MarkGenerating(ctx context.Context, id int64) error
	MarkCompleted(ctx context.Context, id int64, title, summary, overall string, durationMs int, techScore, collabScore float64, pitfallCount, pitfallRecognized int) error
	MarkFailed(ctx context.Context, id int64, errMsg string) error
	IncrementRetry(ctx context.Context, id int64) (int, error)
	// ReplaceSections 幂等整章重写（事务内先删后插），供消息 requeue/重试安全重放。
	ReplaceSections(ctx context.Context, reviewID int64, sections []model.ReviewSection) error
}

// NewWorker 创建消费者。
// newGenerator 为每个消费 goroutine 提供独立生成器实例（原因见
// GeneratorFactory 注释）；collectors 为确定性附加章节采集器
// （坑点分析、助手修改记录等），不传时复盘只含 AI 生成的主章节。
// cfg 零值时采用默认参数（并发度 1）。
func NewReviewWorker(url string, cfg WorkerConfig, repo ReviewTaskRepo, newGenerator GeneratorFactory, logger *zap.Logger, collectors ...SectionCollector) (*ReviewWorker, error) {
	cfg = cfg.withDefaults()
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("连接 RabbitMQ 失败: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("打开 channel 失败: %w", err)
	}

	// 预取：总量等于消费并发度，一条在途消息占用一个额度，
	// 避免单消费者囤积消息而其余消费者空闲。
	if err := ch.Qos(cfg.Concurrency, 0, false); err != nil {
		conn.Close()
		return nil, fmt.Errorf("设置 QoS 失败: %w", err)
	}

	return &ReviewWorker{
		cfg:          cfg,
		conn:         conn,
		ch:           ch,
		repo:         repo,
		newGenerator: newGenerator,
		collectors:   collectors,
		logger:       logger,
	}, nil
}

// Start 开始消费（阻塞至 ctx 取消或消息通道关闭）。
// 调用方应在 goroutine 中运行。concurrency 个消费 goroutine 共用一个
// channel/queue，由 RabbitMQ 在它们之间分发消息。
func (w *ReviewWorker) Start(ctx context.Context) error {
	msgs, err := w.ch.Consume(QueueName, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("开始消费失败: %w", err)
	}

	w.logger.Info("复盘任务 worker 启动",
		zap.String("queue", QueueName),
		zap.Int("concurrency", w.cfg.Concurrency),
	)

	var wg sync.WaitGroup
	for i := 0; i < w.cfg.Concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			w.consume(ctx, msgs, workerID)
		}(i + 1)
	}
	wg.Wait()

	w.logger.Info("复盘任务 worker 已全部停止")
	return nil
}

// consume 单个消费循环。每个 goroutine 持有独立 generator 实例。
func (w *ReviewWorker) consume(ctx context.Context, msgs <-chan amqp.Delivery, workerID int) {
	// 独立实例，避免 MethodSections 端口的实例级 last 缓存并发串章节。
	gen := w.newGenerator()
	logger := w.logger.With(zap.Int("worker_id", workerID))
	logger.Info("消费 goroutine 启动")

	for {
		select {
		case <-ctx.Done():
			logger.Info("消费 goroutine 停止")
			return
		case msg, ok := <-msgs:
			if !ok {
				logger.Info("消息通道关闭，消费 goroutine 退出")
				return
			}
			w.handleMessage(context.Background(), gen, msg, logger)
		}
	}
}

// handleMessage 处理单条消息。
// parent 显式传 context.Background（见 consume），并在此包一层单任务超时：
// 任务生命周期独立于进程停止信号与消费循环，慢 LLM 调用不会被中途取消，
// RabbitMQ 心跳也不受单任务阻塞影响（amqp 库内部独立 goroutine）。
func (w *ReviewWorker) handleMessage(parent context.Context, gen ReviewGenerator, msg amqp.Delivery, baseLogger *zap.Logger) {
	ctx, cancel := context.WithTimeout(parent, w.cfg.TaskTimeout)
	defer cancel()

	var task ReviewTask
	if err := task.Unmarshal(msg.Body); err != nil {
		baseLogger.Error("解析复盘任务失败", zap.Error(err))
		// 解析失败直接丢弃（进死信也没用）
		_ = msg.Nack(false, false)
		return
	}

	logger := baseLogger.With(
		zap.Int64("review_id", task.ReviewID),
		zap.Int64("session_id", task.SessionID),
	)

	logger.Info("收到复盘生成任务")

	if w.growth != nil {
		exists, e := w.growth.HasJob(ctx, task.ReviewID)
		if e != nil {
			_ = msg.Nack(false, true)
			return
		}
		if exists {
			rv, e := w.repo.GetByID(ctx, task.ReviewID)
			if e != nil {
				_ = msg.Nack(false, true)
				return
			}
			if rv.GenerationStatus == model.GenStatusCompleted || rv.GenerationStatus == model.GenStatusFailed {
				_ = msg.Ack(false)
				return
			}
			if e = w.repo.MarkGenerating(ctx, task.ReviewID); e != nil {
				_ = msg.Nack(false, true)
				return
			}
			if e = w.growth.Step(ctx, task.ReviewID); e != nil && !errors.Is(e, ErrGrowthBusy) {
				logger.Warn("成长复盘步骤等待持久任务恢复", zap.Error(e))
			}
			_ = msg.Ack(false)
			return
		}
	}

	// 标记生成中
	if err := w.repo.MarkGenerating(ctx, task.ReviewID); err != nil {
		logger.Error("标记生成中失败", zap.Error(err))
		// DB 失败重新入队
		_ = msg.Nack(false, true)
		return
	}

	// 调用 AI 生成复盘
	title, summary, overall, techScore, collabScore, durationMs, err := gen.Generate(ctx, &task)
	if err != nil {
		logger.Error("复盘生成失败", zap.Error(err))
		w.handleFailure(ctx, msg, &task, err, logger)
		return
	}

	// 章节结构化：基础复盘落根因/时间线两章（T2-30）；方法论复盘（T3-1/T3-2）
	// 若生成器实现 MethodSectionsGenerator，则落根因/时间线/路径对比/亮点坑点/
	// 方法论/知识点六章。
	// 生成器已做过 schema 强校验，这里再防御一层：为空按失败重试，
	// 避免 completed 状态却没有章节正文。
	sections := sectionsFromGenerator(gen, task.ReviewID, summary, overall)
	if len(sections) == 0 {
		logger.Error("生成结果章节为空，按失败重试处理")
		w.handleFailure(ctx, msg, &task, ErrGeneratedSectionsEmpty, logger)
		return
	}

	// 确定性附加章节（规则生成，不调 AI）：坑点分析（T3-13）、助手修改记录
	// （M5）等。采集器无内容返回 nil 跳过；单个采集失败只告警，不阻断复盘。
	// 附加章节自身不知道当次报告级别，这里按主章节级别统一对齐后再落库。
	sectionLevel := sections[0].SectionLevel
	pitfallCount := 0
	pitfallRecognized := 0
	for _, collector := range w.collectors {
		extra, cErr := collector.CollectAndBuild(ctx, task.ReviewID, task.SessionID)
		if cErr != nil {
			logger.Warn("附加章节采集失败，跳过", zap.Error(cErr))
			continue
		}
		if extra == nil {
			continue
		}
		extra.SectionLevel = sectionLevel

		// 坑点章节的统计要随 MarkCompleted 落到 reviews 主表，按章节类型识别，
		// 其余章节（如修改记录）无主表统计字段。
		if extra.SectionType == model.SectionTypeFixAIPitfallAnalysis && len(extra.ContentData) > 0 {
			var analysis model.PitfallAnalysis
			if jErr := json.Unmarshal(extra.ContentData, &analysis); jErr == nil {
				pitfallCount = analysis.TotalCount
				pitfallRecognized = analysis.RecognizedCount
			}
		}
		sections = append(sections, *extra)
	}

	// 章节先落库（幂等整章重写），再标记完成：requeue/重试重放安全，
	// 且不会出现 completed 但章节缺失的中间态对读者可见。
	if err := w.repo.ReplaceSections(ctx, task.ReviewID, sections); err != nil {
		logger.Error("章节落库失败", zap.Error(err))
		// DB 失败重新入队，下次重放先删后插不会产生重复章节
		_ = msg.Nack(false, true)
		return
	}

	// 标记完成
	if err := w.repo.MarkCompleted(ctx, task.ReviewID, title, summary, overall, durationMs, techScore, collabScore, pitfallCount, pitfallRecognized); err != nil {
		logger.Error("标记完成失败", zap.Error(err))
		// DB 失败重新入队
		_ = msg.Nack(false, true)
		return
	}

	logger.Info("复盘生成完成", zap.Int("duration_ms", durationMs))
	_ = msg.Ack(false)
}

// sectionsFromGenerator 把生成器成功结果映射为待落库的章节明细（纯函数，便于单测）。
//
// 生成器实现 MethodSectionsGenerator 且本次返回了方法级六章时，走方法论映射
// （level=method，含路径对比/亮点坑点/方法论/知识点）；端口返回 nil（失败
// Generate 后的契约状态）时退回基础两章（level=basic）。端口返回非 nil 但
// 六章不全（理论上不会发生：ParseMethodSections 已强校验）时返回 nil，由
// 调用方按 ErrEmptySections 失败重试，避免半成品章节落库。
func sectionsFromGenerator(gen ReviewGenerator, reviewID int64, summary, overall string) []model.ReviewSection {
	if mg, ok := gen.(MethodSectionsGenerator); ok {
		if s := mg.MethodSections(); s != nil {
			return model.MethodSections(reviewID, s)
		}
	}
	return model.BasicSections(reviewID, &model.ReviewSections{
		RootCause: summary,
		Timeline:  overall,
	})
}

// handleFailure 处理生成失败：重试 or 进死信。
func (w *ReviewWorker) handleFailure(ctx context.Context, msg amqp.Delivery, task *ReviewTask, genErr error, logger *zap.Logger) {
	// 重试次数 +1
	retryCount, err := w.repo.IncrementRetry(ctx, task.ReviewID)
	if err != nil {
		logger.Error("增加重试次数失败", zap.Error(err))
		// DB 出问题就先重入队
		_ = msg.Nack(false, true)
		return
	}

	if retryCount < MaxRetryCount {
		// 还有重试次数 → 退避后重新入队。
		// T4-16：退避发生在当前消费 goroutine 内，只延后本任务的 Nack，
		// 其余 goroutine 继续消费，不再像单 worker 时队头阻塞整条队列。
		// 等待可被任务 ctx（超时/停止）打断。
		backoff := nextBackoff(retryCount, w.cfg.BackoffBase)
		logger.Warn("复盘生成失败，将重试",
			zap.Int("retry_count", retryCount),
			zap.Int("max_retry", MaxRetryCount),
			zap.Duration("backoff", backoff),
			zap.Error(genErr),
		)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			logger.Warn("退避等待被任务上下文打断，直接重新入队", zap.Error(ctx.Err()))
		}
		_ = msg.Nack(false, true)
		return
	}

	// 重试耗尽 → 进死信队列 + 标记 failed
	logger.Error("复盘生成失败，重试耗尽，进入死信队列",
		zap.Int("retry_count", retryCount),
		zap.Error(genErr),
	)

	// 标记 DB failed
	if markErr := w.repo.MarkFailed(ctx, task.ReviewID, genErr.Error()); markErr != nil {
		logger.Error("标记失败状态失败", zap.Error(markErr))
	}

	// NACK 不重入队 → 进死信队列
	_ = msg.Nack(false, false)
}

// nextBackoff 计算第 retryCount 次重试的线性退避时长（纯函数，便于单测）。
// retryCount 从 1 开始：5s、10s、15s……
func nextBackoff(retryCount int, base time.Duration) time.Duration {
	if retryCount < 1 {
		retryCount = 1
	}
	return time.Duration(retryCount) * base
}

// Close 关闭连接。
func (w *ReviewWorker) Close() {
	if w.ch != nil {
		_ = w.ch.Close()
	}
	if w.conn != nil {
		_ = w.conn.Close()
	}
}
