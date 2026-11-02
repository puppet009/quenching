// 主动线索推送器与数据端口（T3-7）。
//
// CluePusher 注册为 StallMonitor 的 OnStall 钩子，在会话 normal → stalled 翻转时：
//  1. 查会话属主（会话已结束/查不到则不打扰）；
//  2. 以落库事实做频控：以"最近一次有效活动（回落开训时间）"为周期锚点，
//     同一锚点内同原因只推一条；锚点只取决于库内事实，重启/多副本下不丢不重；
//  3. 渲染确定性提问式文案，落库为带 structured_data 标记的助手消息；
//  4. 通过 ws-gateway 向属主发布单帧 ai_message 事件。
//
// 任意环节失败只记日志：推送是增强能力，绝不影响卡顿扫描与学员的正常请求。
//
// ClueStore 把 PG 访问隔离在 service 层之外：核心推送流程只依赖该接口，
// 生产实现见 repository.ClueRepo，单测使用内存 fake。
package service

import (
	"context"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/quenching/training-service/internal/model"
	"github.com/quenching/training-service/internal/pkg/eventbus"
)

// ClueStore 主动线索的数据端口。
type ClueStore interface {
	// SessionOwner 取训练会话属主用户 ID 与开训时间。
	// ok=false 表示会话不存在、已结束或已删除（此时不应打扰，调用方跳过推送）。
	// startedAt 可能为零值（未正式开训），由调用方结合锚点判断。
	SessionOwner(ctx context.Context, sessionID int64) (userID int64, startedAt time.Time, ok bool, err error)

	// LastActivityAnchor 取本次卡顿周期的稳定锚点：会话最近一次有效活动时刻
	// （session_operations 或用户提问，取与卡顿扫描相同的进度窗口口径）；
	// 从无活动时回落到 started_at；两者都缺失返回零值。
	// 该锚点只取决于库内事实、与进程无关：同一卡顿周期内多轮扫描/进程重启/多副本
	// 取到的值一致，恢复后出现新活动才前移，是跨重启、多副本安全频控的关键。
	LastActivityAnchor(ctx context.Context, sessionID int64, startedAt, now time.Time) (time.Time, error)

	// EnsureConversation 取该会话最近的训练助教对话 ID；没有则自动创建一条，
	// 返回对话 ID。线索需要挂在一条 ai_conversations 上才能落库与回看。
	EnsureConversation(ctx context.Context, sessionID, userID int64) (conversationID int64, err error)

	// CountCluesFromAnchor 统计某会话在锚点（含）之后已落库的指定原因主动线索条数，
	// 用于"同一卡顿周期内同类不重复"的频控；锚点取自库内事实，
	// 因此进程重启/多副本下计数一致，不丢不重。
	CountCluesFromAnchor(ctx context.Context, sessionID int64, reason string, anchor time.Time) (int, error)

	// AppendClue 把主动线索作为一条助手消息落库（同事务累加对话消息数），
	// 在 structured_data 写入主动线索标记与卡顿周期锚点，返回新消息 ID。
	AppendClue(ctx context.Context, conversationID int64, content, reason string, anchor time.Time) (messageID int64, err error)
}

// CluePusher 主动线索推送器；零值不可用，请用 NewCluePusher 构造。
type CluePusher struct {
	cfg       model.ClueConfig
	store     ClueStore
	publisher eventbus.Publisher // 可为 nil：ws-gateway 未配置时只落库不发事件

	// inflight 跟踪异步派发，主要供测试等待全部后台处理结束；生产路径无需等待。
	inflight sync.WaitGroup
}

// NewCluePusher 创建推送器。publisher 允许为 nil（静默降级为仅落库）。
func NewCluePusher(store ClueStore, publisher eventbus.Publisher, cfg model.ClueConfig) *CluePusher {
	return &CluePusher{
		cfg:       cfg.Normalize(),
		store:     store,
		publisher: publisher,
	}
}

// OnStall 实现 StallHook。钩子运行在扫描器的评估路径上且涉及多次 IO，
// 这里只做非阻塞派发，实际处理在独立 goroutine 中以短超时完成。
func (p *CluePusher) OnStall(_ context.Context, st model.StallStatus) {
	if !p.cfg.Enabled {
		return
	}
	if st.Reason == model.StallReasonNone {
		return
	}
	p.inflight.Add(1)
	go func() {
		defer p.inflight.Done()
		// 后台 goroutine 兜底：任何意外 panic 都不能波及扫描进程。
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[clue] OnStall 处理异常已恢复 session=%d reason=%s: %v", st.SessionID, st.Reason, r)
			}
		}()
		// 越出请求/扫描生命周期，使用独立上下文，不复用钩子 ctx。
		p.HandleStall(context.Background(), st)
	}()
}

