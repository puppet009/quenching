// Package service 中 AI 对话编排（T2-27）。
//
// training-service 是训练会话的编排中枢，也是 AI 对话的属主：
// - 会话/消息在本服务落库（ai_conversations/ai_messages）；
// - 多轮历史由本服务从库中重建并脱敏后传给 ai-service（系统提示不泄露根因）；
// - ai-service 只负责按固定提示生成文案，坑点由本服务（T3）决定，MVP 不启用。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/quenching/training-service/internal/model"
)

var (
	ErrConversationNotFound = errors.New("对话不存在或无权访问")
	ErrConversationClosed   = errors.New("对话已关闭")
	ErrEmptyMessage         = errors.New("消息内容为空")
)

// 历史携带条数（与 ai-service 的最近 20 轮收敛保持一致）。
const historyLimit = 20

// agentLevel 默认 A1（AI 助手）；A0 纯人工不会发起对话。
const defaultAgentLevel = "A1"

// ChatService AI 对话编排服务。
type ChatService struct {
	toolAudit         *ToolAudit
	historyTools      *SessionHistoryTools
	memoryContext     *SessionMemoryService
	sessionMemory     SessionMemoryStore
	chatRenewInterval time.Duration
	store             ConversationStore
	ai                ChatStreamer
	model             string // 记录使用的模型（mock 阶段为 "mock"）
	version           string // 提示词版本标识（完整提示词由 ai-service prompts 模块属主）

	// T3-9 坑点触发判定（可选依赖）。二者均为 nil 时坑点能力关闭，
	// 对话完全走原有 LLM 路径，保证旧测试与未装配环境行为不变。
	engine  *Engine
	pitfall PitfallRuntime

	// T3-12 踩坑后果传导（可选依赖）。consequenceStore 未装配时整条链路关闭；
	// executor 未就绪（云上 fault-engine 运行时未接线）时不登记、不注入。
	// 任何失败只记日志、绝不阻断 LLM 对话。
	consequenceStore    ConsequenceStore
	consequenceExecutor ConsequenceExecutor

	// T3-4 脱敏故障上下文供给（可选依赖）。仅训练对话在调 ai-service 前尽力加载，
	// 把"现象/类目/难度/状态"随请求透传给助教；未装配或加载失败时安静退化为无上下文引导。
	faultContext FaultContextProvider
	brainContext interface {
		BuildStudentBrainContext(context.Context, int64) (*StudentBrainContext, error)
	}
	brainHints interface {
		ReserveBrainHint(context.Context, int64, int) (int, bool, error)
	}

	// T3-5 渐进式提示配额供给（可选依赖）。未装配时提示请求收到错误帧，
	// 普通应答行为完全不变。
	hintQuota HintQuotaProvider

	// Agent 工具调用（Function Calling）。toolExecutor 未装配时不启用工具，
	// 对话完全走原路径；装配后训练对话会向 ai-service 声明工具白名单，
	// 并在 LLM 请求工具时自动执行观察类工具、驱动多轮循环。
	toolExecutor ToolExecutor

	// 修改类工具的用户审批中介：工具循环在此挂起等待前端“同意/拒绝”。
	approvals *ApprovalBroker
}

func NewChatService(store ConversationStore, ai ChatStreamer, aiModel, promptVersion string) *ChatService {
	if aiModel == "" {
		aiModel = "mock"
	}
	if promptVersion == "" {
		promptVersion = "training_assistant:v1"
	}
	return &ChatService{store: store, ai: ai, model: aiModel, version: promptVersion, approvals: NewApprovalBroker()}
}

// EnableTools 装配 Agent 工具执行器（Function Calling）。executor 为 nil 时
// 不启用，对话维持纯对话路径；装配后训练对话自动声明并执行观察类工具。
func (s *ChatService) EnableTools(executor ToolExecutor) {
	if executor == nil {
		return
	}
	s.toolExecutor = executor
}

// ResolveApproval 投递用户对某修改类工具的审批决定（HTTP 审批端点调用）。
// 令牌不存在（已超时/已处理/非法）时返回 false。
func (s *ChatService) ResolveApproval(approvalID string, approved bool) bool {
	if s.approvals == nil {
		return false
	}
	return s.approvals.Resolve(approvalID, approved)
}

// OwnsConversation 判断对话是否属于该用户（审批端点归属校验）。任何读取错误按无权限处理。
func (s *ChatService) OwnsConversation(ctx context.Context, conversationID, userID int64) bool {
	conv, err := s.store.GetConversation(ctx, conversationID, userID)
	if err != nil {
		return false
	}
	return conv != nil
}

// toolsEnabled 判断当前请求是否应启用工具：执行器已装配且为训练场景。
func (s *ChatService) toolsEnabled(sessionID int64) bool {
	return s.toolExecutor != nil && sessionID != 0
}

