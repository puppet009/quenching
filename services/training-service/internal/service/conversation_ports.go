// Package service 中 AI 对话编排所需的端口定义。
// 数据库与 ai-service 都以接口注入，编排逻辑可在无 PG、无 Python 的环境下单测。
package service

import (
	"context"
	"time"

	"github.com/quenching/training-service/internal/model"
)

// ConversationStore 对话与消息的持久化端口（PG 实现在 internal/repository）。
type ConversationStore interface {
	// CreateConversation 创建对话，回填 ID。
	CreateConversation(ctx context.Context, conv *model.Conversation) (int64, error)
	// GetConversation 按归属用户取对话；不存在或不属于该用户返回 (nil,nil)。
	GetConversation(ctx context.Context, id, userID int64) (*model.Conversation, error)
	// ListHistory 取最近若干条消息，仅返回 role/content（重建给 AI 的脱敏上下文）。
	ListHistory(ctx context.Context, conversationID int64, limit int) ([]model.HistoryTurn, error)
	// AppendUserMessage 落一条用户消息，并在同事务内把 message_count+1。
	AppendUserMessage(ctx context.Context, conversationID int64, content string) (int64, error)
	// AppendAssistantMessage 落一条普通助手消息，并在同事务内更新计数/token/费用。
	AppendAssistantMessage(
		ctx context.Context,
		conversationID int64,
		content string,
		tokens, latencyMS int,
		cost float64,
		isPitfall bool,
		pitfallType string,
	) (int64, error)
	// AppendPitfallMessage 落一条坑点助手消息（T3-9）。确定性模板直出，不经 LLM，
	// 因此 tokens/cost/latency 均为 0；需写全 pitfall_config_id/pitfall_type/confidence_level，
	// 并在同事务内把会话 pitfall_total+1。
	AppendPitfallMessage(
		ctx context.Context,
		conversationID int64,
		content string,
		configID int64,
		pitfallType, confidenceLevel string,
	) (int64, error)
	// ListPendingPitfalls 取本对话尚未判定用户反应（user_response_type 为空）
	// 的坑点助手消息，按注入先后（id 升序）返回（T3-11）。
	ListPendingPitfalls(ctx context.Context, conversationID int64) ([]PendingPitfall, error)
	// MarkPitfallResponse 把判定结果回写到坑点助手消息，并在同事务内幂等地维护
	// training_sessions 的 pitfall_recognized/pitfall_fallen 计数（T3-11）。
	// 该消息若已有判定（被并发抢先处理）则不回写、不计数，返回 applied=false；
	// 由本次调用首次写入时 applied=true。responseType 取 model.Response* 常量。
	MarkPitfallResponse(
		ctx context.Context, conversationID, pitfallMessageID int64, responseType string,
	) (applied bool, err error)
	// AppendHintMessage 落一条提示型助手消息（T3-5）：写 is_hint=true/hint_level，
	// 并在同事务内 message_count+1、累计 token/费用。配额按会话从这些消息实时统计，
	// 服务重启也不丢次数。hintLevel 取值 1/2/3。
	AppendHintMessage(
		ctx context.Context,
		conversationID int64,
		content string,
		hintLevel int,
		tokens, latencyMS int,
		cost float64,
	) (int64, error)
}

// PendingPitfall 一条等待用户反应判定的坑点助手消息（T3-11）。
type PendingPitfall struct {
	MessageID int64
	ConfigID  int64
	// InjectedAt 坑点消息落库时刻，用于只取该时刻之后的操作做判定。
	InjectedAt time.Time
}

// ChatStreamInput 调 ai-service 流式对话的入参（不含根因等敏感上下文）。
type ChatStreamInput struct {
	MemoryContextJSON string
	SessionID         int64
	ConversationID    int64
	Message           string
	AgentLevel        string
	ConversationType  string
	History           []model.HistoryTurn
	// MVP 训练对话恒 false；T3 坑点引擎命中时由编排置位。
	PitfallInstruction bool
	PitfallType        string
	// FaultContextJSON 脱敏后的故障上下文 JSON（T3-4）：只含用户可见信息，
	// 由 FaultContextProvider 重建，编排层不得在此放入 root_cause。
	FaultContextJSON string
	// HintLevel 渐进式提示级别（T3-5）：0=普通应答，1/2/3=方向引导/缩小范围/接近答案。
	HintLevel int
	// HintRemaining 本轮之后剩余可用提示次数；-1=不限/未知。
	HintRemaining int
	// Agent 工具调用（Function Calling）。
	// Tools 本次对话允许 LLM 调用的工具白名单（空=纯对话）。
	Tools []ToolDef
	// ToolResults 上一轮工具调用结果（Agent Loop 回传），首轮空。
	ToolResults []ToolResult
}

