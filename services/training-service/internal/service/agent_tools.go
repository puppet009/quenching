// agent_tools.go Agent 工具白名单定义与展示辅助（Function Calling）。
//
// M1 只注册观察类（只读）工具 get_logs。
// M2 追加本进程内的只读工具：get_metrics（核心指标快照）、query_metric
// （单指标趋势）、list_alerts（活跃告警）。所有工具均无需用户确认。
// M3 追加跨服务只读工具：list_services/list_files/read_file/run_diagnostic。
// M4 追加首个修改类工具 write_file：RequiresApproval=true，执行前必须由用户确认；
// 被拒绝时不执行写操作，由助手改为给出手动操作引导。
package service

import (
	"encoding/json"
	"strings"
)

// 各工具参数的 JSON Schema。
const getLogsParameters = `{
  "type": "object",
  "properties": {
    "service": {"type": "string", "description": "目标服务名（可选，默认全部）"},
    "tail": {"type": "integer", "description": "读取最近多少条，如 50"},
    "keyword": {"type": "string", "description": "关键词过滤（可选）"}
  }
}`

const getMetricsParameters = `{
  "type": "object",
  "properties": {
    "service": {"type": "string", "description": "目标服务名（可选，默认全部聚合）"}
  }
}`

const queryMetricParameters = `{
  "type": "object",
  "properties": {
    "metric": {"type": "string", "enum": ["cpu_usage","memory_usage","qps","error_rate","p99_latency"], "description": "指标名"},
    "service": {"type": "string", "description": "目标服务名（可选）"},
    "minutes": {"type": "integer", "description": "回看最近多少分钟，默认 15"}
  },
  "required": ["metric"]
}`

const listAlertsParameters = `{
  "type": "object",
  "properties": {
    "status": {"type": "string", "enum": ["firing","pending"], "description": "按状态过滤（可选，默认全部活跃告警）"}
  }
}`

// M3：服务拓扑 / 文件 / 只读诊断命令的参数 schema。
const listServicesParameters = `{
  "type": "object",
  "properties": {}
}`

const listFilesParameters = `{
  "type": "object",
  "properties": {
    "pod": {"type": "string", "description": "目标服务器（Pod）名；留空时需配合 path 仅作只读浏览"},
    "container": {"type": "string", "description": "容器名（可选）"},
    "path": {"type": "string", "description": "目录绝对路径，默认根目录 /"}
  },
  "required": ["pod"]
}`

const readFileParameters = `{
  "type": "object",
  "properties": {
    "pod": {"type": "string", "description": "目标服务器（Pod）名"},
    "container": {"type": "string", "description": "容器名（可选）"},
    "path": {"type": "string", "description": "文件绝对路径"}
  },
  "required": ["pod", "path"]
}`

const runDiagnosticParameters = `{
  "type": "object",
  "properties": {
    "pod": {"type": "string", "description": "目标服务器（Pod）名"},
    "container": {"type": "string", "description": "容器名（可选）"},
    "command": {"type": "string", "enum": ["ps","ss","netstat","df","top","uptime","whoami","hostname","free"], "description": "只读诊断命令"},
    "args": {"type": "string", "description": "命令参数，如 aux / -tlnp（可选）"}
  },
  "required": ["pod", "command"]
}`

// M4：写文件参数 schema（修改类，执行前需用户确认）。
const writeFileParameters = `{
  "type": "object",
  "properties": {
    "pod": {"type": "string", "description": "目标服务器（Pod）名"},
    "container": {"type": "string", "description": "容器名（可选）"},
    "path": {"type": "string", "description": "目标文件绝对路径；已存在会被覆盖"},
    "content": {"type": "string", "description": "要写入的完整文件内容"}
  },
  "required": ["pod", "path", "content"]
}`

// AgentToolDefs 返回本次对话声明的工具白名单。
// enabled=false 时返回 nil（纯对话，ai-service 不启用工具）。
func AgentToolDefs(enabled bool) []ToolDef {
	if !enabled {
		return nil
	}
	return []ToolDef{
		{
			Name: "get_logs",
			Description: "读取实训环境中服务的真实运行日志，用于在回答前掌握实际报错与现象；" +
				"可按服务、条数或关键词过滤。",
			ParametersJSON:   getLogsParameters,
			RequiresApproval: false,
		},
		{
			Name: "get_metrics",
			Description: "一次性获取沙箱核心指标当前快照：CPU 使用率、内存使用率、QPS、错误率、P99 延迟；" +
				"可指定服务。用于快速掌握整体运行状态。",
			ParametersJSON:   getMetricsParameters,
			RequiresApproval: false,
		},
		{
			Name:             "query_metric",
			Description:      "查询单个核心指标最近一段时间的变化趋势（按时间点返回），用于判断指标是否异常及异常从何时开始。",
			ParametersJSON:   queryMetricParameters,
			RequiresApproval: false,
		},
		{
			Name:             "list_alerts",
			Description:      "列出沙箱当前的活跃告警（如阈值告警），含级别、所属服务与告警信息；可按 firing/pending 过滤。",
			ParametersJSON:   listAlertsParameters,
			RequiresApproval: false,
		},
		// M3：服务拓扑 / 文件 / 只读诊断。
		{
			Name:             "list_services",
			Description:      "列出实训环境中全部业务服务器（Pod）及其状态（是否就绪），用于了解系统组成。",
			ParametersJSON:   listServicesParameters,
			RequiresApproval: false,
		},
		{
			Name:             "list_files",
			Description:      "列出指定服务器上某目录的文件与子目录（只读），用于查看配置/日志文件是否存在。",
			ParametersJSON:   listFilesParameters,
			RequiresApproval: false,
		},
		{
			Name:             "read_file",
			Description:      "读取指定服务器上某个文件的内容（只读），用于查看配置或日志正文。",
			ParametersJSON:   readFileParameters,
			RequiresApproval: false,
		},
		{
			Name:             "run_diagnostic",
			Description:      "在指定服务器内执行只读诊断命令（ps/ss/netstat/df/top/uptime/whoami/hostname/free）查看进程、端口、连接或资源占用；不做任何修改。",
			ParametersJSON:   runDiagnosticParameters,
			RequiresApproval: false,
		},
		// M4：修改类工具，执行前必须经用户确认。
		{
			Name: "write_file",
			Description: "在指定服务器上写入文件（修改类操作，会覆盖同名文件）。" +
				"仅在需要落盘修复配置/脚本时使用；调用后必须等待用户确认，用户拒绝则不要重试，改为给出手动操作步骤。",
			ParametersJSON:   writeFileParameters,
			RequiresApproval: true,
		},
	}
}