// EnablePitfall 装配规则版坑点引擎与其运行时供给（T3-9）。
// engine 传 nil 时用默认 math/rand 概率源。装配后：训练对话每轮先做坑点判定，
// 命中则确定性模板直出（不经 LLM，保证符合条件 100% 出现），未命中维持原路径。
func (s *ChatService) EnablePitfall(rt PitfallRuntime, engine *Engine) {
	if rt == nil {
		return
	}
	if engine == nil {
		engine = NewEngine(nil)
	}
	s.pitfall = rt
	s.engine = engine
}

// EnableConsequence 装配踩坑后果传导依赖（T3-12）。store 为 nil 时不启用；
// executor 允许为 nil（云上 fault-engine 运行时 HTTP 通道就绪前），此时只不做注入，
// 待云上接线后传入实现即可，编排逻辑无需改动。
func (s *ChatService) EnableConsequence(store ConsequenceStore, executor ConsequenceExecutor) {
	if store == nil {
		return
	}
	s.consequenceStore = store
	s.consequenceExecutor = executor
}

// EnableFaultContext 装配脱敏故障上下文供给（T3-4）。provider 为 nil 时不启用，
// 训练对话退化为无上下文引导。provider 返回 (nil,nil)（故障尚未暴露）同样不带上下文，
// 避免助教在故障出现前"未卜先知"。
func (s *ChatService) EnableFaultContext(provider FaultContextProvider) {
	if provider == nil {
		return
	}
	s.faultContext = provider
}

// StartConversation 创建一次对话。sessionID 可空（非训练场景的问答）。
func (s *ChatService) StartConversation(
	ctx context.Context, userID int64, sessionID *int64, convType string,
) (int64, error) {
	if !validConvType(convType) {
		return 0, fmt.Errorf("非法对话类型: %s", convType)
	}
	if convType == model.ConvTrainingAssistant && sessionID != nil && s.sessionMemory != nil {
		c, e := s.sessionMemory.Resolve(ctx, userID, *sessionID)
		if e != nil {
			return 0, e
		}
		return c.ConversationID, nil
	}
	conv := &model.Conversation{
		UserID:           userID,
		SessionID:        sessionID,
		ConversationType: convType,
		AIModel:          s.model,
		PromptVersion:    s.version,
		Status:           model.ConvStatusActive,
	}
	return s.store.CreateConversation(ctx, conv)
}

