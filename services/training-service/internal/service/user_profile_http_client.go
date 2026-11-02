// user-service 用户协作档案 HTTP 客户端（架构拆库第三批）。
//
// 把原先 training-service 直接读写 user_profiles 的访问，替换为直连 user-service
// 的内部接口（/internal/v1）：
//
//	GET  /internal/v1/profiles/:userId
//	POST /internal/v1/profiles/collab-group/assign-if-absent
//	POST /internal/v1/profiles/collab/rollup
//
// 两侧不产生编译期依赖，JSON 字段名即契约。滚动平均算法由 user-service 在档案
// 属主侧完成，本客户端只提交本次训练的协作评估结果。
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/quenching/training-service/internal/model"
)

// userProfileHTTPTimeout 单次内部调用超时上限。
const userProfileHTTPTimeout = 15 * time.Second

// UserProfileClientConfig 用户协作档案客户端配置。
type UserProfileClientConfig struct {
	// BaseURL user-service 根地址，如 http://user-service:8085（不带结尾斜杠）。
	BaseURL string
	// Token 内部共享密钥，随 X-Internal-Token 头发送；留空则服务端不校验。
	Token string
}

// UserCollabProfile 用户协作档案视图。
type UserCollabProfile struct {
	UserID   int64
	Group    string
	Level    string
	AvgScore *float64
	Radar    *model.RadarData
}

// UserProfileReader 用户协作档案只读端口。
type UserProfileReader interface {
	// GetCollabProfile 读取用户协作档案；不存在时返回 (nil, nil)。
	GetCollabProfile(ctx context.Context, userID int64) (*UserCollabProfile, error)
}

// UserCollabRollupWriter 用户协作汇总提交端口。
type UserCollabRollupWriter interface {
	// SubmitCollabRollup 提交本次协作雷达、分数和等级。
	SubmitCollabRollup(
		ctx context.Context, userID int64,
		radar model.RadarData, score float64, level string,
	) error
}

// UserProfileStore 组合用户协作档案的读写端口。
type UserProfileStore interface {
	UserProfileReader
	UserCollabRollupWriter
}

// UserProfileHTTPClient 基于 HTTP 的用户协作档案客户端。
type UserProfileHTTPClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewUserProfileHTTPClient 创建用户协作档案客户端。BaseURL 去尾斜杠。
func NewUserProfileHTTPClient(cfg UserProfileClientConfig) *UserProfileHTTPClient {
	return &UserProfileHTTPClient{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		token:   cfg.Token,
		http:    &http.Client{Timeout: userProfileHTTPTimeout},
	}
}

// 编译期断言：HTTP 客户端满足交叉分组端口和用户档案读写端口。
var (
	_ CollabGroupStore = (*UserProfileHTTPClient)(nil)
	_ UserProfileStore = (*UserProfileHTTPClient)(nil)
)

// profileDTO 用户协作档案 JSON 响应。
type profileDTO struct {
	UserID         int64           `json:"user_id"`
	CollabGroup    string          `json:"collab_group"`
	CollabLevel    string          `json:"collab_level"`
	AvgCollabScore *float64        `json:"avg_collab_score"`
	CollabRadar    json.RawMessage `json:"collab_radar"`
}

// assignGroupReq 条件分配分组请求。
type assignGroupReq struct {
	UserID int64  `json:"user_id"`
	Group  string `json:"group"`
}

// assignGroupDTO 条件分配分组响应。
type assignGroupDTO struct {
	UserID  int64  `json:"user_id"`
	Group   string `json:"group"`
	Applied bool   `json:"applied"`
}

// rollupReq 协作滚动汇总请求。
type rollupReq struct {
	UserID int64           `json:"user_id"`
	Radar  model.RadarData `json:"radar"`
	Score  float64         `json:"score"`
	Level  string          `json:"level"`
}