// ToolDisplay 返回一次工具调用的人类可读动作文案（步骤时间线展示）。
func ToolDisplay(call ToolCall) string {
	args := parseCallArgs(call.ArgumentsJSON)
	switch call.Name {
	case "get_logs":
		parts := []string{"读取日志"}
		if v := strings.TrimSpace(args["service"]); v != "" {
			parts = append(parts, "服务:"+v)
		}
		return strings.Join(parts, " ")
	case "get_metrics":
		return "查询核心指标快照"
	case "query_metric":
		if v := strings.TrimSpace(args["metric"]); v != "" {
			return "查询指标趋势：" + v
		}
		return "查询指标趋势"
	case "list_alerts":
		return "读取活跃告警"
	case "list_services":
		return "列出服务器"
	case "list_files":
		if v := strings.TrimSpace(args["path"]); v != "" {
			return "列出目录：" + v
		}
		return "列出目录"
	case "read_file":
		if v := strings.TrimSpace(args["path"]); v != "" {
			return "读取文件：" + v
		}
		return "读取文件"
	case "run_diagnostic":
		if v := strings.TrimSpace(args["command"]); v != "" {
			return "执行诊断命令：" + v
		}
		return "执行诊断命令"
	case "write_file":
		if v := strings.TrimSpace(args["path"]); v != "" {
			return "写入文件：" + v
		}
		return "写入文件"
	default:
		return "执行工具：" + call.Name
	}
}

// ToolResultSummary 生成工具结果的简短摘要（折叠步骤展示）。
func ToolResultSummary(call ToolCall, contentJSON string) string {
	var n int
	switch call.Name {
	case "get_logs":
		var parsed struct {
			Items []json.RawMessage `json:"items"`
			Lines []json.RawMessage `json:"lines"`
		}
		if err := json.Unmarshal([]byte(contentJSON), &parsed); err == nil {
			n = len(parsed.Items) + len(parsed.Lines)
		}
		if n > 0 {
			return "获取 " + itoaSimple(n) + " 条日志"
		}
		return "日志已获取"
	case "get_metrics":
		return "5 项指标"
	case "query_metric":
		var parsed struct {
			Points []json.RawMessage `json:"points"`
		}
		if err := json.Unmarshal([]byte(contentJSON), &parsed); err == nil {
			n = len(parsed.Points)
		}
		return itoaSimple(n) + " 个采样点"
	case "list_alerts":
		var parsed struct {
			Alerts []json.RawMessage `json:"alerts"`
		}
		if err := json.Unmarshal([]byte(contentJSON), &parsed); err == nil {
			n = len(parsed.Alerts)
		}
		return itoaSimple(n) + " 条告警"
	case "list_services":
		var parsed []json.RawMessage
		if err := json.Unmarshal([]byte(contentJSON), &parsed); err == nil {
			n = len(parsed)
		}
		return itoaSimple(n) + " 台服务器"
	case "list_files":
		var parsed struct {
			Entries []json.RawMessage `json:"entries"`
		}
		if err := json.Unmarshal([]byte(contentJSON), &parsed); err == nil {
			n = len(parsed.Entries)
		}
		return itoaSimple(n) + " 个条目"
	case "read_file":
		return "文件已读取"
	case "run_diagnostic":
		return "诊断完成"
	case "write_file":
		return "写入完成"
	default:
		return "已完成"
	}
}

// parseCallArgs 解析工具调用参数 JSON，失败返回空 map。
func parseCallArgs(raw string) map[string]string {
	out := map[string]string{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return out
	}
	var generic map[string]any
	if err := json.Unmarshal([]byte(raw), &generic); err != nil {
		return out
	}
	for k, v := range generic {
		switch t := v.(type) {
		case string:
			out[k] = t
		case float64:
			out[k] = itoaSimple(int(t))
		}
	}
	return out
}

// itoaSimple 简单的整数转字符串（避免与其它文件的辅助函数冲突）。
func itoaSimple(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
