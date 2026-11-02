import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { fetchSessions } from "../services/profile";
import { tokenStore, nowMs, formatDate, useServerClock } from "@quenching/shared";

/**
 * 实训环境入口页（/training）
 *
 * 真正的桌面化工作台在 /training/:id，需要 session/sandbox 参数，
 * 因此本页负责：
 * - 介绍实训环境能力，引导用户从项目大厅选择项目开启实训
 * - 登录用户展示最近实训记录，可进入训练历史查看/继续
 */
interface RecentRow {
  id: number;
  projectName: string;
  difficulty: string;
  success: boolean | null;
  startTime: string | null;
}

const DIFFICULTY_LABEL: Record<string, string> = {
  easy: "简单",
  medium: "中等",
  hard: "困难",
};

function formatTime(iso: string | null): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  const now = nowMs();
  const diff = now - d.getTime();
  const min = Math.floor(diff / 60000);
  if (min < 1) return "刚刚";
  if (min < 60) return `${min} 分钟前`;
  const hour = Math.floor(min / 60);
  if (hour < 24) return `${hour} 小时前`;
  const day = Math.floor(hour / 24);
  if (day < 30) return `${day} 天前`;
  return formatDate(d.getTime());
}

export function TrainingEntryPage() {
  useServerClock();
  const navigate = useNavigate();
  const loggedIn = Boolean(tokenStore.get());

  const [recent, setRecent] = useState<RecentRow[]>([]);
  const [loadingRecent, setLoadingRecent] = useState(false);

  useEffect(() => {
    if (!loggedIn) {
      setRecent([]);
      return;
    }
    setLoadingRecent(true);
    fetchSessions({ page: 1, pageSize: 3 })
      .then((res) => setRecent((res.list ?? []) as RecentRow[]))
      .catch(() => setRecent([]))
      .finally(() => setLoadingRecent(false));
  }, [loggedIn]);

  return (
    <div className="page-container py-10">
      {/* Hero */}
      <div className="rounded-3xl overflow-hidden border border-ink-100 bg-gradient-to-br from-ink-900 via-ink-800 to-brand-900 relative">
        <div className="absolute -top-20 -right-20 w-72 h-72 rounded-full bg-brand-500/20 blur-3xl" />
        <div className="absolute -bottom-24 -left-10 w-72 h-72 rounded-full bg-brand-400/10 blur-3xl" />
        <div className="relative px-8 md:px-12 py-14 md:py-16">
          <span className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full bg-white/10 border border-white/15 text-white/80 text-[12px] font-medium mb-5">
            <span className="w-1.5 h-1.5 rounded-full bg-emerald-400" />
            全真沙箱 · 故障实战工作台
          </span>
          <h1 className="text-3xl md:text-4xl font-bold text-white mb-4 leading-tight">
            在隔离沙箱里，<br className="md:hidden" />像真实工程师一样排障
          </h1>
          <p className="text-[15px] text-white/70 leading-relaxed max-w-xl mb-8">
            每个实训项目都会为你启动一套独立运行的环境：真实服务器、实时监控、告警与日志。
            注入故障后，结合 AI 助手定位根因、修复恢复，全过程沉淀为你的能力画像。
          </p>
          <div className="flex flex-wrap items-center gap-3">
            <button
              type="button"
              onClick={() => navigate("/projects")}
              className="h-11 px-7 rounded-xl bg-brand-500 hover:bg-brand-400 text-white text-[14px] font-semibold transition-colors"
            >
              选择项目，开始实训
            </button>
            {!loggedIn && (
              <button
                type="button"
                onClick={() => navigate("/login", { state: { from: "/training" } })}
                className="h-11 px-7 rounded-xl border border-white/20 text-white/90 hover:bg-white/10 text-[14px] font-medium transition-colors"
              >
                登录查看我的实训
              </button>
            )}
          </div>
        </div>
      </div>

      {/* 能力点 */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 mt-6">
        {CAPABILITIES.map((c) => (
          <div
            key={c.title}
            className="bg-white rounded-2xl border border-ink-100 p-5"
          >
            <div className="w-10 h-10 rounded-xl bg-brand-50 flex items-center justify-center text-xl mb-3">
              {c.icon}
            </div>
            <h3 className="text-[14px] font-bold text-ink-800 mb-1">{c.title}</h3>
            <p className="text-[12px] text-ink-400 leading-relaxed">{c.desc}</p>
          </div>
        ))}
      </div>

      {/* 最近实训 */}
      {loggedIn && (
        <div className="mt-10">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-[15px] font-bold text-ink-800 flex items-center gap-2">
              <span className="w-1 h-5 bg-brand-500 rounded-full" />
              最近实训
            </h2>
            <Link
              to="/history"
              className="text-[13px] font-medium text-brand-500 hover:text-brand-600"
            >
              查看全部
            </Link>
          </div>

          <div className="bg-white rounded-2xl border border-ink-100 overflow-hidden">
            {loadingRecent && (
              <div className="p-8 text-center text-[13px] text-ink-400">加载中…</div>
            )}

            {!loadingRecent && recent.length === 0 && (
              <div className="px-6 py-12 text-center">
                <p className="text-[14px] text-ink-600 font-medium mb-1">还没有实训记录</p>
                <p className="text-[12px] text-ink-400 mb-5">从项目大厅选一个项目，开启第一次实战</p>
                <Link
                  to="/projects"
                  className="inline-flex h-9 px-5 rounded-lg bg-brand-500 hover:bg-brand-600 text-white text-[13px] font-medium items-center transition-colors"
                >
                  去项目大厅
                </Link>
              </div>
            )}

            {!loadingRecent && recent.length > 0 && (
              <ul className="divide-y divide-ink-50">
                {recent.map((row) => (
                  <li key={row.id}>
                    <Link
                      to="/history"
                      className="flex items-center gap-4 px-6 py-4 hover:bg-ink-50/60 transition-colors"
                    >
                      <div
                        className={`w-9 h-9 rounded-lg flex items-center justify-center flex-none text-[13px] font-bold ${
                          row.success === true
                            ? "bg-emerald-50 text-emerald-600"
                            : row.success === false
                              ? "bg-danger/10 text-danger"
                              : "bg-ink-50 text-ink-400"
                        }`}
                      >
                        {row.success === true ? "✓" : row.success === false ? "✕" : "…"}
                      </div>
                      <div className="flex-1 min-w-0">
                        <div className="text-[14px] font-semibold text-ink-800 truncate">
                          {row.projectName || "实训项目"}
                        </div>
                        <div className="text-[12px] text-ink-400 mt-0.5">
                          {DIFFICULTY_LABEL[row.difficulty] ?? row.difficulty ?? "中等"}
                        </div>
                      </div>
                      <span className="text-[12px] text-ink-400 flex-none">
                        {formatTime(row.startTime)}
                      </span>
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

const CAPABILITIES = [
  {
    icon: "🖥️",
    title: "独立沙箱环境",
    desc: "每个项目启动专属隔离环境，真实服务与网络，互不干扰。",
  },
  {
    icon: "📊",
    title: "实时监控指标",
    desc: "CPU、内存、错误率等指标实时呈现，异常一眼可见。",
  },
  {
    icon: "🚨",
    title: "告警与日志",
    desc: "故障触发告警，日志随查随筛，支撑根因定位。",
  },
  {
    icon: "🤖",
    title: "人机协作排障",
    desc: "借助 AI 助手分析研判，复盘 AI 协作判断力。",
  },
];
