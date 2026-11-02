// tool_executor_adapter.go Agent 工具的执行适配（M1-M4）。
//
// 把本进程内已装配的数据源接到组合工具执行器：
//   - get_logs      → logquery.Service（Loki）
//   - get_metrics   → metrics.Service（核心指标快照）
//   - query_metric  → metrics.Service（单指标趋势）
//   - list_alerts   → service.AlertManager（活跃告警）
//   - write_file（M4）→ service.SandboxReadonly（经审批的写操作）
//
// 观察类工具只读、无需确认；write_file 为修改类，编排层已在调用本处理器前
// 取得用户审批，此处不再重复挂起。返回的 JSON 已做精简，便于 LLM 阅读。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/quenching/training-service/internal/pkg/logquery"
	"github.com/quenching/training-service/internal/pkg/metrics"
	"github.com/quenching/training-service/internal/service"
)

// RegisterAgentTools 构造组合执行器并注册全部观察工具。
// sandbox 为 nil 时不注册服务/文件/诊断类工具（仅注册日志/指标/告警）。
// recorder 用于登记 write_file 修改（M5）；为 nil 时写操作仍执行但不留痕。
func RegisterAgentTools(
	logs *logquery.Service,
	met *metrics.Service,
	alerts *service.AlertManager,
	sandbox service.SandboxReadonly,
	recorder service.ChangeRecorder,
) *service.CompositeToolExecutor {
	exec := service.NewCompositeToolExecutor()
	registerLogsTool(exec, logs)
	registerMetricsTools(exec, met)
	registerAlertsTool(exec, alerts)
	registerSandboxTools(exec, sandbox, recorder)
	return exec
}

// ---------- get_logs ----------

type getLogsArgs struct {
	Service string `json:"service"`
	Tail    int    `json:"tail"`
	Keyword string `json:"keyword"`
}

func registerLogsTool(exec *service.CompositeToolExecutor, logs *logquery.Service) {
	exec.Register("get_logs", func(ctx context.Context, sessionID int64, call service.ToolCall) (string, error) {
		args := getLogsArgs{Tail: 50}
		if raw := strings.TrimSpace(call.ArgumentsJSON); raw != "" {
			if err := json.Unmarshal([]byte(raw), &args); err != nil {
				return "", fmt.Errorf("解析 get_logs 参数失败: %w", err)
			}
		}
		if args.Tail <= 0 {
			args.Tail = 50
		}
		if args.Tail > 200 {
			args.Tail = 200
		}

		// 沙箱日志按 sandbox_id（命名空间名 sandbox-<sessionID>）隔离。
		sandboxID := fmt.Sprintf("sandbox-%d", sessionID)
		res, err := logs.GetLatestLogs(ctx, sandboxID, args.Service, args.Tail)
		if err != nil {
			return "", fmt.Errorf("查询日志失败: %w", err)
		}

		items := make([]map[string]string, 0, len(res.Entries))
		for _, en := range res.Entries {
			if args.Keyword != "" && !strings.Contains(en.Line, args.Keyword) {
				continue
			}
			items = append(items, map[string]string{
				"level":   en.Labels["level"],
				"service": en.Labels["service"],
				"message": en.Line,
			})
		}
		return marshalToolResult(map[string]any{
			"sandbox_id": sandboxID,
			"count":      len(items),
			"items":      items,
		})
	})
}

// ---------- get_metrics / query_metric ----------

type getMetricsArgs struct {
	Service string `json:"service"`
}

type queryMetricArgs struct {
	Metric  string `json:"metric"`
	Service string `json:"service"`
	Minutes int    `json:"minutes"`
}

func registerMetricsTools(exec *service.CompositeToolExecutor, met *metrics.Service) {
	// get_metrics：5 大核心指标当前快照。
	exec.Register("get_metrics", func(ctx context.Context, _ int64, call service.ToolCall) (string, error) {
		args := getMetricsArgs{}
		if raw := strings.TrimSpace(call.ArgumentsJSON); raw != "" {
			if err := json.Unmarshal([]byte(raw), &args); err != nil {
				return "", fmt.Errorf("解析 get_metrics 参数失败: %w", err)
			}
		}
		snap, err := met.GetAllMetrics(ctx, args.Service)
		if err != nil {
			return "", fmt.Errorf("查询指标快照失败: %w", err)
		}
		return marshalToolResult(snapshotView(snap))
	})

	// query_metric：单指标趋势。
	exec.Register("query_metric", func(ctx context.Context, _ int64, call service.ToolCall) (string, error) {
		args := queryMetricArgs{Minutes: 15}
		if raw := strings.TrimSpace(call.ArgumentsJSON); raw != "" {
			if err := json.Unmarshal([]byte(raw), &args); err != nil {
				return "", fmt.Errorf("解析 query_metric 参数失败: %w", err)
			}
		}
		if args.Metric == "" {
			return "", fmt.Errorf("query_metric 缺少 metric 参数")
		}
		if args.Minutes <= 0 {
			args.Minutes = 15
		}
		if args.Minutes > 120 {
			args.Minutes = 120
		}
		end := time.Now()
		res, err := met.QueryMetric(ctx, metrics.QueryRequest{
			Metric:  metrics.MetricName(args.Metric),
			Service: args.Service,
			Start:   end.Add(-time.Duration(args.Minutes) * time.Minute),
			End:     end,
		})
		if err != nil {
			return "", fmt.Errorf("查询指标趋势失败: %w", err)
		}
		return marshalToolResult(map[string]any{
			"metric": res.Metric,
			"unit":   res.Unit,
			"points": res.Points,
		})
	})
}