// GetCollabGroup 取用户交叉分组；尚未分配时返回空串。
func (c *UserProfileHTTPClient) GetCollabGroup(
	ctx context.Context, userID int64,
) (model.ControlGroup, error) {
	profile, err := c.GetCollabProfile(ctx, userID)
	if err != nil {
		return "", err
	}
	if profile == nil {
		return "", nil
	}
	return model.ControlGroup(profile.Group), nil
}

// AssignCollabGroupIfAbsent 条件写入用户交叉分组。
func (c *UserProfileHTTPClient) AssignCollabGroupIfAbsent(
	ctx context.Context, userID int64, g model.ControlGroup,
) (model.ControlGroup, bool, error) {
	var out assignGroupDTO
	if err := c.call(ctx, http.MethodPost,
		"/internal/v1/profiles/collab-group/assign-if-absent",
		assignGroupReq{UserID: userID, Group: string(g)}, &out); err != nil {
		return "", false, err
	}
	return model.ControlGroup(out.Group), out.Applied, nil
}

// GetCollabProfile 读取用户协作档案；远端返回不存在时转换为 (nil, nil)。
func (c *UserProfileHTTPClient) GetCollabProfile(
	ctx context.Context, userID int64,
) (*UserCollabProfile, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("用户ID非法")
	}
	var dto profileDTO
	err := c.call(ctx, http.MethodGet,
		"/internal/v1/profiles/"+strconv.FormatInt(userID, 10), nil, &dto)
	if err != nil {
		if isHTTPStatusError(err, http.StatusNotFound) {
			return nil, nil
		}
		return nil, err
	}

	profile := &UserCollabProfile{
		UserID:   dto.UserID,
		Group:    dto.CollabGroup,
		Level:    dto.CollabLevel,
		AvgScore: dto.AvgCollabScore,
	}
	if len(dto.CollabRadar) > 0 && string(dto.CollabRadar) != "null" {
		var radar model.RadarData
		if json.Unmarshal(dto.CollabRadar, &radar) == nil {
			profile.Radar = &radar
		}
	}
	return profile, nil
}

// SubmitCollabRollup 提交本次训练的协作评估结果。
func (c *UserProfileHTTPClient) SubmitCollabRollup(
	ctx context.Context, userID int64,
	radar model.RadarData, score float64, level string,
) error {
	if userID <= 0 {
		return fmt.Errorf("用户ID非法")
	}
	return c.call(ctx, http.MethodPost, "/internal/v1/profiles/collab/rollup", rollupReq{
		UserID: userID,
		Radar:  radar,
		Score:  score,
		Level:  level,
	}, nil)
}

// call 发起 JSON 请求并按统一响应范式校验，data 反序列化到 out。
func (c *UserProfileHTTPClient) call(
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
		return fmt.Errorf("调用用户服务失败: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取用户服务响应失败: %w", err)
	}

	var envelope apiEnvelope
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return newHTTPStatusError(resp.StatusCode,
				fmt.Errorf("解析用户服务响应失败: %w", err))
		}
	}
	if resp.StatusCode != http.StatusOK || envelope.Code != 0 {
		msg := envelope.Msg
		if msg == "" {
			msg = "用户服务返回非成功状态"
		}
		return newHTTPStatusError(resp.StatusCode, fmt.Errorf("%s(http=%d code=%d)",
			msg, resp.StatusCode, envelope.Code))
	}
	if out != nil && len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("解析用户服务数据失败: %w", err)
		}
	}
	return nil
}

// httpStatusError 保留远端 HTTP 状态，供调用方识别 404 等情况。
type httpStatusError struct {
	status int
	err    error
}

func (e *httpStatusError) Error() string {
	return e.err.Error()
}

func (e *httpStatusError) Unwrap() error {
	return e.err
}

func newHTTPStatusError(status int, err error) error {
	return &httpStatusError{status: status, err: err}
}

func isHTTPStatusError(err error, status int) bool {
	var target *httpStatusError
	return errors.As(err, &target) && target.status == status
}