// StreamReply 处理一条用户消息并流式返回 AI 回复。
//
// 返回的 channel 在后台 goroutine 中产出事件并最终关闭；调用方（handler）负责消费。
// 用户消息在发起 AI 调用前同步落库（失败则直接返回错误事件）。
// opts 目前仅支持 WithHintLevel（T3-5 渐进式提示）。
func (s *ChatService) StreamReply(
	ctx context.Context, userID, conversationID int64, message string, opts ...ReplyOption,
) <-chan StreamEvent {
	out := make(chan StreamEvent)

	var options replyOptions
	for _, opt := range opts {
		opt(&options)
	}

	message = RedactSessionText(strings.TrimSpace(message))
	if message == "" {
		go func() {
			defer close(out)
			out <- StreamEvent{Type: StreamError, ErrorMsg: ErrEmptyMessage.Error()}
		}()
		return out
	}

	go func() {
		defer close(out)
		// send 在客户端断连（ctx 取消）时放弃下发，避免 goroutine 泄漏；落库仍独立进行。
		send := func(ev StreamEvent) bool {
			select {
			case out <- ev:
				return true
			case <-ctx.Done():
				return false
			}
		}

		fail := func(msg string) { send(StreamEvent{Type: StreamError, ErrorMsg: msg}) }

		conv, err := s.store.GetConversation(ctx, conversationID, userID)
		if err != nil {
			fail("读取对话失败")
			return
		}
		if conv == nil {
			fail(ErrConversationNotFound.Error())
			return
		}
		if conv.Status != model.ConvStatusActive {
			fail(ErrConversationClosed.Error())
			return
		}

		var sessionID int64
		if conv.SessionID != nil {
			sessionID = *conv.SessionID
		}

		if conv.ConversationType == model.ConvTrainingAssistant && sessionID != 0 && s.sessionMemory != nil {
			replyCtx, finish, e := s.beginSessionReply(ctx, userID, sessionID)
			if e != nil {
				fail(e.Error())
				return
			}
			defer finish()
			ctx = replyCtx
			scope, _ := CurrentChatLease(ctx)
			conversationID = scope.Lease.ConversationID
		}

		// T3-5 渐进式提示配额/逐级解锁校验。必须在用户消息落库前完成：
		// 被拒的提示请求不消耗提示次数、不留下消息痕迹，前端可改为普通追问重试。
		// fail-closed：配额供给未装配/读取失败一律拒绝提示，绝不超额泄露线索。
		hintLevel, hintRemaining, err := s.checkHint(ctx, sessionID, options.hintLevel)
		if err != nil {
			fail(err.Error())
			return
		}

		// 1) 先重建脱敏历史（此时不含本次用户消息，避免与 message 重复进入 prompt）。
		var history []model.HistoryTurn
		if scope, ok := CurrentChatLease(ctx); ok {
			page, e := s.sessionMemory.History(ctx, scope.UserID, scope.SessionID, 0, historyLimit)
			err = e
			for _, m := range page.Messages {
				history = append(history, model.HistoryTurn{Role: m.Role, Content: m.Content})
			}
		} else {
			history, err = s.store.ListHistory(ctx, conversationID, historyLimit)
		}
		if err != nil {
			fail("读取历史失败")
			return
		}

		var memoryContextJSON string
		if scope, ok := CurrentChatLease(ctx); ok && s.memoryContext != nil {
			prepared, e := s.memoryContext.Prepare(ctx, scope.UserID, scope.SessionID, message)
			if e != nil {
				fail("班次记忆读取失败，请重试")
				return
			}
			history = prepared.History
			memoryContextJSON = prepared.MemoryJSON
			if prepared.Degraded {
				send(StreamEvent{Type: StreamStep, Step: &StepEvent{StepID: "session-memory", State: "completed", Tool: "session_memory", Display: "班次记忆", Summary: "部分历史尚未整理或显示截断，助手需按需检索原文"}})
			}
		}
		// 2) 本次用户消息落库。
		var saveErr error
		if scope, ok := CurrentChatLease(ctx); ok {
			_, saveErr = s.sessionMemory.AppendLeasedMessage(ctx, scope.UserID, scope.SessionID, scope.Lease, model.SessionMessage{Role: "user", Content: message})
		} else {
			_, saveErr = s.store.AppendUserMessage(ctx, conversationID, message)
		}
		if saveErr != nil {
			fail("消息保存失败")
			return
		}

		// 落库用独立短超时上下文：即便客户端已断连，也尽量把本轮回复保存下来。
		persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()

		// 3) T3-11 先对历史上已注入、尚未判定的坑点做用户反应判定
		//    （本轮用户消息+坑点注入后的操作时间线）。必须先于本轮新坑注入，
		//    否则刚注入的坑会被触发它的那句提问误判。纯尽力而为，失败只记日志。
		//    提示轮（学员主动求提示，不是对坑点的反应）整段跳过坑点判定与注入。
		if hintLevel == 0 && sessionID != 0 && s.pitfall != nil {
			s.judgePitfallResponses(ctx, persistCtx, sessionID, conversationID, message)
		}

		// 4) T3-9 坑点判定：仅训练对话且已装配坑点运行时启用。命中则确定性模板直出，
		//    不调用 LLM，从根上保证"符合条件 100% 出现"；未命中/任何异常则回退原 AI 路径。
		if hintLevel == 0 && sessionID != 0 && s.pitfall != nil && s.engine != nil {
			if injected, ok := s.tryInjectPitfall(ctx, persistCtx, send, sessionID, conversationID, message); ok {
				if injected {
					return
				}
			}
		}

		// 5) T3-4 训练对话尽力加载脱敏故障上下文（仅学员可见信息），随请求透传给助教，
		//    让"主动提问应答"能结合当前故障现象引导思考。任何失败只记日志、绝不阻断对话；
		//    非训练对话（sessionID=0）、未装配供给、故障尚未暴露（nil）时上下文留空。
		var faultContextJSON string
		if sessionID != 0 && s.faultContext != nil {
			fc, err := s.faultContext.LoadFaultContext(ctx, sessionID)
			if err != nil {
				log.Printf("故障上下文加载失败，本轮按无上下文引导: session=%d err=%v", sessionID, err)
			} else if fc != nil && len(fc.Faults) > 0 {
				if raw, mErr := json.Marshal(fc); mErr != nil {
					log.Printf("故障上下文序列化失败，本轮按无上下文引导: session=%d err=%v", sessionID, mErr)
				} else {
					faultContextJSON = string(raw)
				}
			}
		}

		if s.brainContext != nil && sessionID != 0 {
			if public, e := s.brainContext.BuildStudentBrainContext(ctx, sessionID); e == nil && public != nil {
				faultContextJSON = StudentBrainContextJSON(public)
			}
		}

		// 6) Agent 工具循环：调 ai-service；若 LLM 请求工具则执行观察类工具、
		//    累积结果后再次调用，直到 LLM 给出最终答复。普通对话（未装配工具/
		//    非训练场景/提示轮）tools 为空，行为与原单次调用完全一致。
		started := time.Now()
		useTools := hintLevel == 0 && s.toolsEnabled(sessionID)
		toolDefs := AgentToolDefs(useTools)
		if _, ok := CurrentChatLease(ctx); ok && s.historyTools != nil {
			toolDefs = append(toolDefs, SessionHistoryToolDefs()...)
		}
		var accumulated []ToolResult

		// 最多工具轮数，防止异常模型在“调用工具”上空转。
		const maxToolRounds = 8
		for round := 0; round <= maxToolRounds; round++ {
			aiCh, err := s.ai.StreamChat(ctx, ChatStreamInput{
				SessionID:          sessionID,
				ConversationID:     conversationID,
				Message:            message,
				AgentLevel:         defaultAgentLevel,
				ConversationType:   conv.ConversationType,
				History:            history,
				MemoryContextJSON:  memoryContextJSON,
				PitfallInstruction: false,
				FaultContextJSON:   faultContextJSON,
				HintLevel:          hintLevel,
				HintRemaining:      hintRemaining,
				Tools:              toolDefs,
				ToolResults:        accumulated,
			})
			if err != nil {
				fail("AI 服务不可用")
				return
			}

			// 本轮收集：正文、用量、错误、工具调用。
			var rb strings.Builder
			var promptTokens, completionTokens int
			var cost float64
			var gotRoundError string
			var calls []ToolCall
			clientAlive := true
			for ev := range aiCh {
				switch ev.Type {
				case StreamThinking:
					if clientAlive {
						clientAlive = send(StreamEvent{
							Type: StreamThinking, ConversationID: conversationID,
							HintLevel: ev.HintLevel, HintRemaining: ev.HintRemaining,
						})
					}
				case StreamDelta:
					rb.WriteString(ev.Delta)
					if clientAlive {
						clientAlive = send(StreamEvent{
							Type: StreamDelta, Delta: ev.Delta, IsPitfall: ev.IsPitfall,
							HintLevel: ev.HintLevel, HintRemaining: ev.HintRemaining,
						})
					}
				case StreamStep:
					if ev.Step != nil && clientAlive {
						send(StreamEvent{Type: StreamStep, Step: ev.Step})
					}
				case StreamToolCall:
					calls = append(calls, ev.ToolCalls...)
				case StreamDone:
					promptTokens, completionTokens, cost = ev.PromptTokens, ev.CompletionTokens, ev.Cost
				case StreamError:
					gotRoundError = ev.ErrorMsg
				}
			}

			reply := rb.String()

			// 本轮出错：保留已生成片段落库，下发错误帧后结束（与原逻辑一致）。
			if gotRoundError != "" {
				s.persistReply(
					persistCtx, conversationID, reply, hintLevel,
					promptTokens+completionTokens, int(time.Since(started).Milliseconds()), cost,
				)
				if clientAlive {
					send(StreamEvent{Type: StreamError, ErrorMsg: gotRoundError})
				}
				return
			}

			// 没有工具调用：这是最终答复，落库并下发 done，结束循环。
			if len(calls) == 0 {
				latencyMS := int(time.Since(started).Milliseconds())
				msgID, pErr := s.persistReply(
					persistCtx, conversationID, reply, hintLevel,
					promptTokens+completionTokens, latencyMS, cost,
				)
				if pErr != nil {
					if clientAlive {
						send(StreamEvent{Type: StreamError, ErrorMsg: "回复保存失败"})
					}
					return
				}
				if s.toolAudit != nil {
					if linker, ok := s.toolAudit.Store.(interface {
						LinkReply(context.Context, int64, []string, int64) error
					}); ok {
						ids := []string{}
						for _, r := range accumulated {
							if r.OperationID != "" {
								ids = append(ids, r.OperationID)
							}
						}
						if err := linker.LinkReply(persistCtx, sessionID, ids, msgID); err != nil {
							send(StreamEvent{Type: StreamStep, Step: &StepEvent{StepID: "tool-association", State: "unknown", Display: "工具记录关联", Summary: "工具过程已保存，回复关联未确认"}})
						}
					}
				}
				if clientAlive {
					send(StreamEvent{
						Type:             StreamDone,
						ConversationID:   conversationID,
						MessageID:        msgID,
						PromptTokens:     promptTokens,
						CompletionTokens: completionTokens,
						Cost:             cost,
						HintLevel:        hintLevel,
						HintRemaining:    hintRemaining,
					})
				}
				return
			}

			// 本轮请求了工具：执行（观察类自动、修改类先审批），累积结果，进入下一轮。
			for i := range calls {
				calls[i].Round = round
			}
			results, keepLoop := s.runToolCalls(ctx, sessionID, conversationID, calls, toolDefs, send)
			accumulated = append(accumulated, results...)
			remaining := 32 * 1024
			for i := range accumulated {
				if len(accumulated[i].ContentJSON) > remaining {
					accumulated[i].ContentJSON = `{"truncated":true,"note":"工具累计输出预算耗尽；请发起更精准的只读查询，不能宣称读完"}`
				}
				remaining -= len(accumulated[i].ContentJSON)
				if remaining < 0 {
					remaining = 0
				}
			}
			if !keepLoop {
				// 用户/上下文导致无法继续工具循环：直接结束，已下发内容即本轮输出。
				return
			}
		}

		// 工具轮数超限：记录并结束（避免无限循环），此前正文已流式下发。
		log.Printf("工具轮数超过上限，结束 Agent 循环: session=%d conv=%d", sessionID, conversationID)
	}()

	return out
}

