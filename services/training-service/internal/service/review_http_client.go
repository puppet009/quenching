// review-service 复盘协作数据 HTTP 客户端（架构拆库第四批）。
//
// 把原先 training-service 直接读写 reviews 的访问，替换为直连 review-service
// 的内部接口（/internal/v1）：
//
//	GET  /internal/v1/reviews/by-session/:sessionId
//	POST /internal/v1/reviews/collab-scores
//	POST /internal/v1/reviews/existence
//	POST /internal/v1/reviews/generate
//
// 两侧不产生编译期依赖，JSON 字段名即契约。本客户端读取/提交协作子分、
// 触发复盘生成，不接触复盘正文、分享链接等字段。
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// reviewHTTPTimeout 单次内部调用超时上限。
const reviewHTTPTimeout = 15 * time.Second

// ReviewClientConfig 复盘协作数据客户端配置。
type ReviewClientConfig struct {
	// BaseURL review-service 根地址，如 http://review-service:8087（不带结尾斜杠）。
	BaseURL string
	// Token 内部共享密钥，随 X-Internal-Token 头发送；留空则服务端不校验。
	Token string
}

// ReviewCollabView 复盘协作子分视图。指针字段对应可空子分列。
type ReviewCollabView struct {
	ID                     int64
	SessionID              int64
	GenerationStatus       string
	CollabScore            *float64
	QuestionQuality        *float64
	InfoUtilization        *float64
	Judgment               *float64
	ErrorRecognitionRate   *float64
	EvidenceValidationRate *float64
	PitfallCount           *int
	PitfallRecognized      *int
}

// ReviewExistenceItem 复盘存在性批量结果项。
type ReviewExistenceItem struct {
	SessionID int64  `json:"session_id"`
	ReviewID  *int64 `json:"review_id"`
}

// ReviewReader 复盘协作数据只读端口。
type ReviewReader interface {
	// GetReviewBySession 按会话 ID 读取协作子分；复盘不存在时返回 (nil, nil)。
	GetReviewBySession(ctx context.Context, sessionID int64) (*ReviewCollabView, error)
	// ListReviewExistence 批量查询复盘存在性，返回项与请求顺序无关。
	ListReviewExistence(ctx context.Context, sessionIDs []int64) ([]ReviewExistenceItem, error)
}

// ReviewCollabWriter 协作子分提交端口。
type ReviewCollabWriter interface {
	// SubmitCollabScores 提交本次协作评估明细；复盘行尚不存在时 updated=false。
	SubmitCollabScores(ctx context.Context, sessionID int64, d ReviewCollabDetail) (updated bool, err error)
}

// ReviewTriggerWriter 复盘生成触发端口。
//
// 用户态的 POST /api/v1/reviews/generate 依赖浏览器带的登录态，而弃班（长时间空闲
// 被判结束）时没有浏览器在场，只能由训练服务带着会话归属人身份走内部端点触发。
type ReviewTriggerWriter interface {
	// TriggerGeneration 请求为会话生成复盘；已存在复盘记录时远端幂等返回。
	TriggerGeneration(ctx context.Context, sessionID, userID, projectID int64) error
}

// ReviewStore 组合复盘协作数据的读写端口。
type ReviewStore interface {
	ReviewReader
	ReviewCollabWriter
}

// ReviewHTTPClient 基于 HTTP 的复盘协作数据客户端。
type ReviewHTTPClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewReviewHTTPClient 创建复盘协作数据客户端。BaseURL 去尾斜杠。
func NewReviewHTTPClient(cfg ReviewClientConfig) *ReviewHTTPClient {
	return &ReviewHTTPClient{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		token:   cfg.Token,
		http:    &http.Client{Timeout: reviewHTTPTimeout},
	}
}

// 编译期断言：HTTP 客户端满足复盘协作数据端口与复盘生成触发端口。
var (
	_ ReviewStore         = (*ReviewHTTPClient)(nil)
	_ ReviewTriggerWriter = (*ReviewHTTPClient)(nil)
)

// reviewDTO 复盘协作子分 JSON 响应。
type reviewDTO struct {
	ID                     int64    `json:"id"`
	SessionID              int64    `json:"session_id"`
	GenerationStatus       string   `json:"generation_status"`
	CollabScore            *float64 `json:"collab_score"`
	QuestionQualityScore   *float64 `json:"question_quality_score"`
	InfoUtilizationScore   *float64 `json:"info_utilization_score"`
	JudgmentScore          *float64 `json:"judgment_score"`
	ErrorRecognitionRate   *float64 `json:"error_recognition_rate"`
	EvidenceValidationRate *float64 `json:"evidence_validation_rate"`
	AIPitfallCount         *int     `json:"ai_pitfall_count"`
	AIPitfallRecognized    *int     `json:"ai_pitfall_recognized"`
}

// collabScoresReq 提交协作子分请求。
type collabScoresReq struct {
	SessionID              int64   `json:"session_id"`
	CollabScore            float64 `json:"collab_score"`
	QuestionQualityScore   float64 `json:"question_quality_score"`
	InfoUtilizationScore   float64 `json:"info_utilization_score"`
	JudgmentScore          float64 `json:"judgment_score"`
	ErrorRecognitionRate   float64 `json:"error_recognition_rate"`
	EvidenceValidationRate float64 `json:"evidence_validation_rate"`
	AIPitfallCount         int     `json:"ai_pitfall_count"`
	AIPitfallRecognized    int     `json:"ai_pitfall_recognized"`
}

