// 本文件封装 training-service 到 Python ai-service 的 gRPC 调用（原 internal/ai 包，g6 批次3 并入 service）。
//
// 设计要点：
// - 业务层只面对本文件的强类型方法，不直接引用 grpc/proto，便于未来加缓存、熔断或改 HTTP。
// - 契约唯一来源是 packages/proto，禁止在本包重新定义消息结构。
// - MVP 本地/内网明文通信；上云后替换为 TLS credentials。
package service

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/quenching/proto/aiv1"
)

// AIClientConfig 是 ai-service gRPC 连接配置。
type AIClientConfig struct {
	InternalToken string
	Address       string        // 例：127.0.0.1:9000
	DialTimeout   time.Duration // 保留：NewAIClient 为懒连接，超时主要作用在首次调用
	CallTimeout   time.Duration // 普通调用（Chat）默认超时
}

func (c AIClientConfig) withDefaults() AIClientConfig {
	if c.DialTimeout == 0 {
		c.DialTimeout = 5 * time.Second
	}
	if c.CallTimeout == 0 {
		c.CallTimeout = 30 * time.Second
	}
	return c
}

// AIClient 是 ai-service 的 gRPC 客户端。
type AIClient struct {
	cfg  AIClientConfig
	conn *grpc.ClientConn
	raw  aiv1.AIServiceClient
}

// NewAIClient 创建 ai-service gRPC 客户端。grpc.NewClient 为懒连接，
// 地址非法时立即报错，真正连通性在首次 RPC 时体现。
func NewAIClient(cfg AIClientConfig) (*AIClient, error) {
	cfg = cfg.withDefaults()
	if cfg.Address == "" {
		return nil, fmt.Errorf("ai client: empty address")
	}
	conn, err := grpc.NewClient(
		cfg.Address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("create ai-service client %s: %w", cfg.Address, err)
	}
	return &AIClient{cfg: cfg, conn: conn, raw: aiv1.NewAIServiceClient(conn)}, nil
}

// Chat 调用 AI 助教对话。
func (c *AIClient) Chat(ctx context.Context, sessionID, conversationID int64, message, agentLevel string) (*aiv1.ChatResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.CallTimeout)
	defer cancel()
	resp, err := c.raw.Chat(ctx, &aiv1.ChatRequest{
		SessionId:      sessionID,
		ConversationId: conversationID,
		Message:        message,
		AgentLevel:     agentLevel,
	})
	if err != nil {
		return nil, fmt.Errorf("ai.Chat: %w", err)
	}
	return resp, nil
}

// GenerateReview 生成方法级复盘，返回六段式章节 JSON。
// 复盘可能多轮调用 LLM，给予普通调用两倍的超时。
func (c *AIClient) GenerateReview(ctx context.Context, sessionID int64, faultType, timelineJSON, pitfallJSON string) (*aiv1.GenerateReviewResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.CallTimeout*2)
	defer cancel()
	resp, err := c.raw.GenerateReview(ctx, &aiv1.GenerateReviewRequest{
		SessionId:             sessionID,
		FaultType:             faultType,
		OperationTimelineJson: timelineJSON,
		PitfallRecordsJson:    pitfallJSON,
	})
	if err != nil {
		return nil, fmt.Errorf("ai.GenerateReview: %w", err)
	}
	return resp, nil
}

// EvaluateCollab 调用 ai-service 的确定性协作规则评估（不调 LLM）。
// 入参为已组装好的会话协作事件（提问/坑点结局/采纳操作），纯内存计算，普通超时即可。
func (c *AIClient) EvaluateCollab(ctx context.Context, req *aiv1.EvaluateCollabRequest) (*aiv1.EvaluateCollabResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.CallTimeout)
	defer cancel()
	resp, err := c.raw.EvaluateCollab(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("ai.EvaluateCollab: %w", err)
	}
	return resp, nil
}

// ChatStream 发起服务端流式对话，返回 chunk 流。调用方遍历 Recv 直到 io.EOF。
// 不设置额外超时：流式可能持续较久，生命周期由调用方 ctx 控制（含用户取消）。
func (c *AIClient) ChatStream(ctx context.Context, req *aiv1.ChatRequest) (aiv1.AIService_ChatStreamClient, error) {
	stream, err := c.raw.ChatStream(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("ai.ChatStream: %w", err)
	}
	return stream, nil
}

// GenerateComparisonSummary 发起人机对照总结服务端流式调用，返回 chunk 流。
// 不设额外超时：生命周期由调用方 ctx 控制（与 ChatStream 一致，含用户取消）。
func (c *AIClient) GenerateComparisonSummary(
	ctx context.Context, req *aiv1.ComparisonSummaryRequest,
) (aiv1.AIService_GenerateComparisonSummaryClient, error) {
	stream, err := c.raw.GenerateComparisonSummary(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("ai.GenerateComparisonSummary: %w", err)
	}
	return stream, nil
}

// Close 释放底层连接。
func (c *AIClient) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}