// persistReply 按轮次类型把助手回复落库：提示轮落 hint 消息，普通轮落常规消息。
// 返回消息 ID。空回复不新增消息、返回 0。
func (s *ChatService) persistReply(
	ctx context.Context, conversationID int64, reply string, hintLevel, tokens, latencyMS int, cost float64,
) (int64, error) {
	if reply == "" {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if scope, ok := CurrentChatLease(ctx); ok {
		return s.sessionMemory.AppendLeasedMessage(ctx, scope.UserID, scope.SessionID, scope.Lease, model.SessionMessage{Role: "assistant", Content: reply, HintLevel: hintLevel, Tokens: tokens, LatencyMS: latencyMS, Cost: cost})
	}
	if hintLevel > 0 {
		return s.store.AppendHintMessage(ctx, conversationID, reply, hintLevel, tokens, latencyMS, cost)
	}
	return s.store.AppendAssistantMessage(ctx, conversationID, reply, tokens, latencyMS, cost, false, "")
}

// runToolCalls 执行一批工具调用并产出步骤时间线事件（过程透明）。
//
// toolDefs 提供工具是否需用户审批（RequiresApproval）：
//   - 观察类（无需审批）：发 started 步骤后直接执行；
//   - 修改类（需审批）：发 awaiting_approval 步骤（带 approvalID）后阻塞等待，
//     用户同意才执行；拒绝/超时则发 rejected 步骤，并以“已取消”结果回 LLM，
//     由其改给手动引导。
//
// 返回 (结果, 是否继续循环)：执行器整体不可用时 keepLoop=false。
func (s *ChatService) runToolCalls(
	ctx context.Context, sessionID, conversationID int64, calls []ToolCall, toolDefs []ToolDef,
	send func(StreamEvent) bool,
) ([]ToolResult, bool) {
	if s.toolAudit != nil {
		return s.runAuditedToolCalls(ctx, sessionID, conversationID, calls, toolDefs, send)
	}
	if s.toolExecutor == nil && s.historyTools == nil {
		return nil, false
	}

	allowed := map[string]bool{}
	for _, def := range toolDefs {
		allowed[def.Name] = true
	}
	// 需审批工具名集合。
	needApproval := make(map[string]bool, len(toolDefs))
	for _, d := range toolDefs {
		if d.RequiresApproval {
			needApproval[d.Name] = true
		}
	}

	results := make([]ToolResult, 0, len(calls))
	for i, call := range calls {
		if !allowed[call.Name] {
			results = append(results, ToolResult{CallID: call.ID, IsError: true, ContentJSON: `{"error":"tool is not allowed for this reply"}`})
			continue
		}
		call.ConversationID = conversationID
		stepID := fmt.Sprintf("step-%d-%d", sessionID, i+1)
		display := ToolDisplay(call)

		// 修改类工具：先请求用户审批，挂起等待“同意/拒绝”。
		if needApproval[call.Name] {
			approvalID := s.approvals.NewApproval()
			call.ApprovalID = approvalID
			send(StreamEvent{
				Type: StreamStep,
				Step: &StepEvent{
					StepID: stepID, State: "awaiting_approval", Tool: call.Name,
					Display: display, Summary: "等待确认",
					DetailJSON: call.ArgumentsJSON, ApprovalID: approvalID,
				},
			})

			decision, ok := s.approvals.Await(ctx, approvalID)
			if !ok || !decision.Approved {
				// 用户拒绝或等待超时/断连：不执行写操作，步骤标记并回 LLM 取消结果。
				send(StreamEvent{
					Type: StreamStep,
					Step: &StepEvent{
						StepID: stepID, State: "rejected", Tool: call.Name,
						Display: display, Summary: "用户已取消",
					},
				})
				cancelled, _ := json.Marshal(map[string]string{
					"status": "cancelled",
					"reason": "用户未批准该操作，请勿重试；改为给出手动操作步骤引导。",
				})
				results = append(results, ToolResult{
					CallID: call.ID, ContentJSON: string(cancelled), IsError: true,
				})
				continue
			}
		}

		// 步骤开始（观察类直接开始；审批通过的修改类也进入执行）。
		send(StreamEvent{
			Type: StreamStep,
			Step: &StepEvent{StepID: stepID, State: "started", Tool: call.Name, Display: display},
		})

		if e := s.assertSessionReplyLease(ctx); e != nil {
			return results, false
		}
		var content string
		var execErr error
		if isSessionHistoryTool(call.Name) && s.historyTools != nil {
			scope, ok := CurrentChatLease(ctx)
			if !ok {
				execErr = ErrSessionMemoryNotFound
			} else {
				content, execErr = s.historyTools.Execute(ctx, scope.UserID, scope.SessionID, call)
			}
		} else if s.toolExecutor != nil {
			content, execErr = s.toolExecutor.Execute(ctx, sessionID, call)
		} else {
			execErr = errors.New("tool unavailable")
		}
		content = RedactSessionText(content)

		if execErr != nil {
			// 工具失败：步骤标错，但以失败结果回 LLM，让其换思路/继续。
			send(StreamEvent{
				Type: StreamStep,
				Step: &StepEvent{
					StepID: stepID, State: "failed", Tool: call.Name,
					Display: display, Summary: "执行失败",
				},
			})
			errPayload, _ := json.Marshal(map[string]string{"error": execErr.Error()})
			results = append(results, ToolResult{
				CallID: call.ID, ContentJSON: string(errPayload), IsError: true,
			})
			continue
		}

		summary := ToolResultSummary(call, content)
		send(StreamEvent{
			Type: StreamStep,
			Step: &StepEvent{
				StepID: stepID, State: "completed", Tool: call.Name,
				Display: display, Summary: summary, DetailJSON: content,
			},
		})
		results = append(results, ToolResult{CallID: call.ID, ContentJSON: content})
	}
	return results, true
}

func validConvType(t string) bool {
	switch t {
	case model.ConvTrainingAssistant, model.ConvReviewExplainer,
		model.ConvKnowledgeQA, model.ConvArchitectureAdvisor:
		return true
	}
	return false
}

// tryInjectPitfall 执行一轮坑点判定并在命中时确定性直出错误建议（T3-9）。
//
// 返回 (injected, handled)：
//   - handled=false：取配置/运行时快照失败，属基础设施异常；调用方记日志后回退普通 AI 路径，
//     绝不让坑点子系统把训练对话整体打挂；
//   - handled=true, injected=false：判定正常完成但本轮无坑点命中（或已达上限），回退普通 AI 路径；
//   - handled=true, injected=true：坑点消息已直出并落库，调用方结束本轮，不再调用 LLM。
//
// 注入计数每轮从库（ai_messages.is_pitfall）实时重建为 State，服务重启也不丢次数。
func (s *ChatService) tryInjectPitfall(
	ctx, persistCtx context.Context,
	send func(StreamEvent) bool,
	sessionID, conversationID int64, userQuestion string,
) (injected, handled bool) {
	configs, err := s.pitfall.ActiveConfigs(ctx, sessionID)
	if err != nil {
		log.Printf("坑点配置加载失败，本轮回退普通AI: session=%d err=%v", sessionID, err)
		return false, false
	}
	if len(configs) == 0 {
		return false, true
	}

	rtCtx, err := s.pitfall.Snapshot(ctx, sessionID, conversationID, userQuestion)
	if err != nil {
		log.Printf("坑点运行时快照失败，本轮回退普通AI: session=%d err=%v", sessionID, err)
		return false, false
	}

	// 从库重建本会话各坑点的已注入次数。
	st := NewState()
	for _, c := range configs {
		n, err := s.pitfall.InjectedCount(ctx, sessionID, c.ID)
		if err != nil {
			log.Printf("坑点注入次数读取失败，本轮回退普通AI: session=%d config=%d err=%v", sessionID, c.ID, err)
			return false, false
		}
		for i := 0; i < n; i++ {
			st.RecordInjection(c.ID)
		}
	}

	hit := PickFirstFired(s.engine.Evaluate(configs, st, rtCtx))
	if hit == nil {
		return false, true
	}

	inj := BuildInjection(hit, nil)
	msgID, err := s.store.AppendPitfallMessage(
		persistCtx, conversationID, inj.Content, inj.ConfigID, inj.PitfallType, inj.ConfidenceLevel)
	if err != nil {
		log.Printf("坑点消息落库失败，本轮回退普通AI: session=%d config=%d err=%v", sessionID, inj.ConfigID, err)
		return false, false
	}
	log.Printf("注入坑点: session=%d conv=%d %s", sessionID, conversationID, inj)

	// 确定性直出：单帧正文（带坑点标记三字段）+ done，帧结构与普通流式回复一致，
	// 前端无需区分两条路径即可渲染并标记"这是一条坑点建议"。
	if send(StreamEvent{
		Type:            StreamDelta,
		Delta:           inj.Content,
		IsPitfall:       true,
		PitfallConfigID: inj.ConfigID,
		PitfallType:     inj.PitfallType,
		ConfidenceLevel: inj.ConfidenceLevel,
	}) {
		send(StreamEvent{
			Type:            StreamDone,
			ConversationID:  conversationID,
			MessageID:       msgID,
			IsPitfall:       true,
			PitfallConfigID: inj.ConfigID,
			PitfallType:     inj.PitfallType,
			ConfidenceLevel: inj.ConfidenceLevel,
		})
	}
	return true, true
}

// judgePitfallResponses 对本对话中已注入但尚未判定的坑点做一轮用户反应判定（T3-11）。
//
// 判定输入 = 本轮用户回复 reply + 每条坑点注入时刻之后的操作时间线，规则取自该坑点
// 自己的配置（Recognition/PitfallRules，配置化）。调用时机在"本轮新坑注入"之前，
// 因此 ListPendingPitfalls 不会捞到本轮刚触发的坑，杜绝"用触发坑的那句提问自判"。
//
// 纯尽力而为：任何一步基础设施失败只记日志并继续后续步骤/后续对话路径，
// 绝不向调用方返回错误、绝不阻断坑点注入与 LLM 回复。pending（信息不足）的坑
// 保持 user_response_type 为空，下一轮用户再次发言时自然重新参与判定。
func (s *ChatService) judgePitfallResponses(
	ctx, persistCtx context.Context, sessionID, conversationID int64, reply string,
) {
	pending, err := s.store.ListPendingPitfalls(ctx, conversationID)
	if err != nil {
		log.Printf("待判定坑点读取失败，本轮跳过反应判定: session=%d conv=%d err=%v", sessionID, conversationID, err)
		return
	}
	if len(pending) == 0 {
		return
	}

	// 每条坑按自己的配置判定；建 configID -> *Config 索引。
	configs, err := s.pitfall.ActiveConfigs(ctx, sessionID)
	if err != nil {
		log.Printf("坑点配置加载失败，本轮跳过反应判定: session=%d err=%v", sessionID, err)
		return
	}
	cfgByID := make(map[int64]*model.PitfallConfig, len(configs))
	for _, c := range configs {
		cfgByID[c.ID] = c
	}

	for _, p := range pending {
		cfg := cfgByID[p.ConfigID]
		if cfg == nil {
			// 配置已下线/不在挂载缺陷内：无法取得规则，留待后续，不做猜测性判定。
			continue
		}
		// 操作时间线读不到（如 T1-23 尚未落库）时退化为仅按回复文本判定。
		ops, err := s.pitfall.OperationsSince(ctx, sessionID, p.InjectedAt)
		if err != nil {
			log.Printf("操作时间线读取失败，退化为仅文本判定: session=%d config=%d err=%v",
				sessionID, p.ConfigID, err)
			ops = nil
		}

		r := JudgeReaction(cfg, ReactionInput{Reply: reply, Operations: ops})
		if !r.Resolved {
			continue // 信息不足，保持 pending，下一轮继续
		}

		applied, err := s.store.MarkPitfallResponse(persistCtx, conversationID, p.MessageID, r.Response)
		if err != nil {
			log.Printf("坑点反应回写失败: session=%d conv=%d msg=%d response=%s err=%v",
				sessionID, conversationID, p.MessageID, r.Response, err)
			continue
		}
		if applied {
			log.Printf("坑点反应判定: session=%d conv=%d msg=%d config=%d response=%s outcome=%s verified_before_adopt=%t reason=%s",
				sessionID, conversationID, p.MessageID, p.ConfigID,
				r.Response, r.Outcome, r.VerifiedBeforeAdopt, r.Reason)
			// T3-12：采纳错误建议（agree/fell）且配置开启恶化时，联动注入次生故障。
			if r.Response == model.ResponseAgree {
				s.applyConsequence(persistCtx, sessionID, conversationID, p.MessageID, cfg)
			}
		}
	}
}

// applyConsequence 在用户采纳错误建议后联动注入一次次生故障（T3-12）。
//
// 流程：解析规格 -> 幂等登记（同一坑点消息至多一次）-> 执行通道注入 -> 回填句柄/失败终态。
// 纯尽力而为：依赖未装配、登记或注入任一步失败都只记日志返回，绝不影响对话主链路；
// 注入用独立短超时上下文，不依赖可能已被客户端断连取消的请求 ctx。
// 登记先于注入、状态机持久化，进程在二者之间崩溃会留下 pending 记录，可由补偿任务重试。
func (s *ChatService) applyConsequence(
	persistCtx context.Context,
	sessionID, conversationID, pitfallMessageID int64,
	cfg *model.PitfallConfig,
) {
	if s.consequenceStore == nil {
		return // 后果传导未装配（含旧测试路径），安静关闭
	}
	spec := cfg.PitfallRules.ResolveConsequence(cfg.Severity)
	if spec == nil {
		return // worsen_fault=false，该坑点不做后果传导
	}
	if s.consequenceExecutor == nil {
		// 云上故障引擎运行时通道未就绪：登记但不注入，避免"监控可见恶化"承诺落空时静默假装成功。
		log.Printf("次生故障执行通道未就绪，跳过后果传导: session=%d config=%d kind=%s",
			sessionID, cfg.ID, spec.Kind)
		return
	}

	rec, applied, err := s.consequenceStore.RegisterPending(persistCtx, ConsequenceRecord{
		SessionID:        sessionID,
		ConversationID:   conversationID,
		PitfallMessageID: pitfallMessageID,
		ConfigID:         cfg.ID,
		Kind:             spec.Kind,
		CPUPercent:       spec.CPUPercent,
		MemPercent:       spec.MemPercent,
		DiskPercent:      spec.DiskPercent,
		DelayMillis:      spec.DelayMillis,
		LossPercent:      spec.LossPercent,
		DurationSec:      spec.DurationSec,
		Status:           ConsequenceStatusPending,
	})
	if err != nil {
		log.Printf("次生故障登记失败，跳过后果传导: session=%d config=%d err=%v", sessionID, cfg.ID, err)
		return
	}
	if !applied {
		// 该坑点已恶化过（任何状态）：同一坑点只传导一次。
		log.Printf("次生故障已登记，跳过重复恶化: session=%d msg=%d status=%s",
			sessionID, pitfallMessageID, rec.Status)
		return
	}

	injectCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	uid, err := s.consequenceExecutor.Inject(injectCtx, ConsequenceInjectRequest{
		SessionID:   sessionID,
		Kind:        spec.Kind,
		CPUPercent:  spec.CPUPercent,
		MemPercent:  spec.MemPercent,
		DiskPercent: spec.DiskPercent,
		DelayMillis: spec.DelayMillis,
		LossPercent: spec.LossPercent,
		DurationSec: spec.DurationSec,
	})
	if err != nil {
		log.Printf("次生故障注入失败，置 failed 不阻断训练: session=%d config=%d kind=%s err=%v",
			sessionID, cfg.ID, spec.Kind, err)
		_ = s.consequenceStore.MarkFailed(persistCtx, rec.ID, err.Error())
		return
	}
	if err := s.consequenceStore.MarkInjected(persistCtx, rec.ID, uid); err != nil {
		// 注入已实际发生但句柄落库失败：记录日志，尽量直接撤销，避免留下无法追踪的实验。
		log.Printf("次生故障句柄落库失败，尝试即时撤销: session=%d record=%d uid=%s err=%v",
			sessionID, rec.ID, uid, err)
		if revErr := s.consequenceExecutor.Revoke(injectCtx, sessionID, uid); revErr != nil {
			log.Printf("次生故障兜底撤销失败，需人工/补偿清理: session=%d uid=%s err=%v",
				sessionID, uid, revErr)
		}
		return
	}
	log.Printf("次生故障已注入（采纳错误建议，监控应见恶化）: session=%d conv=%d config=%d kind=%s uid=%s",
		sessionID, conversationID, cfg.ID, spec.Kind, uid)
}

// RevokeSessionConsequences 撤销某训练会话全部仍生效的次生故障（T3-12 恢复路径）。
//
// 供会话结束（Complete/Abort，云上接线）调用：按登记记录逐条幂等撤销，
// 任一记录失败不影响其余记录；全部尝试后返回遇到的首个错误供上层记录。
// duration_sec>0 已被故障引擎自动恢复的实验，Revoke 侧应幂等成功。
func (s *ChatService) RevokeSessionConsequences(ctx context.Context, sessionID int64) error {
	if s.consequenceStore == nil || s.consequenceExecutor == nil {
		return nil
	}
	records, err := s.consequenceStore.ListActiveBySession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("列出生效次生故障失败 session=%d: %w", sessionID, err)
	}
	var firstErr error
	for _, rec := range records {
		if err := s.consequenceExecutor.Revoke(ctx, sessionID, rec.ExperimentUID); err != nil {
			log.Printf("次生故障撤销失败: session=%d record=%d uid=%s err=%v",
				sessionID, rec.ID, rec.ExperimentUID, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := s.consequenceStore.MarkRevoked(ctx, rec.ID); err != nil {
			log.Printf("次生故障撤销状态落库失败: session=%d record=%d err=%v", sessionID, rec.ID, err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (s *ChatService) EnableBrainContext(contextProvider interface {
	BuildStudentBrainContext(context.Context, int64) (*StudentBrainContext, error)
}, hints interface {
	ReserveBrainHint(context.Context, int64, int) (int, bool, error)
}) {
	s.brainContext = contextProvider
	s.brainHints = hints
}
