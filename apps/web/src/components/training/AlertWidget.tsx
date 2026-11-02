// AlertWidget.tsx 桌面右上告警小部件（桌面化第一批）。
//
// 铃铛 + firing 红点计数；可展开看最近告警，点击单条跳对应服务器窗口。
// 数据由 TrainingPage 统一轮询后传入。
import { useState } from "react";
import {WorkbenchIcon} from "./WorkbenchIcon";
import type { AlertItem } from "../../services/alerts";

export interface AlertWidgetProps {
  alerts: AlertItem[];
  onAlertClick?: (alert: AlertItem) => void;
}

export function AlertWidget({ alerts, onAlertClick }: AlertWidgetProps) {
  const [open, setOpen] = useState(false);
  const firing = alerts.filter((a) => a.status === "firing").length;
  const recent = alerts.slice(0, 6);

  return (
    <div className="w-full bg-white rounded-xl border border-ink-200 overflow-hidden">
      <button
        onClick={() => setOpen(!open)}
        className="w-full flex items-center gap-2 px-3 py-2.5 hover:bg-ink-50 transition-colors"
      >
        <WorkbenchIcon name="alert" size={18}/>
        <span className="text-[13px] font-semibold text-ink-700 flex-1 text-left">告警中心</span>
        {firing > 0 && (
          <span className="text-xs font-bold bg-red-600 text-white rounded-full px-1.5 min-w-[18px] text-center">
            {firing}
          </span>
        )}
        <span className={`text-[9px] text-ink-400 transition-transform ${open ? "rotate-180" : ""}`}>▾</span>
      </button>

      {open && (
        <div className="border-t border-ink-100 max-h-[220px] overflow-y-auto">
          {recent.length === 0 ? (
            <div className="px-3 py-4 text-center text-[13px] text-ink-400">暂无告警</div>
          ) : (
            recent.map((a) => (
              <button
                key={a.id}
                onClick={() => onAlertClick?.(a)}
                className="w-full flex items-start gap-2 px-3 py-2 text-left hover:bg-brand-50 transition-colors border-b border-ink-50 last:border-0"
              >
                <span className="mt-0.5 text-[13px] flex-shrink-0 leading-none">
                  {a.source === "frontend" ? "🖥️" : "⚙️"}
                </span>
                <span className="flex-1">
                  <span className="block text-[13px] text-ink-700 leading-snug">{a.message}</span>
                  <span className="block text-xs text-ink-400 mt-0.5">
                    {a.source === "frontend" ? "页面观测" : "后端"}
                    {a.step ? ` · ${a.step}` : ""}
                    {a.service ? ` · ${a.service}` : ""}
                  </span>
                </span>
                <span
                  className={`mt-1 w-2 h-2 rounded-full flex-shrink-0 ${
                    a.status === "firing" ? "bg-red-600" : "bg-ink-300"
                  }`}
                />
              </button>
            ))
          )}
        </div>
      )}
    </div>
  );
}
