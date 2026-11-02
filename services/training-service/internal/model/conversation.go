// Package model 训练会话与 AI 对话领域模型。
package model

import "time"

// 对话类型（对应 ai_conversations.conversation_type）。
const (
	ConvTrainingAssistant   = "training_assistant"
	ConvReviewExplainer     = "review_explainer"
	ConvKnowledgeQA         = "knowledge_qa"
	ConvArchitectureAdvisor = "architecture_advisor"
)

// 对话状态。
const (
	ConvStatusActive = "active"
	ConvStatusClosed = "closed"
	ConvStatusError  = "error"
)

// 消息角色。
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleSystem    = "system"
)

// Conversation 一次 AI 对话（ai_conversations）。
type Conversation struct {
	ID               int64     `json:"id"`
	SessionID        *int64    `json:"session_id,omitempty"`
	UserID           int64     `json:"user_id"`
	ConversationType string    `json:"conversation_type"`
	AIModel          string    `json:"ai_model,omitempty"`
	SystemPrompt     string    `json:"-"` // 快照，用于追溯，不下发前端
	PromptVersion    string    `json:"prompt_version,omitempty"`
	Status           string    `json:"status"`
	MessageCount     int       `json:"message_count"`
	TotalTokens      int       `json:"total_tokens"`
	TotalCost        float64   `json:"total_cost"`
	ContextData      []byte    `json:"-"` // JSONB 原始字节
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// Message 一条对话消息（ai_messages）。
type Message struct {
	ID             int64     `json:"id"`
	ConversationID int64     `json:"conversation_id"`
	Role           string    `json:"role"`
	Content        string    `json:"content"`
	ContentType    string    `json:"content_type"`
	IsPitfall      bool      `json:"is_pitfall"`
	PitfallType    string    `json:"pitfall_type,omitempty"`
	Tokens         *int      `json:"tokens,omitempty"`
	Cost           *float64  `json:"cost,omitempty"`
	LatencyMS      *int      `json:"latency_ms,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// HistoryTurn 传给 ai-service 的脱敏历史条目（仅 role/content，绝不含根因）。
type HistoryTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
