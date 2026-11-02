import { useMemo } from "react";
import type { RadarData } from "../services/radar";

interface SkillRadarProps {
  /** 主数据（实线填充） */
  data: RadarData;
  /** 对比数据（虚线，可选） */
  compareData?: RadarData;
  /** 主数据颜色（十六进制，不带 #） */
  color?: string;
  /** 对比数据颜色 */
  compareColor?: string;
  /** 尺寸（px，正方形） */
  size?: number;
  /** 网格层数 */
  levels?: number;
  /** 是否显示维度标签 */
  showLabels?: boolean;
  /** 标签字体大小 */
  labelSize?: number;
}

/**
 * 能力雷达图组件（SVG 实现）
 *
 * - 纯 SVG 绘制，无外部依赖
 * - 支持主数据 + 对比数据（本次 vs 历史平均）
 * - 支持自定义尺寸、颜色、网格层数
 * - 自适应维度数量（3-8 维都能正确渲染）
 * - 渐变填充 + 数据点高亮
 */
export function SkillRadar({
  data,
  compareData,
  color = "139, 92, 246", // brand-500
  compareColor = "148, 163, 184", // ink-300
  size = 280,
  levels = 5,
  showLabels = true,
  labelSize = 12,
}: SkillRadarProps) {
  const dims = data.dimensions.length;
  // 四周内边距：为轴标签留出完整空间。左右标签最长 4 个汉字（约 4 倍字号），
  // 需保证右对齐/左对齐文字不超出 viewBox；上下按 2 倍字号即可。
  const padX = labelSize * 4 + 6;
  const padY = labelSize * 2 + 4;
  const innerW = size;
  const innerH = size;
  const svgW = innerW + padX * 2;
  const svgH = innerH + padY * 2;
  // 几何中心平移到内边距之后；半径仍按传入 size 计算，雷达图形本身不变形。
  const center = size / 2 + padX;
  const centerY = size / 2 + padY;
  const radius = size * 0.35; // 雷达半径
  const angleStep = (Math.PI * 2) / dims;
  const startAngle = -Math.PI / 2; // 从顶部开始

  // 计算多边形顶点坐标
  const points = useMemo(() => {
    return data.values.map((val, i) => {
      const angle = startAngle + angleStep * i;
      const r = (Math.max(0, Math.min(100, val)) / 100) * radius;
      return {
        x: center + r * Math.cos(angle),
        y: centerY + r * Math.sin(angle),
      };
    });
  }, [data, center, centerY, radius, angleStep, startAngle]);

  const comparePoints = useMemo(() => {
    if (!compareData) return [];
    return compareData.values.map((val, i) => {
      const angle = startAngle + angleStep * i;
      const r = (Math.max(0, Math.min(100, val)) / 100) * radius;
      return {
        x: center + r * Math.cos(angle),
        y: centerY + r * Math.sin(angle),
      };
    });
  }, [compareData, center, centerY, radius, angleStep, startAngle]);

  // 网格层（levels 层多边形）
  const gridPolygons = useMemo(() => {
    const result = [];
    for (let i = 1; i <= levels; i++) {
      const r = (radius * i) / levels;
      const pts = [];
      for (let j = 0; j < dims; j++) {
        const angle = startAngle + angleStep * j;
        pts.push(`${center + r * Math.cos(angle)},${centerY + r * Math.sin(angle)}`);
      }
      result.push(pts.join(" "));
    }
    return result;
  }, [center, centerY, radius, levels, dims, angleStep, startAngle]);

  // 轴线
  const axes = useMemo(() => {
    const result = [];
    for (let i = 0; i < dims; i++) {
      const angle = startAngle + angleStep * i;
      result.push({
        x1: center,
        y1: centerY,
        x2: center + radius * Math.cos(angle),
        y2: centerY + radius * Math.sin(angle),
      });
    }
    return result;
  }, [center, centerY, radius, dims, angleStep, startAngle]);

  // 维度标签位置
  const labelPositions = useMemo(() => {
    const labelR = radius + labelSize + 6;
    return data.dimensions.map((dim, i) => {
      const angle = startAngle + angleStep * i;
      const x = center + labelR * Math.cos(angle);
      const y = centerY + labelR * Math.sin(angle);
      // 文本对齐：左侧右对齐，右侧左对齐，上下居中
      let textAnchor: "start" | "middle" | "end" = "middle";
      const cos = Math.cos(angle);
      if (cos > 0.2) textAnchor = "start";
      else if (cos < -0.2) textAnchor = "end";
      return { dim, x, y, textAnchor };
    });
  }, [data, center, centerY, radius, angleStep, startAngle, labelSize]);

  const polygonStr = points.map((p) => `${p.x},${p.y}`).join(" ");
  const comparePolygonStr = comparePoints.map((p) => `${p.x},${p.y}`).join(" ");

  // 渐变 id（唯一化，避免页面内多个雷达互相影响）
  const gradientId = `radar-grad-${color.replace(/,/g, "-")}`;

  return (
    <svg
      width={svgW}
      height={svgH}
      viewBox={`0 0 ${svgW} ${svgH}`}
      className="select-none max-w-full h-auto"
    >
      {/* 渐变定义 */}
      <defs>
        <radialGradient id={gradientId} cx="50%" cy="50%" r="50%">
          <stop offset="0%" stopColor={`rgba(${color}, 0.4)`} />
          <stop offset="100%" stopColor={`rgba(${color}, 0.15)`} />
        </radialGradient>
      </defs>

      {/* 网格层 */}
      {gridPolygons.map((pts, i) => (
        <polygon
          key={`grid-${i}`}
          points={pts}
          fill="none"
          stroke="rgb(226, 232, 240)"
          strokeWidth={1}
        />
      ))}

      {/* 轴线 */}
      {axes.map((ax, i) => (
        <line
          key={`axis-${i}`}
          x1={ax.x1}
          y1={ax.y1}
          x2={ax.x2}
          y2={ax.y2}
          stroke="rgb(226, 232, 240)"
          strokeWidth={1}
        />
      ))}

      {/* 对比数据（虚线，在主数据下层） */}
      {compareData && comparePolygonStr && (
        <>
          <polygon
            points={comparePolygonStr}
            fill={`rgba(${compareColor}, 0.1)`}
            stroke={`rgb(${compareColor})`}
            strokeWidth={1.5}
            strokeDasharray="4 3"
          />
          {comparePoints.map((p, i) => (
            <circle
              key={`compare-pt-${i}`}
              cx={p.x}
              cy={p.y}
              r={3}
              fill="white"
              stroke={`rgb(${compareColor})`}
              strokeWidth={1.5}
            />
          ))}
        </>
      )}

      {/* 主数据多边形 */}
      <polygon
        points={polygonStr}
        fill={`url(#${gradientId})`}
        stroke={`rgb(${color})`}
        strokeWidth={2}
        strokeLinejoin="round"
      />

      {/* 主数据顶点 */}
      {points.map((p, i) => (
        <circle
          key={`pt-${i}`}
          cx={p.x}
          cy={p.y}
          r={4}
          fill="white"
          stroke={`rgb(${color})`}
          strokeWidth={2}
        />
      ))}

      {/* 维度标签 */}
      {showLabels &&
        labelPositions.map((lp, i) => (
          <text
            key={`label-${i}`}
            x={lp.x}
            y={lp.y}
            textAnchor={lp.textAnchor}
            dominantBaseline="middle"
            fontSize={labelSize}
            fill="rgb(71, 85, 105)"
            fontWeight={500}
          >
            {lp.dim}
          </text>
        ))}
    </svg>
  );
}
