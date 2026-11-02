// blade_executor.go ChaosBlade 执行通道抽象与 blade JSON 输出解析。
//
// 设计：把"注入规格 -> blade 命令参数"做成纯函数（blade_command.go），不接触真实环境，
// 可在无集群机器上完整单测；真正执行交给 BladeExecutor 接口（云上接 blade，测试用 fake）。

package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/quenching/fault-engine/internal/model"
)

// BladeExecResult 执行通道返回结果。
type BladeExecResult struct {
	// ExperimentUID ChaosBlade 返回的实验 uid（恢复必用）。
	ExperimentUID string
	// Raw 原始输出，审计/排障用。
	Raw string
}

// BladeExecutor 命令执行通道抽象。
//   - 生产：对目标 Pod 用 kubectl exec / agent HTTP 执行 blade（BladeCloudExec 实现，云上补）。
//   - 测试：fakeExecutor 记录命令并返回可控 uid，不依赖集群。
type BladeExecutor interface {
	// Run 在 spec 定位的目标环境中执行 blade args。
	Run(ctx context.Context, spec model.InjectSpec, args []string) (*BladeExecResult, error)
}

// bladeResponse ChaosBlade CLI 的 JSON 输出。
type bladeResponse struct {
	Code    int    `json:"code"`
	Success bool   `json:"success"`
	Result  string `json:"result"` // 成功时为实验 uid
	Error   string `json:"error,omitempty"`
}

// ParseBladeUID 从 blade create 的 JSON 输出中解析实验 uid。
// blade 成功输出形如：{"code":200,"success":true,"result":"<uid>"}
func ParseBladeUID(raw string) (string, error) {
	var resp bladeResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return "", fmt.Errorf("解析 blade 输出失败: %w, raw=%s", err, raw)
	}
	if !resp.Success || resp.Code != 200 {
		return "", fmt.Errorf("blade 执行失败: code=%d error=%s", resp.Code, resp.Error)
	}
	if resp.Result == "" {
		return "", fmt.Errorf("blade 未返回实验 uid")
	}
	return resp.Result, nil
}