// snapshotView 把指标快照压成稳定结构。
func snapshotView(snap *metrics.AllMetricsSnapshot) map[string]any {
	return map[string]any{
		"timestamp": snap.Timestamp,
		"metrics": map[string]any{
			"cpu_usage":    snap.CPUUsage,
			"memory_usage": snap.MemoryUsage,
			"qps":          snap.QPS,
			"error_rate":   snap.ErrorRate,
			"p99_latency":  snap.P99Latency,
		},
	}
}

// ---------- list_alerts ----------

type listAlertsArgs struct {
	Status string `json:"status"`
}

func registerAlertsTool(exec *service.CompositeToolExecutor, alerts *service.AlertManager) {
	exec.Register("list_alerts", func(ctx context.Context, _ int64, call service.ToolCall) (string, error) {
		args := listAlertsArgs{}
		if raw := strings.TrimSpace(call.ArgumentsJSON); raw != "" {
			if err := json.Unmarshal([]byte(raw), &args); err != nil {
				return "", fmt.Errorf("解析 list_alerts 参数失败: %w", err)
			}
		}
		// 空 status 取全部活跃（pending + firing）。
		var list []*service.AlertInstance
		switch args.Status {
		case "firing":
			list = alerts.ListAlerts(service.AlertStatusFiring)
		case "pending":
			list = alerts.ListAlerts(service.AlertStatusPending)
		default:
			firing := alerts.ListAlerts(service.AlertStatusFiring)
			pending := alerts.ListAlerts(service.AlertStatusPending)
			list = append(pending, firing...)
		}
		return marshalToolResult(map[string]any{
			"count":  len(list),
			"alerts": list,
		})
	})
}

// ---------- 通用辅助 ----------

func marshalToolResult(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("序列化工具结果失败: %w", err)
	}
	return string(raw), nil
}

// ---------- M3：服务拓扑 / 文件 / 只读诊断 ----------

// sandboxListServicesArgs list_services 无参数。
type sandboxListFilesArgs struct {
	Pod       string `json:"pod"`
	Container string `json:"container"`
	Path      string `json:"path"`
}

type sandboxReadFileArgs struct {
	Pod       string `json:"pod"`
	Container string `json:"container"`
	Path      string `json:"path"`
}

type sandboxDiagnosticArgs struct {
	Pod       string `json:"pod"`
	Container string `json:"container"`
	Command   string `json:"command"`
	Args      string `json:"args"`
}

// sandboxWriteFileArgs write_file 参数（M4，修改类）。
type sandboxWriteFileArgs struct {
	Pod       string `json:"pod"`
	Container string `json:"container"`
	Path      string `json:"path"`
	Content   string `json:"content"`
}

