// 坑点数据采集的 HTTP 适配器（架构拆库第五批）。
//
// 替代旧的 PitfallRepo PG 4 表 JOIN：AI 坑点原始数据由 training-service 经
// 单端点输出，本服务不再跨库读 ai_messages/ai_conversations/ai_pitfall_configs/
// training_sessions。
//
//	GET /internal/v1/sessions/:id/pitfalls
//
// training 只负责取数；用户是否识别坑点、识别率统计属于复盘领域，仍在本服务
// 完成（model.PitfallItem.IsRecognized）。无坑点返回空切片（非 nil）。
package repository

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/quenching/review-service/internal/model"
)

// pitfallRawItem training pitfalls 端点返回的坑点原始数据。
// JSON 标签与 training-service service.ReviewPitfallRawItem 一一对应。
type pitfallRawItem struct {
	MessageID       int64  `json:"message_id"`
	PitfallConfigID int64  `json:"pitfall_config_id"`
	PitfallType     string `json:"pitfall_type"`
	UserResponse    string `json:"user_response"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	ErrorContent    string `json:"error_content"`
	CorrectApproach string `json:"correct_approach"`
	Severity        string `json:"severity"`
	AppearOffsetSec int    `json:"appear_offset_sec"`
}

// HTTPPitfallRepo 基于服务间 HTTP 的坑点采集仓储。
type HTTPPitfallRepo struct {
	client *internalClient
}

// NewHTTPPitfallRepo 创建坑点 HTTP 仓储。
func NewHTTPPitfallRepo(baseURL, token string) *HTTPPitfallRepo {
	return &HTTPPitfallRepo{client: newInternalClient(baseURL, token)}
}

// CollectSessionPitfalls 采集一次会话中的全部坑点并重算识别统计。
func (r *HTTPPitfallRepo) CollectSessionPitfalls(
	ctx context.Context, sessionID int64,
) (*model.PitfallAnalysis, error) {
	if r == nil || r.client == nil || r.client.baseURL == "" {
		return nil, errors.New("pitfall repo: training_service.base_url is not configured")
	}
	if sessionID <= 0 {
		return nil, fmt.Errorf("会话ID非法")
	}

	raw := []pitfallRawItem{}
	path := fmt.Sprintf("/internal/v1/sessions/%d/pitfalls", sessionID)
	if err := r.client.do(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return nil, err
	}

	items := make([]model.PitfallItem, 0, len(raw))
	recognizedCount := 0
	for i := range raw {
		item := model.PitfallItem{
			MessageID:       raw[i].MessageID,
			PitfallConfigID: raw[i].PitfallConfigID,
			PitfallType:     raw[i].PitfallType,
			UserResponse:    raw[i].UserResponse,
			Name:            raw[i].Name,
			Description:     raw[i].Description,
			ErrorContent:    raw[i].ErrorContent,
			CorrectApproach: raw[i].CorrectApproach,
			Severity:        raw[i].Severity,
			AppearOffsetSec: raw[i].AppearOffsetSec,
		}
		item.Recognized = item.IsRecognized()
		if item.Recognized {
			recognizedCount++
		}
		items = append(items, item)
	}

	total := len(items)
	recognitionRate := 0.0
	if total > 0 {
		recognitionRate = float64(recognizedCount) / float64(total) * 100
		// 保留一位小数
		recognitionRate = float64(int(recognitionRate*10)) / 10
	}

	return &model.PitfallAnalysis{
		TotalCount:      total,
		RecognizedCount: recognizedCount,
		RecognitionRate: recognitionRate,
		Pitfalls:        items,
	}, nil
}
