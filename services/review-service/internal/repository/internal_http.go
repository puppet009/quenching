// 服务间内部 HTTP 调用公共设施（架构拆库第五批）。
//
// review-service 不再直查 training-service 的任何表，复盘输入与坑点数据统一
// 直连 training-service 的 /internal/v1 端点获取（东西向直连，不经网关）。
// 约定：
//   - 统一响应 {"code":0,"msg":"ok","data":...}，code!=0 视为业务错误；
//   - X-Internal-Token 头做共享密钥校验，token 留空时对端不校验（本地开发）；
//   - 单 http.Client、15s 超时，BaseURL 去尾斜杠。
package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// internalHTTPTimeout 单次服务间调用超时上限。
const internalHTTPTimeout = 15 * time.Second

// internalEnvelope 统一响应外壳；Data 延迟二次反序列化。
type internalEnvelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

type internalHTTPError struct {
	Status  int
	Message string
}

func (e *internalHTTPError) Error() string { return e.Message }

// internalClient 服务间 HTTP 调用基础客户端。
type internalClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// newInternalClient 创建内部调用客户端，BaseURL 去尾斜杠。
func newInternalClient(baseURL, token string) *internalClient {
	return &internalClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: internalHTTPTimeout},
	}
}

// do 发起请求并按统一响应范式校验，data 反序列化到 out（out 为 nil 时只校验）。
// method 仅按需使用 GET/POST。
func (c *internalClient) do(
	ctx context.Context, method, path string, body any, out any,
) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("序列化请求失败: %w", err)
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("构造请求失败: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("X-Internal-Token", c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("调用实训服务失败: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取实训服务响应失败: %w", err)
	}

	var envelope internalEnvelope
	if len(rawBody) > 0 {
		if err := json.Unmarshal(rawBody, &envelope); err != nil {
			return fmt.Errorf("解析实训服务响应失败(http=%d): %w", resp.StatusCode, err)
		}
	}
	if resp.StatusCode != http.StatusOK || envelope.Code != 0 {
		return &internalHTTPError{Status: resp.StatusCode, Message: fmt.Sprintf("实训服务返回错误(http=%d code=%d): %s", resp.StatusCode, envelope.Code, envelope.Msg)}
	}
	if out != nil && len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("解析实训服务数据失败: %w", err)
		}
	}
	return nil
}