// collabScoresDTO 提交协作子分响应。
type collabScoresDTO struct {
	Updated bool `json:"updated"`
}

// existenceReq 批量复盘存在性请求。
type existenceReq struct {
	SessionIDs []int64 `json:"session_ids"`
}

// generateReviewReq 触发复盘生成请求；user_id 为会话归属人（内部端点无登录态可取）。
type generateReviewReq struct {
	SessionID int64 `json:"session_id"`
	UserID    int64 `json:"user_id"`
	ProjectID int64 `json:"project_id"`
}

// existenceDTO 批量复盘存在性响应。
type existenceDTO struct {
	Items []ReviewExistenceItem `json:"items"`
}

// GetReviewBySession 按会话 ID 读取协作子分；远端 404 转为 (nil, nil)。
func (c *ReviewHTTPClient) GetReviewBySession(
	ctx context.Context, sessionID int64,
) (*ReviewCollabView, error) {
	if sessionID <= 0 {
		return nil, fmt.Errorf("会话ID非法")
	}
	var dto reviewDTO
	err := c.call(ctx, http.MethodGet,
		"/internal/v1/reviews/by-session/"+strconv.FormatInt(sessionID, 10), nil, &dto)
	if err != nil {
		if isHTTPStatusError(err, http.StatusNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &ReviewCollabView{
		ID:                     dto.ID,
		SessionID:              dto.SessionID,
		GenerationStatus:       dto.GenerationStatus,
		CollabScore:            dto.CollabScore,
		QuestionQuality:        dto.QuestionQualityScore,
		InfoUtilization:        dto.InfoUtilizationScore,
		Judgment:               dto.JudgmentScore,
		ErrorRecognitionRate:   dto.ErrorRecognitionRate,
		EvidenceValidationRate: dto.EvidenceValidationRate,
		PitfallCount:           dto.AIPitfallCount,
		PitfallRecognized:      dto.AIPitfallRecognized,
	}, nil
}

// SubmitCollabScores 提交本次协作评估明细。
func (c *ReviewHTTPClient) SubmitCollabScores(
	ctx context.Context, sessionID int64, d ReviewCollabDetail,
) (bool, error) {
	if sessionID <= 0 {
		return false, fmt.Errorf("会话ID非法")
	}
	var dto collabScoresDTO
	err := c.call(ctx, http.MethodPost, "/internal/v1/reviews/collab-scores", collabScoresReq{
		SessionID:              sessionID,
		CollabScore:            d.CollabOverall,
		QuestionQualityScore:   d.QuestionQuality,
		InfoUtilizationScore:   d.InfoUtilization,
		JudgmentScore:          d.Judgment,
		ErrorRecognitionRate:   d.ErrorRecognitionRate,
		EvidenceValidationRate: d.EvidenceValidationRate,
		AIPitfallCount:         d.PitfallTotal,
		AIPitfallRecognized:    d.PitfallRecognized,
	}, &dto)
	if err != nil {
		return false, err
	}
	return dto.Updated, nil
}

// ListReviewExistence 批量查询复盘存在性；自动去重并过滤非法 ID。
func (c *ReviewHTTPClient) ListReviewExistence(
	ctx context.Context, sessionIDs []int64,
) ([]ReviewExistenceItem, error) {
	var dto existenceDTO
	err := c.call(ctx, http.MethodPost, "/internal/v1/reviews/existence",
		existenceReq{SessionIDs: sessionIDs}, &dto)
	if err != nil {
		return nil, err
	}
	return dto.Items, nil
}

// TriggerGeneration 请求为会话生成复盘（弃班等无浏览器在场的终态用）。
// 远端按会话幂等：已有复盘记录时直接返回现有记录，不会重复投递生成任务。
func (c *ReviewHTTPClient) TriggerGeneration(
	ctx context.Context, sessionID, userID, projectID int64,
) error {
	if sessionID <= 0 || userID <= 0 {
		return fmt.Errorf("会话ID/用户ID非法")
	}
	return c.call(ctx, http.MethodPost, "/internal/v1/reviews/generate",
		generateReviewReq{SessionID: sessionID, UserID: userID, ProjectID: projectID}, nil)
}

// call 发起 JSON 请求并按统一响应范式校验，data 反序列化到 out。
func (c *ReviewHTTPClient) call(
	ctx context.Context, method, path string, payload any, out any,
) error {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("序列化请求失败: %w", err)
		}
		body = bytes.NewReader(raw)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("构造请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		httpReq.Header.Set("X-Internal-Token", c.token)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("调用复盘服务失败: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取复盘服务响应失败: %w", err)
	}

	var envelope apiEnvelope
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return newHTTPStatusError(resp.StatusCode,
				fmt.Errorf("解析复盘服务响应失败: %w", err))
		}
	}
	if resp.StatusCode != http.StatusOK || envelope.Code != 0 {
		msg := envelope.Msg
		if msg == "" {
			msg = "复盘服务返回非成功状态"
		}
		return newHTTPStatusError(resp.StatusCode, fmt.Errorf("%s(http=%d code=%d)",
			msg, resp.StatusCode, envelope.Code))
	}
	if out != nil && len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("解析复盘服务数据失败: %w", err)
		}
	}
	return nil
}