// ToolDef 工具定义（白名单）。字段与 proto ToolDef 对齐。
type ToolDef struct {
	Name             string
	Description      string
	ParametersJSON   string
	RequiresApproval bool
}

// ToolCall LLM 请求的一次工具调用。
type ToolCall struct {
	OperationID   string
	Round         int
	ID            string
	Name          string
	ArgumentsJSON string
	// ConversationID 所属会话内对话（M5 修改登记用）；观察类工具不关心。
	ConversationID int64
	// ApprovalID 修改类工具对应的用户审批句柄（M5）；未走审批时为空。
	ApprovalID string
}

// ToolResult 一次工具执行结果。
type ToolResult struct {
	OperationID        string
	OutcomeUnconfirmed bool
	CallID             string
	ContentJSON        string
	IsError            bool
}

// StepEvent 步骤时间线事件（过程透明）。
type StepEvent struct {
	OperationID string `json:"operation_id"`
	StepID      string `json:"step_id"`
	State       string `json:"state"` // started/progress/completed/failed/awaiting_approval/rejected
	Tool        string `json:"tool"`
	Display     string `json:"display"`
	Summary     string `json:"summary"`
	DetailJSON  string `json:"detail_json"`
	// ApprovalID 仅 awaiting_approval 状态携带：前端确认弹窗据此回调 HTTP 审批端点。
	ApprovalID string `json:"approval_id"`
}

// StreamEventType 流式事件类型。
type StreamEventType string

const (
	StreamThinking StreamEventType = "thinking"  // 思考态占位
	StreamDelta    StreamEventType = "delta"     // 正文增量
	StreamDone     StreamEventType = "done"      // 正常结束（携带落库后的计费/消息ID）
	StreamError    StreamEventType = "error"     // 出错
	StreamToolCall StreamEventType = "tool_call" // L 请求工具调用（编排层据此执行并继续循环）
	StreamStep     StreamEventType = "step"      // 步骤时间线事件（转发前端显示过程）
)

// StreamEvent 编排层流式事件，handler 直接映射为 SSE 帧。
type StreamEvent struct {
	Type             StreamEventType `json:"type"`
	Delta            string          `json:"delta,omitempty"`
	IsPitfall        bool            `json:"is_pitfall,omitempty"`
	PitfallConfigID  int64           `json:"pitfall_config_id,omitempty"`
	PitfallType      string          `json:"pitfall_type,omitempty"`
	ConfidenceLevel  string          `json:"confidence_level,omitempty"`
	ConversationID   int64           `json:"conversation_id,omitempty"`
	MessageID        int64           `json:"message_id,omitempty"`
	PromptTokens     int             `json:"prompt_tokens,omitempty"`
	CompletionTokens int             `json:"completion_tokens,omitempty"`
	Cost             float64         `json:"cost,omitempty"`
	HintLevel        int             `json:"hint_level,omitempty"`
	HintRemaining    int             `json:"hint_remaining,omitempty"`
	ErrorMsg         string          `json:"error,omitempty"`
	// Agent 工具调用 / 步骤时间线。
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Step      *StepEvent `json:"step,omitempty"`
}

