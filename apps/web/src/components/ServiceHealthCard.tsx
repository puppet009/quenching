import {
  HEALTH_CONFIG,
  getHealthLevel,
  type ServiceHealth,
} from "@quenching/shared";

interface ServiceHealthCardProps {
  /** 健康状态数据 */
  data: ServiceHealth;
  /** 服务显示名（可选，默认用 data.service） */
  displayName?: string;
  /** 点击回调（可选） */
  onClick?: () => void;
  /** 加载状态 */
  loading?: boolean;
  className?: string;
}

/**
 * 服务健康卡片组件（T2-20）
 *
 * - 显示服务名 + 健康状态（正常/注意/异常）
 * - 3 个关键指标：CPU / 内存 / 错误率
 * - 异常状态红色高亮 + 心跳动画
 * - 异常时显示原因
 * - 可选点击跳转
 * - 加载骨架屏
 */
export function ServiceHealthCard({
  data,
  displayName,
  onClick,
  loading = false,
  className = "",
}: ServiceHealthCardProps) {
  const level = getHealthLevel(data);
  const config = HEALTH_CONFIG[level];
  const name = displayName ?? data.service;

  // 加载骨架屏
  if (loading) {
    return (
      <div
        className={`bg-white rounded-xl border border-ink-100 p-4 animate-pulse ${className}`}
      >
        <div className="flex items-center justify-between mb-3">
          <div className="h-4 w-24 bg-ink-100 rounded" />
          <div className="h-4 w-16 bg-ink-100 rounded-full" />
        </div>
        <div className="grid grid-cols-3 gap-2">
          <div className="h-10 bg-ink-100 rounded-lg" />
          <div className="h-10 bg-ink-100 rounded-lg" />
          <div className="h-10 bg-ink-100 rounded-lg" />
        </div>
      </div>
    );
  }

  return (
    <div
      className={`
        bg-white rounded-xl border-2 p-4 transition-all duration-200
        ${config.borderColor} ${onClick ? "cursor-pointer hover:shadow-md hover:-translate-y-0.5" : ""}
        ${level === "critical" ? "shadow-sm" : ""}
        ${className}
      `}
      onClick={onClick}
      role={onClick ? "button" : undefined}
      tabIndex={onClick ? 0 : undefined}
    >
      {/* 头部：服务名 + 状态 */}
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          <span className="text-lg">🔧</span>
          <span className="text-[14px] font-semibold text-ink-800 truncate max-w-[140px]">
            {name}
          </span>
        </div>
        <div className="flex items-center gap-1.5">
          {/* 状态指示灯 */}
          <span className="relative flex h-2.5 w-2.5">
            {config.pulseColor && (
              <span
                className={`
                  animate-ping absolute inline-flex h-full w-full rounded-full
                  ${config.pulseColor} opacity-75
                `}
              />
            )}
            <span className={`relative inline-flex rounded-full h-2.5 w-2.5 ${config.dotColor}`} />
          </span>
          <span className={`text-[11px] font-semibold ${config.textColor}`}>
            {config.label}
          </span>
        </div>
      </div>

      {/* 3 个指标 */}
      <div className="grid grid-cols-3 gap-2">
        <MiniMetric
          label="CPU"
          value={data.cpu_usage}
          unit="%"
          warning={data.cpu_usage > 80}
          critical={data.cpu_usage > 90}
        />
        <MiniMetric
          label="内存"
          value={data.memory_usage}
          unit="%"
          warning={data.memory_usage > 80}
          critical={data.memory_usage > 90}
        />
        <MiniMetric
          label="错误率"
          value={data.error_rate}
          unit="%"
          decimals={2}
          warning={data.error_rate > 2}
          critical={data.error_rate > 5}
        />
      </div>

      {/* 异常原因 */}
      {data.reason && level === "critical" && (
        <div className="mt-3 pt-3 border-t border-danger-200">
          <div className="text-[11px] text-danger-600 flex items-start gap-1">
            <span>⚠️</span>
            <span className="line-clamp-2">{data.reason}</span>
          </div>
        </div>
      )}
    </div>
  );
}

/* ===== 子组件：迷你指标 ===== */

function MiniMetric({
  label,
  value,
  unit,
  decimals = 1,
  warning = false,
  critical = false,
}: {
  label: string;
  value: number;
  unit: string;
  decimals?: number;
  warning?: boolean;
  critical?: boolean;
}) {
  const valueColor = critical
    ? "text-danger-600"
    : warning
      ? "text-warn-600"
      : "text-ink-700";

  return (
    <div className={`rounded-lg p-2 text-center ${
      critical ? "bg-danger-50" : warning ? "bg-warn-50" : "bg-ink-50"
    }`}>
      <div className="text-[10px] text-ink-500 mb-0.5">{label}</div>
      <div className={`text-[14px] font-bold leading-tight ${valueColor}`}>
        {value.toFixed(decimals)}
        <span className="text-[10px] font-normal ml-0.5 opacity-70">{unit}</span>
      </div>
    </div>
  );
}

/* ===== 服务健康列表（多个服务横向排列） ===== */

/** 固定列数 → 完整静态 grid 类名（供 Tailwind JIT 扫描）。 */
const FIXED_GRID_COLS: Record<number, string> = {
  1: "grid-cols-1",
  2: "grid-cols-2",
  3: "grid-cols-3",
  4: "grid-cols-4",
  5: "grid-cols-5",
  6: "grid-cols-6",
};

interface ServiceHealthGridProps {
  services: ServiceHealth[];
  onServiceClick?: (service: string) => void;
  loading?: boolean;
  className?: string;
  /**
   * 固定每行列数。传入后忽略基于浏览器视口的响应式断点——
   * 网格若渲染在非全屏的“桌面窗口”内，视口很宽但容器很窄，
   * 响应式断点会误判列数，导致卡片被压扁、指标文字竖排。
   */
  columns?: number;
}

/** 服务健康卡片网格（用于概览页展示多服务） */
export function ServiceHealthGrid({
  services,
  onServiceClick,
  loading = false,
  className = "",
  columns,
}: ServiceHealthGridProps) {
  // 用完整静态类名映射：Tailwind JIT 扫描不到 `grid-cols-${n}` 这类
  // 动态拼接，运行时会缺样式。
  const gridCols =
    columns && columns > 0
      ? FIXED_GRID_COLS[Math.min(columns, 6)] ?? "grid-cols-2"
      : "grid-cols-2 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5";
  return (
    <div className={`grid ${gridCols} gap-3 ${className}`}>
      {loading
        ? Array.from({ length: 5 }).map((_, i) => (
            <ServiceHealthCard
              key={`skeleton-${i}`}
              data={{
                service: "",
                healthy: true,
                cpu_usage: 0,
                memory_usage: 0,
                error_rate: 0,
              }}
              loading
            />
          ))
        : services.map((s) => (
            <ServiceHealthCard
              key={s.service}
              data={s}
              onClick={onServiceClick ? () => onServiceClick(s.service) : undefined}
            />
          ))}
    </div>
  );
}