// Wait 等待全部异步派发结束（仅测试使用）。
func (p *CluePusher) Wait() { p.inflight.Wait() }

// HandleStall 同步执行一次卡顿线索推送，供 OnStall 异步调用，也供单测直接验证。
func (p *CluePusher) HandleStall(ctx context.Context, st model.StallStatus) {
	sessionID := st.SessionID
	reason := string(st.Reason)

	persistCtx, cancel := context.WithTimeout(ctx, p.cfg.PersistTimeout)
	defer cancel()

	// 1. 属主校验：只打扰 running 会话的真实属主；同时取开训时间作为锚点回落。
	userID, startedAt, ok, err := p.store.SessionOwner(persistCtx, sessionID)
	if err != nil {
		log.Printf("[clue] 查询会话属主失败，跳过推送 session=%d: %v", sessionID, err)
		return
	}
	if !ok {
		log.Printf("[clue] 会话不存在或已结束，跳过推送 session=%d", sessionID)
		return
	}

	// 2. 取卡顿周期锚点（最近一次有效活动，无活动回落开训时间）。
	//    锚点只由库内事实决定，不使用扫描器内存态的 st.Since：后者在进程重启/多副本
	//    下会回到各自的评估时刻，无法跨进程去重。锚点缺失（未开训且查无活动）时
	//    保守不打扰。
	anchor, err := p.store.LastActivityAnchor(persistCtx, sessionID, startedAt, st.EvaluatedAt)
	if err != nil {
		log.Printf("[clue] 取卡顿周期锚点失败，跳过推送 session=%d: %v", sessionID, err)
		return
	}
	if anchor.IsZero() {
		log.Printf("[clue] 会话缺少开训时间且无有效活动，无法确定卡顿周期，跳过推送 session=%d", sessionID)
		return
	}

	// 3. 频控：同一卡顿周期（同一锚点）内同原因只推一条。
	count, err := p.store.CountCluesFromAnchor(persistCtx, sessionID, reason, anchor)
	if err != nil {
		log.Printf("[clue] 频控计数失败，为避免打扰跳过推送 session=%d reason=%s: %v", sessionID, reason, err)
		return
	}
	if count > 0 {
		log.Printf("[clue] 本卡顿周期已推送过同类线索，跳过 session=%d reason=%s", sessionID, reason)
		return
	}

	// 4. 渲染确定性文案并归属对话（无对话则自动创建）。
	content := RenderClue(reason)
	conversationID, err := p.store.EnsureConversation(persistCtx, sessionID, userID)
	if err != nil {
		log.Printf("[clue] 归属/创建对话失败，跳过推送 session=%d: %v", sessionID, err)
		return
	}

	// 5. 落库（structured_data 标记主动线索与周期锚点），保证历史可回看、频控可计数。
	messageID, err := p.store.AppendClue(persistCtx, conversationID, content, reason, anchor)
	if err != nil {
		log.Printf("[clue] 线索落库失败，跳过事件发布 session=%d: %v", sessionID, err)
		return
	}

	log.Printf("[clue] 主动线索已落库 session=%d conversation=%d message=%d reason=%s anchor=%s",
		sessionID, conversationID, messageID, reason, anchor.Format(time.RFC3339))

	// 6. 发布 ai_message 事件（publisher 未配置则仅落库）。
	if p.publisher == nil {
		log.Printf("[clue] ws-gateway 未配置，线索仅落库未推送 session=%d", sessionID)
		return
	}

	publishCtx, publishCancel := context.WithTimeout(ctx, p.cfg.PublishTimeout)
	defer publishCancel()

	data := eventbus.AIMessageEventData{
		SessionID:   strconv.FormatInt(sessionID, 10),
		MessageID:   strconv.FormatInt(messageID, 10),
		Role:        "assistant",
		Content:     content,
		IsStreaming: false,
		IsEnd:       true,
		Timestamp:   time.Now().UnixMilli(),
	}
	if err := p.publisher.PublishToUser(publishCtx, userID, eventbus.EventAIMessage, data); err != nil {
		// 线索已落库，事件失败不回滚、不重试，避免重复打扰。
		log.Printf("[clue] ai_message 事件发布失败（线索已落库） session=%d user=%d: %v", sessionID, userID, err)
		return
	}
	log.Printf("[clue] 主动线索已推送 session=%d user=%d reason=%s", sessionID, userID, reason)
}
