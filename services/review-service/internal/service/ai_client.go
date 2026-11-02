// ai_client.go 封装 review-service 到 Python ai-service 的 gRPC 调用。
//
// 与 training-service/internal/ai 同款封装：业务层只面对强类型方法，
// 不直接引用 grpc/proto；契约唯一来源是 packages/proto。
// 随 internal/ai 野包归位并入 service 层（g4）。
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
	DialTimeout   time.Duration // grpc.NewClient 为懒连接，主要作用在首次调用
	CallTimeout   time.Duration // 复盘生成默认超时
}

func (c AIClientConfig) withDefaults() AIClientConfig {
	if c.DialTimeout == 0 {
		c.DialTimeout = 5 * time.Second
	}
	if c.CallTimeout == 0 {
		// 复盘只产生 1 次 LLM 调用（六章在一个 JSON 内返回），模型单次调用
		// 超时配置 300s（ai-service llm_timeout，T4-16 起接 qwen flash，
		// 不限 max_tokens），gRPC 兜底取 310s 让模型侧超时先返回明确错误；
		// 超时不重试（已等满，重试只会翻倍），worker 侧仅对快速失败重试。
		c.CallTimeout = 310 * time.Second
	}
	return c
}

// AIClient 是 ai-service 的 gRPC 客户端，实现 ReviewAIClient。
type AIClient struct {
	cfg  AIClientConfig
	conn *grpc.ClientConn
	raw  aiv1.AIServiceClient
}

// 编译期保证满足 ReviewAIClient。
var _ ReviewAIClient = (*AIClient)(nil)

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

// GenerateReview 调用 ai-service 生成复盘章节，返回 sections_json 原文。
func (c *AIClient) GenerateReview(ctx context.Context, sessionID int64, p Prompt) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.CallTimeout)
	defer cancel()

	resp, err := c.raw.GenerateReview(ctx, &aiv1.GenerateReviewRequest{
		SessionId:             sessionID,
		FaultType:             p.FaultType,
		OperationTimelineJson: p.TimelineJSON,
		PitfallRecordsJson:    p.PitfallJSON,
	})
	if err != nil {
		return "", fmt.Errorf("ai.GenerateReview: %w", err)
	}
	return resp.GetSectionsJson(), nil
}

// Close 释放底层连接。
func (c *AIClient) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}
