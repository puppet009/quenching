// MonitorWidget.tsx 桌面右上常驻监控小部件（桌面化第一批）。
//
// 迷你曲线 + 当前值；点击打开完整“实时监控”窗口。数据由 TrainingPage
// 统一轮询后传入，本组件不重复发请求。
import { RealtimeChart, type DataPoint } from "@quenching/shared";
import {WorkbenchIcon} from "./WorkbenchIcon";

export interface MonitorWidgetProps {
  label: string;
  color: string;
  points: DataPoint[];
  onClick?: () => void;
}

export function MonitorWidget({ label, color, points, onClick }: MonitorWidgetProps) {
  const current = points.length > 0 ? points[points.length - 1].value : null;
  return (
    <button
      onClick={onClick}
      className="w-full text-left bg-white rounded-xl border border-ink-200 p-4 hover:border-brand-300 transition-colors"
      title="打开实时监控"
    >
      <div className="flex items-center justify-between mb-1">
        <span className="text-xs font-semibold text-ink-700 flex items-center gap-2"><WorkbenchIcon name="metrics" size={17}/>{label}</span>
        <span className="text-lg font-semibold" style={{ color }}>
          {current===null?"—":Math.round(current)}
          <span className="text-xs font-medium text-ink-500 ml-0.5">%</span>
        </span>
      </div>
      <div className="h-[64px] mt-3">
        {points.length > 0 ? (
          <RealtimeChart data={points} metric="cpu_usage" height={64} filled showCurrentValue={false} showYAxis={false} showXAxis={false} />
        ) : (
          <div className="h-full flex items-center justify-center text-[10px] text-ink-300">暂无数据</div>
        )}
      </div>
    </button>
  );
}
