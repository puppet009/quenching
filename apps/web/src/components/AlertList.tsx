import { useState, useEffect } from "react";
import {useServerClock} from "@quenching/shared";
import {
  ALERT_LEVEL_CONFIG,
  formatAlertTime,
  formatAlertExactTime,
  type AlertItem,
  type AlertLevel,
  type AlertStatus,
} from "../services/alerts";

interface AlertListProps {
  /** 告警列表数据 */
  alerts: AlertItem[];
  /** 加载状态 */
  loading?: boolean;
  /** 点击告警回调 */
  onAlertClick?: (alert: AlertItem) => void;
  /** 最大显示条数（默认 20） */
  maxItems?: number;
  /** 是否显示筛选器 */
  showFilter?: boolean;
  className?: string;
}

/**
 * 告警实时列表组件（T2-21）
 *
 * - 按级别展示告警（info/warning/critical）
 * - 触发中 vs 已恢复 状态区分
 * - 新告警高亮闪烁动画
 * - 支持按级别/状态筛选
 * - 时间相对展示 + hover 精确时间
 * - 加载骨架屏 / 空状态
 */
export function AlertList({
  alerts,
  loading = false,
  onAlertClick,
  maxItems = 20,
  showFilter = true,
  className = "",
}: AlertListProps) {
  useServerClock();
  const [levelFilter, setLevelFilter] = useState<AlertLevel | "all">("all");
  const [statusFilter, setStatusFilter] = useState<AlertStatus | "all">("all");
  const [newAlertIds, setNewAlertIds] = useState<Set<string>>(new Set());

  // 新告警高亮动画：新出现的告警 3 秒后移除高亮
  useEffect(() => {
    const ids = new Set(alerts.slice(0, maxItems).map((a) => a.id));
    setNewAlertIds((prev) => {
      const next = new Set(prev);
      ids.forEach((id) => {
        if (!prev.has(id)) next.add(id);
      });
      return next;
    });

    const timer = setTimeout(() => {
      setNewAlertIds(new Set());
    }, 3000);

    return () => clearTimeout(timer);
  }, [alerts, maxItems]);

  // 筛选
  const filtered = alerts.filter((a) => {
    if (levelFilter !== "all" && a.level !== levelFilter) return false;
    if (statusFilter !== "all" && a.status !== statusFilter) return false;
    return true;
  });

  const displayAlerts = filtered.slice(0, maxItems);

  // 统计
  const firingCount = alerts.filter((a) => a.status === "firing").length;
  const criticalCount = alerts.filter((a) => a.level === "critical" && a.status === "firing").length;
  const warningCount = alerts.filter((a) => a.level === "warning" && a.status === "firing").length;

  return (
    <div className={`bg-white rounded-xl border border-ink-100 overflow-hidden ${className}`}>
      {/* 头部：统计 + 筛选 */}
      <div className="px-4 py-3 border-b border-ink-100 bg-ink-50/50">
        <div className="flex items-center justify-between mb-2">
          <div className="flex items-center gap-2">
            <span className="text-base">🔔</span>
            <span className="text-[14px] font-bold text-ink-800">实时告警</span>
            {firingCount > 0 && (
              <span className="px-1.5 py-0.5 text-[10px] font-bold text-white bg-danger-500 rounded-full">
                {firingCount}
              </span>
            )}
          </div>
          <div className="flex items-center gap-3 text-[11px]">
            {criticalCount > 0 && (
              <span className="flex items-center gap-1 text-danger-600 font-medium">
                <span className="w-1.5 h-1.5 rounded-full bg-danger-500 animate-pulse" />
                严重 {criticalCount}
              </span>
            )}
            {warningCount > 0 && (
              <span className="flex items-center gap-1 text-warn-600 font-medium">
                <span className="w-1.5 h-1.5 rounded-full bg-warn-500" />
                警告 {warningCount}
              </span>
            )}
          </div>
        </div>

        {showFilter && (
          <div className="flex items-center gap-2">
            <div className="flex items-center gap-1 bg-white rounded-lg p-0.5 border border-ink-200">
              {(["all", "critical", "warning", "info"] as const).map((lvl) => (
                <button
                  key={lvl}
                  onClick={() => setLevelFilter(lvl)}
                  className={`px-2 py-1 text-[11px] rounded-md font-medium transition-colors ${
                    levelFilter === lvl
                      ? "bg-brand-500 text-white"
                      : "text-ink-500 hover:text-ink-700 hover:bg-ink-50"
                  }`}
                >
                  {lvl === "all" ? "全部" : ALERT_LEVEL_CONFIG[lvl].label}
                </button>
              ))}
            </div>
            <div className="flex items-center gap-1 bg-white rounded-lg p-0.5 border border-ink-200">
              {(["all", "firing", "resolved"] as const).map((st) => (
                <button
                  key={st}
                  onClick={() => setStatusFilter(st)}
                  className={`px-2 py-1 text-[11px] rounded-md font-medium transition-colors ${
                    statusFilter === st
                      ? "bg-brand-500 text-white"
                      : "text-ink-500 hover:text-ink-700 hover:bg-ink-50"
                  }`}
                >
                  {st === "all" ? "全部" : st === "firing" ? "触发中" : "已恢复"}
                </button>
              ))}
            </div>
          </div>
        )}
      </div>

      {/* 告警列表 */}
      <div className="max-h-[400px] overflow-y-auto">
        {loading ? (
          <SkeletonList count={5} />
        ) : displayAlerts.length === 0 ? (
          <EmptyState />
        ) : (
          <ul className="divide-y divide-ink-50">
            {displayAlerts.map((alert) => (
              <AlertItemRow
                key={alert.id}
                alert={alert}
                isNew={newAlertIds.has(alert.id)}
                onClick={onAlertClick ? () => onAlertClick(alert) : undefined}
              />
            ))}
          </ul>
        )}
      </div>

      {/* 底部提示 */}
      {!loading && filtered.length > maxItems && (
        <div className="px-4 py-2 text-center text-[11px] text-ink-400 border-t border-ink-50">
          共 {filtered.length} 条，仅展示最近 {maxItems} 条
        </div>
      )}
    </div>
  );
}

