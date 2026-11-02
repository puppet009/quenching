import {nowMs} from "../utils/time";
import { useMemo, useRef, useState, useEffect } from "react";
import {
  METRIC_CONFIG,
  downsample,
  formatTime,
  type DataPoint,
  type MetricName,
} from "../services/metrics";

interface RealtimeChartProps {
  /** 数据点数组（按时间升序） */
  data: DataPoint[];
  /** 指标名称（决定颜色、单位、格式化） */
  metric: MetricName;
  /** 图表高度（px） */
  height?: number;
  /** 是否显示 Y 轴标签 */
  showYAxis?: boolean;
  /** 是否显示 X 轴时间标签 */
  showXAxis?: boolean;
  /** 是否填充曲线下方区域 */
  filled?: boolean;
  /** 最大显示数据点数（超过自动降采样） */
  maxPoints?: number;
  /** Y 轴最小值（默认自动） */
  yMin?: number;
  /** Y 轴最大值（默认自动） */
  yMax?: number;
  /** 右上角当前值标签 */
  showCurrentValue?: boolean;
  /** 加载状态 */
  loading?: boolean;
  className?: string;
}

/**
 * 实时图表组件（纯 SVG 实现）
 *
 * 特性：
 * - 纯 SVG 绘制，零依赖，轻量高性能
 * - 自动降采样（LTTB 简化版），数据点多时不卡
 * - 渐变填充 + 平滑曲线 + 数据点高亮
 * - Y 轴/X 轴标签，自动计算刻度
 * - 当前值标签（右上角）
 * - 支持自定义高度、颜色、范围
 * - 加载骨架屏
 */