// FaultContextItem 单个脱敏故障（T3-4）。
// 严禁携带 root_cause/fix_config/trigger_config 等答案或注入细节字段——
// 这些字段一旦进入给 LLM 的上下文，就可能被助教直接复述给学员。
type FaultContextItem struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Difficulty  string `json:"difficulty"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

// FaultContext 一次训练会话的脱敏故障上下文（T3-4）。
type FaultContext struct {
	SessionID int64 `json:"session_id"`
	// Difficulty 会话难度（故障剧本难度缺失时的兜底展示用）。
	Difficulty string             `json:"difficulty,omitempty"`
	Faults     []FaultContextItem `json:"faults"`
}

// FaultContextProvider 训练对话的脱敏故障上下文供给端口（T3-4）。
// 实现侧跨库只读 session_defects JOIN defects，但 SELECT 列中禁止出现
// root_cause 等答案列；返回 (nil,nil) 表示该会话没有可用上下文（非训练对话/
// 故障尚未暴露），编排层据此退化为无上下文引导，不阻断对话。
type FaultContextProvider interface {
	LoadFaultContext(ctx context.Context, sessionID int64) (*FaultContext, error)
}

// HintUsage 某次训练会话的渐进式提示用量（T3-5）。
// Used 为已消耗提示次数（跨该会话的全部对话统计），MaxLevel 为已用过的最高提示级别
// （首次请求前为 0）；二者共同支撑"次数限制 + 逐级解锁"。
type HintUsage struct {
	Used     int
	MaxLevel int
}

// HintQuotaProvider 渐进式提示的配额与用量供给端口（T3-5）。
// 实现侧按会话难度给出配额（internal/difficulty：简单 8 次/3 级、普通 5 次/2 级、
// 困难 3 次/1 级），并从 ai_messages.is_hint 实时重建用量；编排层不直接碰库。
type HintQuotaProvider interface {
	// Difficulty 返回会话难度的归一化取值（easy/normal/hard）。
	// DB 历史值 "medium" 须归一为 "normal"；读不到时实现侧回退默认（normal）。
	Difficulty(ctx context.Context, sessionID int64) (string, error)
	// Usage 返回该会话已消耗的提示次数与已用最高级别。
	Usage(ctx context.Context, sessionID int64) (HintUsage, error)
}

// PitfallRuntime 坑点触发判定所需的会话运行时供给端口（T3-9）。
// 由会话编排侧实现（云上读 session_defects/会话阶段/ai_messages），纯逻辑测试用内存 fake。
// 对话编排层只依赖该接口，不直接碰会话状态表，保持与对话存储解耦。
type PitfallRuntime interface {
	// ActiveConfigs 返回某训练会话当前挂载的全部坑点配置（已按 sort_order,id 排序）。
	// 非训练对话（无 session）返回空切片且不报错。
	ActiveConfigs(ctx context.Context, sessionID int64) ([]*model.PitfallConfig, error)
	// Snapshot 取判定时刻的运行时上下文：当前轮次、排障阶段、已过秒数、故障是否已触发。
	Snapshot(ctx context.Context, sessionID, conversationID int64, userQuestion string) (model.PitfallRuntimeContext, error)
	// InjectedCount 某坑点在本会话内已注入次数（从 ai_messages 的 is_pitfall 记录统计）。
	InjectedCount(ctx context.Context, sessionID, configID int64) (int, error)
	// OperationsSince 取某会话在 since 时刻（严格）之后按时间正序发生的操作（T3-11）。
	// 操作采集（session_operations 写入）属 T1-23；在其落库前本端口只读返回空切片，
	// 反应判定退化为"仅按回复文本"，不阻断对话。
	OperationsSince(ctx context.Context, sessionID int64, since time.Time) ([]model.PitfallOperation, error)
}

// ChatStreamer ai-service 流式端口（本包 AIChatStreamer 实现，gRPC 通信）。
type ChatStreamer interface {
	// StreamChat 产出 AI 事件通道；通道在流结束后关闭。done/error 为最后一个事件。
	StreamChat(ctx context.Context, in ChatStreamInput) (<-chan StreamEvent, error)
}

// ComparisonSummaryStreamer 人机对照总结流式端口（本包 AIComparisonSummaryStreamer 实现）。
// 入参为 GET /training/comparison 响应序列化后的 JSON，编排层不解释内容；
// ai-service 逐块产出四段式点评，done/error 为最后一个事件。
type ComparisonSummaryStreamer interface {
	// StreamComparisonSummary 产出对照总结事件通道；通道在流结束后关闭。
	StreamComparisonSummary(ctx context.Context, comparisonJSON string) (<-chan StreamEvent, error)
}

// ConsequenceExecutor 次生故障执行通道（T3-12）。
//
// 云上实现把 Inject/Revoke 翻译为 fault-engine 运行时 API（底层 ChaosBlade）；
// 本地无集群、单测中用内存 fake。training 与 fault-engine 不发生编译期依赖
// （fault-engine 代码均在 internal/ 下），两侧以本接口的字段名作为 JSON 契约。
//
// 实现必须是"尽力而为"语义：编排层在判定踩坑后调用，任何错误只记日志、
// 绝不阻断 LLM 对话；恢复（Revoke）在会话结束时按登记记录幂等执行。
type ConsequenceExecutor interface {
	// Inject 对某训练会话的沙箱目标注入一次次生故障，返回故障引擎侧实验句柄。
	// ExperimentUID 是日后撤销（blade destroy）的必需凭据，必须随登记落库。
	Inject(ctx context.Context, req ConsequenceInjectRequest) (experimentUID string, err error)
	// Revoke 按实验句柄撤销一次已注入的次生故障；对已撤销/不存在的实验应幂等成功。
	Revoke(ctx context.Context, sessionID int64, experimentUID string) error
}

// ConsequenceInjectRequest 一次次生故障注入请求（字段与 model.ConsequenceFault 同名同义，
// 另带定位会话沙箱所需的会话标识与目标）。
type ConsequenceInjectRequest struct {
	SessionID int64
	Kind      string
	// Namespace/Pod 目标定位（T3-12）：编排层可按会话沙箱显式指定；
	// 留空时由执行通道兜底（HTTP 客户端用构造时的全局默认命名空间，
	// Pod 留空表示命名空间内匹配/不限制目标）。
	Namespace   string
	Pod         string
	CPUPercent  int
	MemPercent  int
	DiskPercent int
	DelayMillis int
	LossPercent int
	// DurationSec 0=平台掌控（会话结束撤销）；>0 交由故障引擎自动恢复。
	// 注意：临时注入 HTTP 契约不透传该字段（fault-engine 临时链路统一平台掌控恢复）。
	DurationSec int
}

// ConsequenceRecord 一条次生故障登记记录（T3-12，对应表 ai_consequence_injections）。
type ConsequenceRecord struct {
	ID               int64
	SessionID        int64
	ConversationID   int64
	PitfallMessageID int64 // 触发恶化的坑点助手消息（幂等键）
	ConfigID         int64
	Kind             string
	CPUPercent       int
	MemPercent       int
	DiskPercent      int
	DelayMillis      int
	LossPercent      int
	DurationSec      int
	Status           string // 取 ConsequenceStatus* 常量
	ExperimentUID    string
	FailReason       string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// 次生故障登记状态机。
const (
	// ConsequenceStatusPending 已登记、尚未向故障引擎注入成功（进程崩溃可据此重试/补偿）。
	ConsequenceStatusPending = "pending"
	// ConsequenceStatusInjected 已注入并拿到 experiment_uid，等待会话结束撤销。
	ConsequenceStatusInjected = "injected"
	// ConsequenceStatusRevoked 已成功撤销（终态）。
	ConsequenceStatusRevoked = "revoked"
	// ConsequenceStatusFailed 注入失败且放弃（终态；仅记日志，不影响训练）。
	ConsequenceStatusFailed = "failed"
)

// ConsequenceStore 次生故障登记端口（T3-12，PG 实现在 internal/repository）。
//
// 解决三件事：①"同一坑点只恶化一次"——以 pitfall_message_id 为幂等键；
// ②进程重启后仍可恢复——登记记录持久化，状态机记录注入/撤销进度；
// ③会话结束统一撤销——按会话列出仍 injected 的记录。
type ConsequenceStore interface {
	// RegisterPending 幂等登记一条待注入记录。
	// 若该 pitfallMessageID 已登记（任何状态），不新增、applied=false 并返回既有记录；
	// 首次登记 applied=true，状态为 pending。
	RegisterPending(ctx context.Context, rec ConsequenceRecord) (saved *ConsequenceRecord, applied bool, err error)
	// MarkInjected 把 pending 记录置为 injected 并记录 experiment_uid。
	MarkInjected(ctx context.Context, id int64, experimentUID string) error
	// MarkFailed 把 pending 记录置为 failed 并记录原因（注入失败终态）。
	MarkFailed(ctx context.Context, id int64, reason string) error
	// MarkRevoked 把 injected 记录置为 revoked（撤销终态）。
	MarkRevoked(ctx context.Context, id int64) error
	// ListActiveBySession 列出某会话仍处于 injected、会话结束时需要撤销的记录。
	ListActiveBySession(ctx context.Context, sessionID int64) ([]ConsequenceRecord, error)
}

// ToolExecutor Agent 工具执行端口（云上由各只读接口/fault 通道实现，单测用 fake）。
//
// M1 只实现观察类（只读）工具 get_logs，可在不依赖用户确认的情况下由
// 编排层自动调用；修改类工具（requires_approval）在后续里程碑接入，
// 执行前必须经用户确认。执行结果以 JSON 字符串返回，失败由调用方包装成
// ToolResult.IsError，任何执行错误都不阻断对话主链路。
type ToolExecutor interface {
	// Execute 在会话沙箱内执行一次工具。call 提供工具名与参数。
	// 返回结果内容 JSON（交给 LLM）；返回 error 时编排层按失败工具处理。
	Execute(ctx context.Context, sessionID int64, call ToolCall) (contentJSON string, err error)
}

// ChangeRecord 一次助手修改操作的登记内容（M5）。
type ChangeRecord struct {
	SessionID      int64
	ConversationID int64  // 0 表示未关联会话内对话
	ApprovalID     string // 空表示未经审批（理论上修改类必有）
	Pod            string
	Container      string
	Path           string
	BeforeExisted  bool
	BeforeContent  string
	AfterContent   string
}

// ChangeRecorder 修改操作登记端口（M5）。
// 写文件成功后调用；实现应做到"登记失败不影响已成功的写操作"，
// 由调用方记录日志后吞掉错误。
type ChangeRecorder interface {
	Record(ctx context.Context, rec ChangeRecord) error
}
