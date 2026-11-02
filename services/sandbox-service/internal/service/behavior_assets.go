// behavior_assets.go 行为模拟运行时资产的内嵌与配置模型（行为模拟第二批）。
//
// runner.js 是 Playwright 运行脚本，随 sandbox-service 二进制一起编译
// （go:embed），部署时写入沙箱 namespace 内的 ConfigMap，再由 runner Pod
// 只读挂载，保证"代码与资产同版本发布"，不依赖节点本地文件。
package service

import (
	_ "embed"
	"encoding/json"

	"github.com/quenching/go-common/templates"
)

// runnerScript 内嵌的 Playwright runner 脚本（见 behavior_assets/runner.js）。
//
//go:embed behavior_assets/runner.js
var runnerScript string

//go:embed behavior_assets/command_runner.js
var commandRunnerScript string

//go:embed behavior_assets/protocol_runner.js
var protocolRunnerScript string

// BehaviorRunnerScriptFile runner 脚本在 ConfigMap / 挂载目录内的文件名。
const BehaviorRunnerScriptFile = "runner.js"

// BehaviorFlowConfig 写入 flow.json 的运行配置。
// 由 content-service 模板的 behavior_flow 声明转换而来，只包含 runner
// 执行所需字段（与 runner.js 的 loadFlow 解析结构一一对应）。
type BehaviorFlowConfig struct {
	Protocol   json.RawMessage          `json:"protocol,omitempty"`
	Name       string                   `json:"name"`
	EntryURL   string                   `json:"entry_url"`
	IntervalMS int                      `json:"interval_ms"`
	TimeoutMS  int                      `json:"timeout_ms"`
	Steps      []templates.BehaviorStep `json:"steps"`
}

// BehaviorSignal runner 上报的浏览器侧行为信号。
// JSON 契约与 runner.js 的 report() 输出严格一致；training-service
// 第三批消费时复用同名字段，避免二次转换。
type BehaviorSignal struct {
	// SandboxID 沙箱/会话ID（当前 SESSION_ID 即沙箱会话ID）。
	SandboxID int64 `json:"sandbox_id"`
	// SessionID 实训会话ID；与 SandboxID 同值，保留字段供后续拆分。
	SessionID int64 `json:"session_id"`
	// Type 信号类型：step_failure | page_error | console_error | http_error | request_failed。
	Type string `json:"type"`
	// Severity 严重级别，当前统一 error。
	Severity string `json:"severity"`
	// Timestamp 事件发生时间（RFC3339）。
	Timestamp string `json:"timestamp"`
	// Step 触发信号时正在执行的步骤名。
	Step string `json:"step,omitempty"`
	// Action 步骤动作（仅 step_failure 携带）。
	Action string `json:"action,omitempty"`
	// Message 人可读的错误摘要。
	Message string `json:"message,omitempty"`
	// URL 相关页面/请求地址。
	URL string `json:"url,omitempty"`
	// Status HTTP 状态码（仅 http_error 携带）。
	Status int `json:"status,omitempty"`
}

// 行为信号类型集合（与 runner.js 保持同步）。
const (
	SignalStepFailure   = "step_failure"
	SignalPageError     = "page_error"
	SignalConsoleError  = "console_error"
	SignalHTTPError     = "http_error"
	SignalRequestFailed = "request_failed"
)
