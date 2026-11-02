import { ManagedCommandPanel } from "../components/training/ManagedCommandPanel";
import { BrainStatus } from "../components/training/BrainStatus";
import { useState, useEffect, useRef, useCallback } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { createTrainingCompletion } from "../services/trainingCompletion";
import { createOperationFlush } from "../services/operationQueue";
import {
  startSession,
  finishSession,
  fetchSessionDetail,
  reportOperations,
  type SessionDetail,
  type OperationInput,
} from "../services/session";
import {
  queryMetricRange,
  getHealthOverview,
  type HealthServiceCard,
} from "../services/monitoring";
import { listAlerts, type AlertItem } from "../services/alerts";
import { TrainingDesktop, type TrainingStatus, type DockItem } from "../components/training/TrainingDesktop";
import { Window } from "../components/training/Window";
import { DesktopIcon } from "../components/training/DesktopIcon";
import { ServerWindow } from "../components/training/ServerWindow";
import { MonitorWidget } from "../components/training/MonitorWidget";
import { AlertWidget } from "../components/training/AlertWidget";
import { AssistantFab } from "../components/training/AssistantFab";
import { raiseWindow } from "../utils/windowGeometry";
import { listTargets, type ServerTarget } from "../services/sandbox";
import { RealtimeChart, nowMs, sessionElapsedMs } from "@quenching/shared";
import { ServiceHealthGrid } from "../components/ServiceHealthCard";
import { LogViewer } from "../components/LogViewer";
import {
  ALL_METRICS,
  METRIC_CONFIG,
  type MetricName,
  type DataPoint,
  type ServiceHealth,
} from "@quenching/shared";
import {
  queryLogs,
  getLatestLogs,
  tailLogs,
  listLogsServices,
  startLogStream,
  stopLogStream,
  exportLogs,
  LogStreamLimitError,
  type LogItem,
  type LogLevel,
} from "../services/logs";
import {
  commandToOperation,
  isRecordableCommand,
  uiOperation,
  type OperationCategory,
} from "../utils/operationTaxonomy";

/** 后端难度 → 布局难度（normal 映射为 medium） */
function adaptDifficulty(d: string): "easy" | "medium" | "hard" {
  if (d === "easy" || d === "hard") return d;
  return "medium";
}

/** 后端会话状态 → 布局状态 */
function adaptSessionStatus(s: string): TrainingStatus {
  if (s === "completed") return "completed";
  // aborted 有两种来路：用户主动离开、连续空闲被系统判弃班；两者都意味着环境已回收。
  if (s === "failed" || s === "aborted") return "ended";
  return "running";
}

/** 健康卡片 → shared ServiceHealth（供 ServiceHealthGrid 使用） */
function adaptHealthCard(card: HealthServiceCard): ServiceHealth {
  return {
    service: card.service,
    healthy: card.status === "healthy",
    cpu_usage: card.cpu_usage,
    memory_usage: card.memory_usage,
    error_rate: card.error_rate,
    reason: card.reason || undefined,
  };
}

/** 工具窗口定义（Dock 可开）。配置统一通过服务器窗口的文件管理器查看，不再单列。 */
type ToolKind = "metrics" | "health" | "logs" | "commands";

const TOOL_META: Record<ToolKind, { title: string; icon: string; width: number; height: number }> = {
 commands: { title: "命令执行与操作记录", icon: "commands", width: 1000, height: 680 },
  metrics: { title: "实时监控", icon: "metrics", width: 1000, height: 680 },
  health: { title: "服务健康", icon: "health", width: 960, height: 640 },
  logs: { title: "日志查看", icon: "logs", width: 1040, height: 680 },
};

/** 服务器窗口默认尺寸。 */
const SERVER_WIN = { width: 1040, height: 680 };

/** Dock 工具窗口 → 对应的观察类操作类型。 */
const TOOL_OBSERVE_TYPE: Record<string, string> = {
  metrics: "view_metric",
  health: "view_metric",
  logs: "view_log",
};

/**
 * 实训环境页（桌面化：电脑桌面 + 服务器图标 + 真实终端 + 监控/告警小部件）
 *
 * 访问路径：/training/:projectId?session=<会话ID>&sandbox=<沙箱ID>
 */