func registerSandboxTools(
	exec *service.CompositeToolExecutor, sb service.SandboxReadonly, recorder service.ChangeRecorder,
) {
	if sb == nil {
		return
	}

	// list_services：服务器拓扑。
	exec.Register("list_services", func(ctx context.Context, sessionID int64, call service.ToolCall) (string, error) {
		targets, err := sb.Targets(ctx, sessionID)
		if err != nil {
			return "", fmt.Errorf("查询服务器列表失败: %w", err)
		}
		return marshalToolResult(targets)
	})

	// list_files：目录条目。
	exec.Register("list_files", func(ctx context.Context, sessionID int64, call service.ToolCall) (string, error) {
		args := sandboxListFilesArgs{Path: "/"}
		if err := json.Unmarshal([]byte(call.ArgumentsJSON), &args); err != nil {
			return "", fmt.Errorf("解析 list_files 参数失败: %w", err)
		}
		if args.Pod == "" {
			return "", fmt.Errorf("list_files 缺少 pod 参数")
		}
		entries, err := sb.ListFiles(ctx, sessionID, args.Pod, args.Container, args.Path)
		if err != nil {
			return "", fmt.Errorf("列目录失败: %w", err)
		}
		return marshalToolResult(map[string]any{"path": args.Path, "entries": entries})
	})

	// read_file：文件内容。
	exec.Register("read_file", func(ctx context.Context, sessionID int64, call service.ToolCall) (string, error) {
		args := sandboxReadFileArgs{}
		if err := json.Unmarshal([]byte(call.ArgumentsJSON), &args); err != nil {
			return "", fmt.Errorf("解析 read_file 参数失败: %w", err)
		}
		if args.Pod == "" || args.Path == "" {
			return "", fmt.Errorf("read_file 缺少 pod/path 参数")
		}
		content, err := sb.ReadFile(ctx, sessionID, args.Pod, args.Container, args.Path)
		if err != nil {
			return "", fmt.Errorf("读取文件失败: %w", err)
		}
		return marshalToolResult(map[string]any{"path": args.Path, "content": content})
	})

	// run_diagnostic：只读诊断命令。
	exec.Register("run_diagnostic", func(ctx context.Context, sessionID int64, call service.ToolCall) (string, error) {
		args := sandboxDiagnosticArgs{}
		if err := json.Unmarshal([]byte(call.ArgumentsJSON), &args); err != nil {
			return "", fmt.Errorf("解析 run_diagnostic 参数失败: %w", err)
		}
		if args.Pod == "" || args.Command == "" {
			return "", fmt.Errorf("run_diagnostic 缺少 pod/command 参数")
		}
		var argList []string
		if args.Args != "" {
			argList = strings.Fields(args.Args)
		}
		out, err := sb.Exec(ctx, sessionID, service.ExecSpec{
			Pod:       args.Pod,
			Container: args.Container,
			Command:   args.Command,
			Args:      argList,
		})
		if err != nil {
			return "", fmt.Errorf("诊断命令执行失败: %w", err)
		}
		return marshalToolResult(map[string]any{
			"command": append([]string{args.Command}, argList...),
			"output":  out,
		})
	})

	// write_file：写文件（M4）。编排层已在调用前完成用户审批。
	// M5：写入前取旧内容快照，写入成功后登记（登记失败仅日志，不影响写结果）。
	exec.Register("write_file", func(ctx context.Context, sessionID int64, call service.ToolCall) (string, error) {
		args := sandboxWriteFileArgs{}
		if err := json.Unmarshal([]byte(call.ArgumentsJSON), &args); err != nil {
			return "", fmt.Errorf("解析 write_file 参数失败: %w", err)
		}
		if args.Pod == "" || args.Path == "" {
			return "", fmt.Errorf("write_file 缺少 pod/path 参数")
		}

		// 写前快照：区分"新建"与"覆盖"；其他读错误不阻断写，仅按未知前态处理。
		beforeState := "missing"
		beforeExisted := false
		beforeContent := ""
		if old, err := sb.ReadFile(ctx, sessionID, args.Pod, args.Container, args.Path); err != nil {
			if errors.Is(err, service.ErrSandboxFileNotFound) {
				beforeExisted = false
			} else {
				beforeState = "unknown"
				log.Printf("write_file 写前快照读取失败，前态未知: session=%d path=%s err=%v",
					sessionID, args.Path, err)
			}
		} else {
			beforeState = "existed"
			beforeExisted = true
			beforeContent = old
		}

		if err := sb.WriteFile(ctx, sessionID, args.Pod, args.Container, args.Path, args.Content); err != nil {
			return "", fmt.Errorf("%w: write result unavailable", service.ErrToolOutcomeUnconfirmed)
		}

		// 写成功后登记：失败只记日志，不让已成功的写操作报错。
		if recorder != nil && beforeState != "unknown" {
			if err := recorder.Record(ctx, service.ChangeRecord{
				SessionID:      sessionID,
				ConversationID: call.ConversationID,
				ApprovalID:     call.ApprovalID,
				Pod:            args.Pod,
				Container:      args.Container,
				Path:           args.Path,
				BeforeExisted:  beforeExisted,
				BeforeContent:  beforeContent,
				AfterContent:   args.Content,
			}); err != nil {
				log.Printf("write_file 修改登记失败，结果未确认: session=%d", sessionID)
				return "", service.ErrToolOutcomeUnconfirmed
			}
		}

		return marshalToolResult(map[string]any{
			"path":         args.Path,
			"bytes":        len(args.Content),
			"before_state": beforeState,
		})
	})
}