export function RealtimeChart({
  data,
  metric,
  height = 200,
  showYAxis = true,
  showXAxis = true,
  filled = true,
  maxPoints = 120,
  yMin,
  yMax,
  showCurrentValue = true,
  loading = false,
  className = "",
}: RealtimeChartProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(600);
  const [hoverIndex, setHoverIndex] = useState<number | null>(null);

  const config = METRIC_CONFIG[metric];

  // 响应式宽度
  useEffect(() => {
    if (!containerRef.current) return;
    const observer = new ResizeObserver((entries) => {
      for (const entry of entries) {
        setWidth(entry.contentRect.width);
      }
    });
    observer.observe(containerRef.current);
    return () => observer.disconnect();
  }, []);

  // 降采样 + 计算绘图参数
  const { points, yRange, xRange, chartWidth, chartHeight, padding } = useMemo(() => {
    const sampled = downsample(data, maxPoints);

    const leftPad = showYAxis ? 45 : 8;
    const rightPad = showCurrentValue ? 8 : 8;
    const bottomPad = showXAxis ? 24 : 4;
    const topPad = showCurrentValue ? 8 : 8;

    const chartW = Math.max(50, width - leftPad - rightPad);
    const chartH = Math.max(30, height - topPad - bottomPad);

    // Y 轴范围
    const values = sampled.map((p) => p.value);
    const dataMin = values.length > 0 ? Math.min(...values) : 0;
    const dataMax = values.length > 0 ? Math.max(...values) : 100;
    const ymin = yMin ?? config.min ?? Math.max(0, dataMin - (dataMax - dataMin) * 0.15);
    let ymax = yMax ?? config.max ?? dataMax + (dataMax - dataMin) * 0.15;
    if (ymax === ymin) ymax = ymin + 1;

    // X 轴范围
    const xmin = sampled.length > 0 ? sampled[0].ts : nowMs() - 15 * 60 * 1000;
    const xmax = sampled.length > 0 ? sampled[sampled.length - 1].ts : nowMs();

    return {
      points: sampled,
      yRange: { min: ymin, max: ymax },
      xRange: { min: xmin, max: xmax },
      chartWidth: chartW,
      chartHeight: chartH,
      padding: { left: leftPad, right: rightPad, top: topPad, bottom: bottomPad },
    };
  }, [data, width, height, maxPoints, showYAxis, showXAxis, showCurrentValue, yMin, yMax, config]);

  // 坐标转换
  const xScale = (ts: number) => {
    if (xRange.max === xRange.min) return chartWidth / 2;
    return padding.left + ((ts - xRange.min) / (xRange.max - xRange.min)) * chartWidth;
  };

  const yScale = (value: number) => {
    if (yRange.max === yRange.min) return chartHeight / 2;
    return padding.top + (1 - (value - yRange.min) / (yRange.max - yRange.min)) * chartHeight;
  };

  // 构建 SVG path（贝塞尔平滑曲线）
  const { linePath, areaPath } = useMemo(() => {
    if (points.length < 2) {
      return { linePath: "", areaPath: "" };
    }

    // 起点
    let line = `M ${xScale(points[0].ts)} ${yScale(points[0].value)}`;

    // 贝塞尔曲线（控制点基于相邻点斜率）
    for (let i = 1; i < points.length; i++) {
      const prev = points[i - 1];
      const curr = points[i];
      const x0 = xScale(prev.ts);
      const y0 = yScale(prev.value);
      const x1 = xScale(curr.ts);
      const y1 = yScale(curr.value);

      // 控制点偏移
      const cpx = (x0 + x1) / 2;
      line += ` C ${cpx} ${y0}, ${cpx} ${y1}, ${x1} ${y1}`;
    }

    // 填充区域路径
    const firstX = xScale(points[0].ts);
    const lastX = xScale(points[points.length - 1].ts);
    const bottomY = padding.top + chartHeight;
    const area = `${line} L ${lastX} ${bottomY} L ${firstX} ${bottomY} Z`;

    return { linePath: line, areaPath: area };
  }, [points, chartWidth, chartHeight, padding, xRange, yRange]);

  // Y 轴刻度
  const yTicks = useMemo(() => {
    const ticks: number[] = [];
    const steps = 4;
    for (let i = 0; i <= steps; i++) {
      ticks.push(yRange.min + ((yRange.max - yRange.min) * i) / steps);
    }
    return ticks;
  }, [yRange]);

  // X 轴刻度
  const xTicks = useMemo(() => {
    if (points.length < 2) return [];
    const count = Math.min(5, Math.max(2, Math.floor(chartWidth / 100)));
    const step = (xRange.max - xRange.min) / count;
    const ticks: number[] = [];
    for (let i = 0; i <= count; i++) {
      ticks.push(xRange.min + step * i);
    }
    return ticks;
  }, [xRange, chartWidth, points.length]);

  // 鼠标悬停查找最近点
  const handleMouseMove = (e: React.MouseEvent<SVGSVGElement>) => {
    if (points.length < 2) return;
    const svg = e.currentTarget;
    const rect = svg.getBoundingClientRect();
    const mouseX = e.clientX - rect.left - padding.left;
    const ratio = mouseX / chartWidth;
    const targetTs = xRange.min + ratio * (xRange.max - xRange.min);

    // 二分查找最近点
    let lo = 0, hi = points.length - 1;
    while (lo < hi) {
      const mid = Math.floor((lo + hi) / 2);
      if (points[mid].ts < targetTs) lo = mid + 1;
      else hi = mid;
    }
    setHoverIndex(lo);
  };

  const handleMouseLeave = () => setHoverIndex(null);

  const currentValue = points.length > 0 ? points[points.length - 1].value : 0;
  const hoverPoint = hoverIndex !== null ? points[hoverIndex] : null;

  // 加载骨架屏
  if (loading) {
    return (
      <div ref={containerRef} className={`w-full ${className}`} style={{ height }}>
        <div className="h-full bg-ink-50 rounded-lg animate-pulse flex items-center justify-center">
          <div className="text-ink-300 text-[13px]">加载中...</div>
        </div>
      </div>
    );
  }

  return (
    <div
      ref={containerRef}
      className={`w-full relative ${className}`}
      style={{ height }}
    >
      {/* 当前值标签 */}
      {showCurrentValue && points.length > 0 && (
        <div className="absolute top-2 right-2 z-10 text-right">
          <div className="text-[11px] text-ink-400">{config.displayName}</div>
          <div
            className="text-lg font-bold leading-tight"
            style={{ color: `rgb(${config.color})` }}
          >
            {config.formatValue(currentValue)}
          </div>
        </div>
      )}

      <svg
        width={width}
        height={height}
        className="block"
        onMouseMove={handleMouseMove}
        onMouseLeave={handleMouseLeave}
      >
        {/* 渐变定义 */}
        <defs>
          <linearGradient id={`chart-grad-${metric}`} x1="0%" y1="0%" x2="0%" y2="100%">
            <stop offset="0%" stopColor={`rgba(${config.color}, 0.3)`} />
            <stop offset="100%" stopColor={`rgba(${config.color}, 0.02)`} />
          </linearGradient>
        </defs>

        {/* Y 轴网格线 + 标签 */}
        {showYAxis &&
          yTicks.map((tick, i) => (
            <g key={`yt-${i}`}>
              <line
                x1={padding.left}
                y1={yScale(tick)}
                x2={padding.left + chartWidth}
                y2={yScale(tick)}
                stroke="rgb(241, 245, 249)"
                strokeWidth={1}
              />
              <text
                x={padding.left - 6}
                y={yScale(tick)}
                textAnchor="end"
                dominantBaseline="middle"
                fontSize={10}
                fill="rgb(148, 163, 184)"
              >
                {config.formatValue(tick)}
              </text>
            </g>
          ))}

        {/* X 轴时间标签 */}
        {showXAxis &&
          xTicks.map((ts, i) => (
            <text
              key={`xt-${i}`}
              x={xScale(ts)}
              y={padding.top + chartHeight + 16}
              textAnchor="middle"
              fontSize={10}
              fill="rgb(148, 163, 184)"
            >
              {formatTime(ts)}
            </text>
          ))}

        {/* 填充区域 */}
        {filled && areaPath && (
          <path d={areaPath} fill={`url(#chart-grad-${metric})`} />
        )}

        {/* 曲线 */}
        {linePath && (
          <path
            d={linePath}
            fill="none"
            stroke={`rgb(${config.color})`}
            strokeWidth={2}
            strokeLinejoin="round"
            strokeLinecap="round"
          />
        )}

        {/* 最后一个点高亮 */}
        {points.length > 0 && !hoverPoint && (
          <circle
            cx={xScale(points[points.length - 1].ts)}
            cy={yScale(points[points.length - 1].value)}
            r={4}
            fill="white"
            stroke={`rgb(${config.color})`}
            strokeWidth={2}
          />
        )}

        {/* 悬停指示器 */}
        {hoverPoint && (
          <g>
            {/* 垂直参考线 */}
            <line
              x1={xScale(hoverPoint.ts)}
              y1={padding.top}
              x2={xScale(hoverPoint.ts)}
              y2={padding.top + chartHeight}
              stroke={`rgb(${config.color})`}
              strokeWidth={1}
              strokeDasharray="3 3"
              opacity={0.5}
            />
            {/* 数据点 */}
            <circle
              cx={xScale(hoverPoint.ts)}
              cy={yScale(hoverPoint.value)}
              r={5}
              fill="white"
              stroke={`rgb(${config.color})`}
              strokeWidth={2}
            />
            {/* Tooltip */}
            <g
              transform={`translate(${Math.min(
                xScale(hoverPoint.ts) + 8,
                width - 120,
              )}, ${Math.max(padding.top, yScale(hoverPoint.value) - 30)})`}
            >
              <rect
                x={0}
                y={0}
                width={110}
                height={36}
                rx={6}
                fill="rgba(15, 23, 42, 0.9)"
              />
              <text x={8} y={14} fontSize={10} fill="rgb(148, 163, 184)">
                {formatTime(hoverPoint.ts)}
              </text>
              <text x={8} y={28} fontSize={12} fill="white" fontWeight={600}>
                {config.displayName}: {config.formatValue(hoverPoint.value)}
              </text>
            </g>
          </g>
        )}

        {/* 空状态 */}
        {points.length === 0 && (
          <text
            x={padding.left + chartWidth / 2}
            y={padding.top + chartHeight / 2}
            textAnchor="middle"
            fontSize={12}
            fill="rgb(203, 213, 225)"
          >
            暂无数据
          </text>
        )}
      </svg>
    </div>
  );
}