export function TrainingPage() {
  const { id } = useParams<{ id: string }>();
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();

  const projectId = Number(id);
  const sessionParam = searchParams.get("session");
  const sessionId = sessionParam ? Number(sessionParam) : NaN;
  const sandboxId = searchParams.get("sandbox") || "";

  const hasSession = Number.isFinite(sessionId) && sessionId > 0;
  const hasSandbox = Boolean(sandboxId);

  // 实训状态
  const [status, setStatus] = useState<TrainingStatus>("running");
  const [elapsedMs, setElapsedMs] = useState(0);
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState("");

  // 会话详情（header 数据源）
  const [detail, setDetail] = useState<SessionDetail | null>(null);

  // 服务器目标（桌面图标 / 服务器窗口）
  const [targets, setTargets] = useState<ServerTarget[]>([]);

  // 日志面板
  const [logData, setLogData] = useState<LogItem[]>([]);
  const [logServices, setLogServices] = useState<string[]>([]);
  const [logService, setLogService] = useState("");
  const [logLevel, setLogLevel] = useState<LogLevel | "all">("all");
  const [logsLoading, setLogsLoading] = useState(false);
  const [logsError, setLogsError] = useState("");
  const [logHasMore, setLogHasMore] = useState(false);
  const [logLoadingMore, setLogLoadingMore] = useState(false);
  const [logCursor, setLogCursor] = useState("");
  const [logLive, setLogLive] = useState(true);
  const [logNotice, setLogNotice] = useState("");
  const [exporting, setExporting] = useState(false);
  const [exportTruncated, setExportTruncated] = useState(false);
  const logsEnteredRef = useRef(false);
  const logsBusyRef = useRef(false);
  const logNewestTsRef = useRef(0);

  // 左侧（现健康卡片驱动的）当前服务
  const [activeService, setActiveService] = useState("");

  // 指标数据
  const [metricData, setMetricData] = useState<Record<MetricName, DataPoint[]>>(() => {
    const data = {} as Record<MetricName, DataPoint[]>;
    ALL_METRICS.forEach((m) => {
      data[m] = [];
    });
    return data;
  });
  const [metricsLoading, setMetricsLoading] = useState(false);
  const [metricsError, setMetricsError] = useState("");
  const [selectedMetric, setSelectedMetric] = useState<MetricName>("cpu_usage");

  // 健康概览
  const [healthCards, setHealthCards] = useState<HealthServiceCard[]>([]);
  const [healthLoading, setHealthLoading] = useState(false);
  const [healthError, setHealthError] = useState("");

  // 告警
  const [alerts, setAlerts] = useState<AlertItem[]>([]);
  const [alertsLoading, setAlertsLoading] = useState(false);

  // ===== 窗口管理 =====
  const [assistantOpen, setAssistantOpen] = useState(false);
  // openOrder：已打开窗口 id 的叠放顺序（末尾为最前/聚焦）。
  const [openOrder, setOpenOrder] = useState<string[]>([]);
  // 最小化集合。
  const [minimized, setMinimized] = useState<Record<string, boolean>>({});
  // 当前聚焦窗口（z-index 依据 openOrder 末尾，单独记录用于高亮）。
  const [focusId, setFocusId] = useState<string>("");

  const isOpen = useCallback((winId: string) => openOrder.includes(winId), [openOrder]);

  const openWindow = useCallback((winId: string) => {
    setMinimized((m) => ({ ...m, [winId]: false }));
    setOpenOrder((cur) => raiseWindow(cur, winId));
    setFocusId(winId);
  }, []);

  const focusWindow = useCallback((winId: string) => {
    setMinimized((m) => ({ ...m, [winId]: false }));
    setOpenOrder((cur) => cur.includes(winId) ? raiseWindow(cur, winId) : cur);
    setFocusId(winId);
  }, []);

  const closeWindow = useCallback((winId: string) => {
    setOpenOrder((cur) => cur.filter((x) => x !== winId));
    setFocusId((cur) => (cur === winId ? "" : cur));
  }, []);

  const minimizeWindow = useCallback((winId: string) => {
    setMinimized((m) => ({ ...m, [winId]: true }));
    setFocusId((current) => current === winId ? "" : current);
  }, []);

  // 打开服务器窗口（桌面图标 / 告警点击）。
  const openServer = useCallback(
    (name: string) => {
      if (!targets.some((t) => t.name === name)) return;
      openWindow(`server:${name}`);
    },
    [targets, openWindow],
  );

  // ===== 操作上报队列 =====
  const operationQueueRef = useRef<{ sessionId: number; operations: OperationInput[]; flush(): Promise<void> } | null>(null);
  if (!operationQueueRef.current || !Object.is(operationQueueRef.current.sessionId, sessionId)) {
    const operations: OperationInput[] = [];
    operationQueueRef.current = {
      sessionId,
      operations,
      flush: hasSession ? createOperationFlush(operations, batch => reportOperations(sessionId, batch)) : async () => {},
    };
  }
  const { operations, flush: flushOperations } = operationQueueRef.current;
  const completeTrainingRef = useRef(createTrainingCompletion());
  const completionBusyRef = useRef(false);
  const completionVersionRef = useRef(0);

  useEffect(() => {
    completionVersionRef.current += 1;
    completeTrainingRef.current = createTrainingCompletion();
    completionBusyRef.current = false;
    setSubmitting(false);
    setSubmitError("");
    return () => { completionVersionRef.current += 1; };
  }, [sessionId, projectId]);

  const enqueueOperation = useCallback((op: OperationInput) => {
    operations.push({
      ...op,
      virtual_timestamp_sec: Math.floor(nowMs() / 1000),
    });
  }, [operations]);

  /** 界面动作留痕（观察类只在"打开/切换"这一决策点记一次，避免轮询污染）。 */
  const reportUi = useCallback(
    (operationType: string, category: OperationCategory, target: string, source: string) => {
      enqueueOperation(uiOperation(operationType, category, target, source));
    },
    [enqueueOperation],
  );

  /** 终端命令留痕：按语义字典归一为 observe/analyze/action。 */
  const reportCommand = useCallback(
    (pod: string, line: string) => {
      if (!isRecordableCommand(line)) return;
      enqueueOperation(commandToOperation(line, pod));
    },
    [enqueueOperation],
  );

  /** 打开窗口即一次观察动作。 */
  const observedWindowsRef = useRef<Record<string, boolean>>({});

  // ===== 进入页面：开始会话（幂等）+ 拉详情 =====
  useEffect(() => {
    if (!hasSession) return;
    let cancelled = false;

    const boot = async () => {
      try {
        await startSession(sessionId);
      } catch {
        /* 开始失败不阻塞查看 */
      }
      try {
        const d = await fetchSessionDetail(sessionId);
        if (cancelled) return;
        setDetail(d);
        setStatus(adaptSessionStatus(d.status));
      } catch {
        /* 使用兜底值 */
      }
    };

    void boot();
    return () => {
      cancelled = true;
    };
  }, [hasSession, sessionId]);

  // ===== 班次可能被系统判定弃班（连续空闲超时后回收环境）：只轮询状态 =====
  // 离开活跃态就把页面停住，否则学员回来会对着一个已经不存在的环境继续敲命令。
  // 同步后端详情与开始时刻；各面板自己的数据仍由各自的刷新负责。
  useEffect(() => {
    if (!hasSession || status !== "running") return;
    let active = true;
    const timer = setInterval(() => {
      fetchSessionDetail(sessionId)
        .then((d) => {
          if (!active) return;
          setDetail(d);
          const next = adaptSessionStatus(d.status);
          if (next !== "running") setStatus(next);
        })
        .catch(() => {
          /* 单次失败忽略，下一轮再试 */
        });
    }, 30000);
    return () => { active = false; clearInterval(timer); };
  }, [hasSession, sessionId, status]);

  // ===== 拉取服务器目标（会话就绪后）=====
  useEffect(() => {
    if (!hasSession || !hasSandbox) return;
    let cancelled = false;
    const load = async () => {
      try {
        const rows = await listTargets(sessionId);
        if (!cancelled) setTargets(rows);
      } catch {
        /* 单次失败保留空，可用监控兜底；不阻塞页面 */
      }
    };
    void load();
    const timer = setInterval(load, 15000);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [hasSession, hasSandbox, sessionId]);

  // 从后端开始时刻计算，刷新页面、后台标签节流后仍能恢复真实已用时间。
  useEffect(() => {
    if (status !== "running") return;
    const update = () => setElapsedMs(sessionElapsedMs(detail?.startTime));
    update();
    const timer = setInterval(update, 1000);
    return () => clearInterval(timer);
  }, [status, detail?.startTime]);

  // ===== 指标查询：最近 15 分钟，按当前服务 =====
  const refreshMetrics = useCallback(async () => {
    if (!hasSandbox) return;
    setMetricsLoading(true);
    setMetricsError("");
    const end = nowMs();
    const start = end - 15 * 60 * 1000;
    try {
      const results = await Promise.all(
        ALL_METRICS.map((m) =>
          queryMetricRange({
            metric: m,
            service: activeService || undefined,
            start_time: start,
            end_time: end,
            step: 30,
          }).catch(() => null),
        ),
      );
      const next = {} as Record<MetricName, DataPoint[]>;
      ALL_METRICS.forEach((m, i) => {
        const r = results[i];
        next[m] = r?.points ?? [];
      });
      setMetricData(next);
    } catch (err) {
      setMetricsError(err instanceof Error ? err.message : "指标加载失败");
    } finally {
      setMetricsLoading(false);
    }
  }, [hasSandbox, activeService]);

  useEffect(() => {
    if (!hasSandbox) return;
    void refreshMetrics();
    const timer = setInterval(() => {
      if (status === "running") void refreshMetrics();
    }, 15000);
    return () => clearInterval(timer);
  }, [hasSandbox, status, refreshMetrics]);

  // ===== 健康概览：首次拉取 + 10s 轮询 =====
  const refreshHealth = useCallback(async () => {
    if (!hasSandbox) return;
    setHealthLoading(true);
    try {
      const overview = await getHealthOverview(sandboxId);
      setHealthCards(overview.services ?? []);
      setHealthError("");
    } catch (err) {
      setHealthError(err instanceof Error ? err.message : "健康概览加载失败");
    } finally {
      setHealthLoading(false);
    }
  }, [hasSandbox, sandboxId]);

  useEffect(() => {
    if (!hasSandbox) return;
    void refreshHealth();
    const timer = setInterval(() => {
      if (status === "running") void refreshHealth();
    }, 10000);
    return () => clearInterval(timer);
  }, [hasSandbox, status, refreshHealth]);

  useEffect(() => {
    if (!activeService && healthCards.length > 0) {
      setActiveService(healthCards[0].service);
    }
  }, [activeService, healthCards]);

  const healthData: ServiceHealth[] = healthCards.map(adaptHealthCard);

  // ===== 告警：首次拉取 + 10s 轮询 =====
  const refreshAlerts = useCallback(async () => {
    if (!hasSandbox) return;
    setAlertsLoading(true);
    try {
      const rows = await listAlerts({ sandboxId });
      setAlerts(rows);
    } catch {
      /* 保留现有告警 */
    } finally {
      setAlertsLoading(false);
    }
  }, [hasSandbox, sandboxId]);

  useEffect(() => {
    if (!hasSandbox) return;
    void refreshAlerts();
    const timer = setInterval(() => {
      if (status === "running") void refreshAlerts();
    }, 10000);
    return () => clearInterval(timer);
  }, [hasSandbox, status, refreshAlerts]);

  // ===== 操作上报定时 flush + 卸载 flush =====
  useEffect(() => {
    const timer = setInterval(() => {
      void flushOperations().catch(() => {});
    }, 10000);
    return () => clearInterval(timer);
  }, [flushOperations]);

  useEffect(() => {
    return () => {
      void flushOperations().catch(() => {});
    };
  }, [flushOperations]);

  // ===== 日志：首次打开时加载，之后按服务重载 =====
  const loadInitialLogs = useCallback(async () => {
    if (!sandboxId) return;
    logsBusyRef.current = true;
    setLogsLoading(true);
    setLogsError("");
    setLogNotice("");
    try {
      const [latest, services] = await Promise.all([
        getLatestLogs(sandboxId, { service: logService || undefined, limit: 200 }),
        listLogsServices(sandboxId).catch(() => [] as string[]),
      ]);
      const reversed = [...latest.items].reverse();
      setLogData(reversed);
      logNewestTsRef.current = reversed.length > 0 ? reversed[reversed.length - 1].ts : 0;
      setLogHasMore(latest.hasMore);
      setLogCursor(latest.nextCursor || "");
      setLogServices(services);
      try {
        await startLogStream({ sandboxId, service: logService || undefined });
      } catch (err) {
        if (err instanceof LogStreamLimitError) {
          setLogNotice(err.message);
        }
      }
    } catch (err) {
      setLogsError(err instanceof Error ? err.message : "日志加载失败");
    } finally {
      setLogsLoading(false);
      logsBusyRef.current = false;
    }
  }, [sandboxId, logService]);

  // 日志窗口首次打开时加载。
  useEffect(() => {
    if (!isOpen("tool:logs") || logsEnteredRef.current || !sandboxId) return;
    logsEnteredRef.current = true;
    void loadInitialLogs();
  }, [isOpen, sandboxId, loadInitialLogs]);

  useEffect(() => {
    if (!logsEnteredRef.current || !sandboxId) return;
    void loadInitialLogs();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [logService, sandboxId]);

  // 实时 tail（仅日志窗口打开且 running）。
  useEffect(() => {
    if (!isOpen("tool:logs") || !logLive || status !== "running" || !sandboxId) return;
    const timer = setInterval(() => {
      if (logsBusyRef.current) return;
      logsBusyRef.current = true;
      const since = logNewestTsRef.current || nowMs();
      tailLogs(sandboxId, since, { service: logService || undefined, limit: 200 })
        .then((res) => {
          if (res.items.length === 0) return;
          setLogData((cur) => {
            const seen = new Set(cur.map((l) => `${l.ts}|${l.message}`));
            const merged = [...cur];
            res.items.forEach((l) => {
              const key = `${l.ts}|${l.message}`;
              if (!seen.has(key)) {
                seen.add(key);
                merged.push(l);
              }
            });
            const clipped = merged.slice(-1000);
            const last = clipped[clipped.length - 1];
            if (last && last.ts > logNewestTsRef.current) logNewestTsRef.current = last.ts;
            return clipped;
          });
        })
        .catch(() => {
          /* 静默，下周期重试 */
        })
        .finally(() => {
          logsBusyRef.current = false;
        });
    }, 3000);
    return () => clearInterval(timer);
  }, [isOpen, logLive, status, sandboxId, logService]);

  useEffect(() => {
    return () => {
      if (sandboxId) void stopLogStream(sandboxId).catch(() => undefined);
    };
  }, [sandboxId]);

  const handleLoadMoreLogs = useCallback(async () => {
    if (!sandboxId || logLoadingMore || !logHasMore || !logCursor) return;
    setLogLoadingMore(true);
    try {
      const res = await queryLogs({
        sandboxId,
        service: logService || undefined,
        limit: 200,
        direction: "backward",
        cursor: logCursor,
      });
      setLogData((prev) => {
        const newestPrev = prev.length > 0 ? prev[0].ts : Infinity;
        const older = res.items.filter((l) => l.ts < newestPrev).reverse();
        return [...older, ...prev].slice(-1000);
      });
      setLogHasMore(res.hasMore);
      setLogCursor(res.nextCursor || "");
    } catch (err) {
      setLogsError(err instanceof Error ? err.message : "历史日志加载失败");
    } finally {
      setLogLoadingMore(false);
    }
  }, [sandboxId, logLoadingMore, logHasMore, logCursor, logService]);

  const handleExportLogs = useCallback(async () => {
    if (!sandboxId || exporting) return;
    setExporting(true);
    setExportTruncated(false);
    try {
      const res = await exportLogs({
        sandboxId,
        service: logService || undefined,
        level: logLevel === "all" ? undefined : logLevel,
      });
      const url = URL.createObjectURL(res.blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = res.filename;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
      setExportTruncated(res.truncated);
    } catch (err) {
      setLogNotice(err instanceof Error ? err.message : "日志导出失败");
    } finally {
      setExporting(false);
    }
  }, [sandboxId, exporting, logService, logLevel]);

  // 健康卡片点击：记录当前服务（用于指标过滤）。
  const handleHealthServiceClick = useCallback(
    (svc: string) => {
      reportUi("view_metric", "observe", svc, "health_grid");
      setActiveService(svc);
    },
    [reportUi],
  );

  // 收到后端结束确认即可离开，环境回收与复盘由后台继续。
  const handleComplete = useCallback(async (outcome: "completed" | "aborted") => {
    if (completionBusyRef.current) return;
    setSubmitError("");

    if (!Number.isSafeInteger(projectId) || projectId < 0) {
      setSubmitError("缺少有效的项目 ID，无法结束实训");
      return;
    }
    if (!hasSession) {
      setSubmitError("当前训练会话尚未绑定（缺少 session 参数），请从项目入口进入训练");
      return;
    }

    completionBusyRef.current = true;
    const version = completionVersionRef.current;
    const location = window.location.pathname + window.location.search;
    const isCurrentCompletion = () => version === completionVersionRef.current && location === window.location.pathname + window.location.search;
    setSubmitting(true);
    try {
      if (outcome === "completed") reportUi("fix_complete", "action", "all_services", "submit");
      const accepted = await completeTrainingRef.current(sessionId, projectId, {
        flush: flushOperations,
        finish: () => finishSession(sessionId, outcome),
      });
      if (!accepted || !isCurrentCompletion()) return;
      setStatus(outcome === "completed" ? "completed" : "ended");
      navigate(`/history?session=${sessionId}`);
    } catch (err) {
      if (isCurrentCompletion()) setSubmitError(err instanceof Error ? err.message : "结束实训失败，请重试");
    } finally {
      if (isCurrentCompletion()) {
        completionBusyRef.current = false;
        setSubmitting(false);
      }
    }
  }, [projectId, hasSession, sessionId, flushOperations, navigate, reportUi]);

  const handleExit = useCallback(() => handleComplete("aborted"), [handleComplete]);
  const handleSubmit = useCallback(() => handleComplete("completed"), [handleComplete]);

  // Dock 项。
  const dockItems: DockItem[] = [
 {id:"commands",label:"命令执行",icon:"⌨️"},
    { id: "metrics", label: "实时监控", icon: "📊" },
    { id: "health", label: "服务健康", icon: "💚" },
    { id: "logs", label: "日志查看", icon: "📜" },
  ];

  const handleDockClick = useCallback(
    (tool: string) => {
      const winId = `tool:${tool}`;
      if (isOpen(winId)) {
        focusWindow(winId);
      } else {
        openWindow(winId);
        if (!observedWindowsRef.current[winId]) {
          observedWindowsRef.current[winId] = true;
          reportUi(TOOL_OBSERVE_TYPE[tool] ?? "other", "observe", tool, "dock");
        }
      }
    },
    [isOpen, focusWindow, openWindow, reportUi],
  );

  /** 日志按服务过滤 = 换一次观察对象。 */
  const handleLogServiceChange = useCallback(
    (svc: string) => {
      reportUi("view_log", "observe", svc || "all", "logs_window");
      setLogService(svc);
    },
    [reportUi],
  );

  // 切换查看的指标 = 一次观察动作（复盘"漏了哪一步"看的就是这些点）。
  const selectMetric = useCallback(
    (m: MetricName) => {
      reportUi("view_metric", "observe", `${activeService || "all"}/${m}`, "metrics_window");
      setSelectedMetric(m);
    },
    [reportUi, activeService],
  );

  // 渲染工具窗口内容。
  const renderToolBody = (tool: ToolKind) => {
    switch (tool) {
      case "commands": return <ManagedCommandPanel key={sessionId} sessionId={sessionId} targets={targets} enabled={hasSession && status==="running"}/>;
      case "metrics":
        return (
          <div className="p-4 space-y-4 overflow-y-auto h-full">
            <div className="flex items-center gap-2 flex-wrap">
              {ALL_METRICS.map((m) => (
                <button
                  key={m}
                  onClick={() => selectMetric(m)}
                  className={`px-3 py-1.5 text-[12px] rounded-lg transition-colors ${
                    selectedMetric === m
                      ? "bg-brand-500 text-white font-medium"
                      : "bg-white text-ink-600 border border-ink-200 hover:border-brand-300"
                  }`}
                >
                  {METRIC_CONFIG[m].displayName}
                </button>
              ))}
            </div>

            <div className="bg-white rounded-xl border border-ink-100 p-4">
              <div className="flex items-center justify-between mb-3">
                <h3 className="text-[14px] font-bold text-ink-800">
                  {METRIC_CONFIG[selectedMetric].displayName}
                </h3>
                <span className="text-[12px] text-ink-400">最近 15 分钟</span>
              </div>
              {metricsError ? (
                <div className="h-[240px] flex items-center justify-center text-[13px] text-danger-600">
                  {metricsError}
                </div>
              ) : metricsLoading && metricData[selectedMetric].length === 0 ? (
                <div className="h-[240px] flex items-center justify-center text-[13px] text-ink-400">
                  指标加载中…
                </div>
              ) : metricData[selectedMetric].length === 0 ? (
                <div className="h-[240px] flex items-center justify-center text-[13px] text-ink-400">
                  暂无指标数据，环境可能尚未产生流量
                </div>
              ) : (
                <RealtimeChart
                  data={metricData[selectedMetric]}
                  metric={selectedMetric}
                  height={240}
                  filled
                  showCurrentValue
                />
              )}
            </div>

            <div className="grid grid-cols-2 gap-3">
              {ALL_METRICS.filter((m) => m !== selectedMetric)
                .slice(0, 4)
                .map((m) => (
                  <div
                    key={m}
                    className="bg-white rounded-xl border border-ink-100 p-3 cursor-pointer hover:border-brand-300 transition-colors"
                    onClick={() => selectMetric(m)}
                  >
                    <div className="flex items-center justify-between mb-2">
                      <span className="text-[12px] font-medium text-ink-600">
                        {METRIC_CONFIG[m].displayName}
                      </span>
                      <span
                        className="text-[14px] font-bold"
                        style={{ color: `rgb(${METRIC_CONFIG[m].color})` }}
                      >
                        {METRIC_CONFIG[m].formatValue(
                          metricData[m][metricData[m].length - 1]?.value || 0,
                        )}
                      </span>
                    </div>
                    {metricData[m].length > 0 ? (
                      <RealtimeChart
                        data={metricData[m]}
                        metric={m}
                        height={56}
                        filled
                        showYAxis={false}
                        showXAxis={false}
                      />
                    ) : (
                      <div className="h-[56px] flex items-center justify-center text-[11px] text-ink-300">
                        暂无数据
                      </div>
                    )}
                  </div>
                ))}
            </div>
          </div>
        );

      case "health":
        return (
          <div className="p-4 h-full overflow-y-auto">
            <h3 className="text-[14px] font-bold text-ink-800 mb-3">服务健康状态</h3>
            {healthError ? (
              <div className="bg-white rounded-xl border border-ink-100 py-16 text-center">
                <div className="text-[13px] text-danger-600">{healthError}</div>
              </div>
            ) : (
              <ServiceHealthGrid
                services={healthData}
                onServiceClick={handleHealthServiceClick}
                loading={healthLoading && healthCards.length === 0}
                columns={2}
              />
            )}
          </div>
        );

      case "logs":
        if (!sandboxId) {
          return (
            <div className="p-4">
              <div className="bg-white rounded-xl border border-ink-100 py-16 text-center">
                <div className="text-3xl mb-2">📋</div>
                <div className="text-[13px] text-ink-500 font-medium">当前训练会话未绑定沙箱环境</div>
                <div className="text-[11px] text-ink-400 mt-1">
                  缺少 sandbox 参数，日志功能不可用；请从项目入口正常进入训练
                </div>
              </div>
            </div>
          );
        }
        return (
          <div className="p-3 h-full">
            <LogViewer
              logs={logData}
              loading={logsLoading}
              services={logServices}
              selectedService={logService}
              onServiceChange={handleLogServiceChange}
              onLevelFilter={setLogLevel}
              order="oldest-first"
              maxLines={200}
              isLive={logLive && status === "running"}
              onLiveToggle={setLogLive}
              hasMore={logHasMore}
              loadingMore={logLoadingMore}
              onLoadMore={handleLoadMoreLogs}
              onExport={handleExportLogs}
              exporting={exporting}
              exportTruncated={exportTruncated}
              notice={
                logsError ? (
                  <span className="text-danger-600">{logsError}</span>
                ) : logNotice ? (
                  logNotice
                ) : undefined
              }
            />
          </div>
        );
    }
  };

  // 当前聚焦服务器（供助手提示）。
  const focusServer =
    focusId.startsWith("server:")
      ? focusId.slice("server:".length)
      : undefined;

  return (
    <TrainingDesktop
      projectName={detail?.projectName || "实训项目"}
      difficulty={adaptDifficulty(detail?.difficulty ?? "normal")}
      status={status}
      elapsedMs={elapsedMs}
      onExit={handleExit}
      onSubmit={status === "running" && !submitting ? handleSubmit : undefined}
      submitting={submitting}
      headerExtra={
        submitting ? (
          <span className="text-[12px] text-warn font-medium animate-pulse">
            正在结束实训…
          </span>
        ) : submitError ? (
          <span
            className="text-[12px] text-danger font-medium max-w-[280px] truncate"
            title={submitError}
          >
            {submitError}
          </span>
        ) : null
      }
      dockItems={dockItems}
      activeDock={focusId.startsWith("tool:") ? focusId.slice(5) : undefined}
      sidebar={<>
      <div className="workbench-overview" hidden={assistantOpen}><div className="workbench-overview-title"><span>环境概览</span><span className="text-xs font-normal text-ink-500">实时更新</span></div>{hasSession && <BrainStatus sessionId={sessionId} />}
        <MonitorWidget
          label="CPU 使用率"
          color="rgb(91,143,230)"
          points={metricData.cpu_usage}
          onClick={() => handleDockClick("metrics")}
        />
        <AlertWidget
          alerts={alerts}
          onAlertClick={(a) => {
            reportUi("view_alert", "observe", a.service || "unknown", "alert_widget");
            if (a.service) openServer(a.service);
          }}
        />
        {(alertsLoading || metricsLoading) && alerts.length === 0 && null}
      </div>


      <AssistantFab open={assistantOpen} onClose={()=>setAssistantOpen(false)}
        sessionId={hasSession ? sessionId : undefined}
        focusServer={focusServer}
        onUserQuestion={(text) =>
          enqueueOperation({
            operation_type: "ask_ai",
            operation_category: "ai_assist",
            target: focusServer || "global",
            detail: { question: text.slice(0, 300), source: "assistant" },
          })
        }
        onApprovalResolved={(approved) =>
          enqueueOperation({
            operation_type: approved ? "follow_ai" : "reject_ai",
            operation_category: "action",
            target: focusServer || "assistant_write",
            result: approved ? "executed" : "skipped",
            detail: { source: "assistant_approval" },
          })
        }
      />
</>}
      onDockClick={handleDockClick}
      windowItems={openOrder.filter(winId=>winId.startsWith("server:")).map(winId=>({id:winId,label:targets.find(target=>target.name===winId.slice(7))?.displayName||winId.slice(7),active:focusId===winId,minimized:!!minimized[winId]}))}
      onWindowClick={focusWindow}
      assistantOpen={assistantOpen}
      onAssistantToggle={()=>setAssistantOpen(open=>!open)}
    >
      {/* 服务器桌面图标（左上） */}
      <div className="absolute left-6 top-6 right-6 flex flex-wrap content-start gap-3">
        {targets.length === 0 && (
          <div className="text-[11px] text-ink-400 px-2 py-1">
            {hasSandbox ? "正在发现服务器…" : "未绑定沙箱"}
          </div>
        )}
        {targets.map((t) => (
          <DesktopIcon
            key={t.name}
            name={t.displayName || t.name}
            phase={t.phase}
            ready={t.ready}
            active={focusId === `server:${t.name}`}
            onOpen={() => openServer(t.name)}
          />
        ))}
      </div>

      {/* 窗口层：工具窗口 + 服务器窗口 */}
      {openOrder.map((winId, idx) => {
        const z = 10 + idx;
        const isActive = winId === focusId;

        if (winId.startsWith("tool:")) {
          const tool = winId.slice("tool:".length) as ToolKind;
          const meta = TOOL_META[tool];
          const cascade = idx % 5;
          return (
            <Window
              key={winId}
              id={winId}
              title={meta.title}
              icon={meta.icon}
              active={isActive}
              zIndex={z}
              initialX={24 + cascade * 30}
              initialY={24 + cascade * 26}
              width={meta.width}
              height={meta.height}
              minimized={!!minimized[winId]}
              onFocus={() => focusWindow(winId)}
              onClose={() => closeWindow(winId)}
              onMinimize={() => minimizeWindow(winId)}
            >
              {renderToolBody(tool)}
            </Window>
          );
        }

        const name = winId.slice("server:".length);
        const target = targets.find((t) => t.name === name);
        if (!target) return null;
        const cascade = idx % 5;
        return (
          <Window
            key={winId}
            id={winId}
            title={`${target.displayName || name} — 终端`}
            icon="🖥️"
            active={isActive}
            zIndex={z}
            initialX={24 + cascade * 32}
            initialY={24 + cascade * 28}
            width={SERVER_WIN.width}
            height={SERVER_WIN.height}
            minimized={!!minimized[winId]}
            onFocus={() => focusWindow(winId)}
            onClose={() => closeWindow(winId)}
            onMinimize={() => minimizeWindow(winId)}
          >
            <ServerWindow
              key={name}
              sessionId={sessionId}
              target={target}
              onCommand={reportCommand}
            />
          </Window>
        );
      })}

    </TrainingDesktop>
  );
}