/* ===== 子组件：单条告警 ===== */

function AlertItemRow({
  alert,
  isNew,
  onClick,
}: {
  alert: AlertItem;
  isNew: boolean;
  onClick?: () => void;
}) {
  const config = ALERT_LEVEL_CONFIG[alert.level];
  const isFiring = alert.status === "firing";
  const isResolved = alert.status === "resolved";

  return (
    <li
      className={`
        px-4 py-3 transition-all duration-300
        ${onClick ? "cursor-pointer hover:bg-ink-50" : ""}
        ${isNew ? "animate-pulse bg-brand-50/50" : ""}
        ${isResolved ? "opacity-60" : ""}
      `}
      onClick={onClick}
    >
      <div className="flex items-start gap-3">
        {/* 状态图标 */}
        <div className="flex-shrink-0 mt-0.5">
          {isFiring && config.pulseColor ? (
            <span className="relative flex h-3 w-3">
              <span
                className={`animate-ping absolute inline-flex h-full w-full rounded-full ${config.pulseColor} opacity-75`}
              />
              <span className={`relative inline-flex rounded-full h-3 w-3 ${config.dotColor}`} />
            </span>
          ) : (
            <span className={`inline-flex rounded-full h-3 w-3 ${config.dotColor}`} />
          )}
        </div>

        {/* 内容 */}
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 mb-0.5">
            <span className={`text-[13px] font-semibold ${config.textColor}`}>
              {alert.title}
            </span>
            <span
              className={`
                px-1.5 py-0.5 text-[9px] font-bold rounded
                ${config.bgColor} ${config.textColor}
              `}
            >
              {config.label}
            </span>
            <span
              className={`px-1.5 py-0.5 text-[9px] font-bold rounded ${
                alert.source === "frontend"
                  ? "bg-violet-50 text-violet-700"
                  : "bg-ink-100 text-ink-600"
              }`}
            >
              {alert.source === "frontend" ? "页面观测" : "后端"}
            </span>
            {isResolved && (
              <span className="px-1.5 py-0.5 text-[9px] font-bold rounded bg-ok-50 text-ok-700">
                已恢复
              </span>
            )}
          </div>

          <p className="text-[12px] text-ink-500 mb-1.5 line-clamp-2">
            {alert.message}
          </p>

          <div className="flex items-center gap-3 text-[11px] text-ink-400">
            {alert.step && (
              <span className="flex items-center gap-1">
                <span>🖱️</span>
                {alert.step}
              </span>
            )}
            {alert.service && (
              <span className="flex items-center gap-1">
                <span>🔧</span>
                {alert.service}
              </span>
            )}
            {alert.current_value !== undefined && alert.threshold !== undefined && (
              <span className="flex items-center gap-1">
                <span>📊</span>
                当前 {alert.current_value}{" "}
                <span className="text-ink-300">/</span> 阈值 {alert.threshold}
              </span>
            )}
            <span
              className="ml-auto flex items-center gap-1"
              title={formatAlertExactTime(alert.firing_at)}
            >
              <span>⏱️</span>
              {isResolved
                ? `恢复于 ${formatAlertTime(alert.resolved_at!)}`
                : formatAlertTime(alert.firing_at)}
            </span>
          </div>
        </div>
      </div>
    </li>
  );
}

/* ===== 子组件：骨架屏 ===== */

function SkeletonList({ count }: { count: number }) {
  return (
    <ul className="divide-y divide-ink-50">
      {Array.from({ length: count }).map((_, i) => (
        <li key={i} className="px-4 py-3 animate-pulse">
          <div className="flex items-start gap-3">
            <div className="w-3 h-3 rounded-full bg-ink-100 mt-0.5" />
            <div className="flex-1">
              <div className="h-4 w-32 bg-ink-100 rounded mb-1.5" />
              <div className="h-3 w-full bg-ink-100 rounded mb-1.5" />
              <div className="h-3 w-2/3 bg-ink-100 rounded" />
            </div>
          </div>
        </li>
      ))}
    </ul>
  );
}

/* ===== 子组件：空状态 ===== */

function EmptyState() {
  return (
    <div className="py-12 text-center">
      <div className="text-4xl mb-2">✅</div>
      <div className="text-[13px] text-ink-500 font-medium">暂无告警</div>
      <div className="text-[11px] text-ink-400 mt-1">系统运行正常，一切安好</div>
    </div>
  );
}
