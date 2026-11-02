// NPS 反馈管理页（T5-9 后台）
//
// 功能：
//   - NPS 统计概览（NPS 分数、推荐者/中立者/贬损者分布）
//   - 反馈列表（筛选：分数区间 / 有评论）
//   - 价值评价统计
//   - 平均分

import { useState, useEffect } from "react";
import { DataTable, type DataTableColumn } from "@quenching/shared";
import {
  listNpsFeedback,
  getNpsCategory,
  NPS_CATEGORY,
  type NpsFeedback,
  type NpsStats,
} from "@quenching/shared";
import { formatTimeAgo, useServerClock } from "@quenching/shared";

/**
 * NPS 反馈管理页（后台）
 */
export function NpsAdminPage() {
  useServerClock();
  const [feedbacks, setFeedbacks] = useState<NpsFeedback[]>([]);
  const [stats, setStats] = useState<NpsStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [error,setError]=useState("");
  const [total,setTotal]=useState(0);
  const [refresh,setRefresh]=useState(0);

  // 筛选
  const [scoreFilter, setScoreFilter] = useState<string>("");
  const [hasComment, setHasComment] = useState<string>("");

  // 分页
  const [page, setPage] = useState(1);
  const pageSize = 10;

  useEffect(()=>{
    let live=true;setLoading(true);setError("");
    const range=scoreFilter==="promoter"?{score_min:9,score_max:10}:scoreFilter==="passive"?{score_min:7,score_max:8}:scoreFilter==="detractor"?{score_min:0,score_max:6}:{};
    listNpsFeedback({page,page_size:pageSize,...range,has_comment:hasComment===""?undefined:hasComment==="true"})
    .then(result=>{if(live){setFeedbacks(result.items);setTotal(result.total);setStats(result.stats)}})
    .catch(err=>{if(live){setFeedbacks([]);setTotal(0);setStats(null);setError(err instanceof Error?err.message:"反馈加载失败")}})
    .finally(()=>{if(live)setLoading(false)});return()=>{live=false};
  },[page,scoreFilter,hasComment,refresh]);
  const totalPages=Math.ceil(total/pageSize);
  const pageData=feedbacks;

  // 列配置
  const columns: DataTableColumn<NpsFeedback>[] = [
    {
      key: "user",
      title: "用户",
      render: (row) => (
        <div className="flex items-center gap-2">
          <div className="w-8 h-8 rounded-full bg-brand-100 text-brand-600 flex items-center justify-center text-[12px] font-bold">
            {row.user_name.charAt(0)}
          </div>
          <div>
            <div className="text-[13px] font-medium text-ink-700">{row.user_name}</div>
            <div className="text-[11px] text-ink-400">UID: {row.user_id}</div>
          </div>
        </div>
      ),
    },
    {
      key: "training",
      title: "训练内容",
      render: (row) => (
        <div>
          <div className="text-[13px] text-ink-700">{row.project_name}</div>
          <div className="text-[11px] text-ink-400">{row.defect_name}</div>
        </div>
      ),
    },
    {
      key: "score",
      title: "NPS 评分",
      width: "140px",
      render: (row) => {
        const cat = getNpsCategory(row.score);
        const cfg = NPS_CATEGORY[cat];
        return (
          <div className="flex items-center gap-2">
            <div className={`w-10 h-10 rounded-lg flex items-center justify-center font-bold ${cfg.bg} ${cfg.color}`}>
              {row.score}
            </div>
            <div>
              <div className={`text-[12px] font-medium ${cfg.color}`}>{cfg.label}</div>
              <div className="text-[10px] text-ink-400">{cfg.range}</div>
            </div>
          </div>
        );
      },
    },
    {
      key: "value",
      title: "价值评价",
      width: "90px",
      render: (row) => {
        if (!row.value_feedback) return <span className="text-ink-300">—</span>;
        return row.value_feedback === "valuable" ? (
          <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full bg-ok-50 text-ok-600 text-[11px] font-medium">
            👍 有价值
          </span>
        ) : (
          <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full bg-danger-50 text-danger-600 text-[11px] font-medium">
            👎 没价值
          </span>
        );
      },
    },
    {
      key: "comment",
      title: "反馈内容",
      render: (row) => (
        <div className="max-w-xs">
          {row.comment ? (
            <p className="text-[12px] text-ink-600 line-clamp-2 leading-relaxed">
              {row.comment}
            </p>
          ) : (
            <span className="text-ink-300 text-[12px]">无文字反馈</span>
          )}
        </div>
      ),
    },
    {
      key: "time",
      title: "提交时间",
      width: "100px",
      render: (row) => (
        <span className="text-[12px] text-ink-500">
          {formatTimeAgo(new Date(row.created_at).getTime())}
        </span>
      ),
    },
  ];

  return (
    <div className="space-y-6">
      {error&&<div role="alert" className="p-3 text-danger-600 bg-danger-50">{error}<button className="ml-3 underline" onClick={()=>setRefresh(v=>v+1)}>重试</button></div>}
      {/* 统计卡片 */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <div className="bg-white rounded-xl border border-ink-100 p-5">
          <div className="text-[12px] text-ink-500 mb-1">NPS 净推荐值</div>
          <div className={`text-[32px] font-bold ${
            stats && stats.nps_score !== null && stats.nps_score >= 50 ? "text-ok-600"
              : stats && stats.nps_score !== null && stats.nps_score >= 0 ? "text-brand-600"
              : "text-danger-600"
          }`}>
            {stats?.nps_score ?? "暂无数据"}
            <span className="text-[16px] ml-0.5">%</span>
          </div>
          <div className="text-[11px] text-ink-400 mt-1">
            {stats ? `共 ${stats.total_count} 份反馈` : ""}
          </div>
        </div>

        <StatMini
          label="推荐者"
          value={stats?.promoters ?? 0}
          total={stats?.total_count ?? 0}
          color="text-ok-600"
          bg="bg-ok-50"
          icon="😊"
          desc="9-10 分"
        />

        <StatMini
          label="中立者"
          value={stats?.passives ?? 0}
          total={stats?.total_count ?? 0}
          color="text-warn-600"
          bg="bg-warn-50"
          icon="😐"
          desc="7-8 分"
        />

        <StatMini
          label="贬损者"
          value={stats?.detractors ?? 0}
          total={stats?.total_count ?? 0}
          color="text-danger-600"
          bg="bg-danger-50"
          icon="😞"
          desc="0-6 分"
        />
      </div>

      {/* 价值评价 + 平均分 */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <div className="bg-white rounded-xl border border-ink-100 p-5">
          <h3 className="text-[14px] font-bold text-ink-800 mb-4">价值评价分布</h3>
          <div className="space-y-4">
            <ValueBar
              label="认为有价值"
              icon="👍"
              value={stats?.valuable_count ?? 0}
              total={stats?.total_count ?? 1}
              color="bg-ok-500"
              textColor="text-ok-600"
            />
            <ValueBar
              label="认为没价值"
              icon="👎"
              value={stats?.not_valuable_count ?? 0}
              total={stats?.total_count ?? 1}
              color="bg-danger-500"
              textColor="text-danger-600"
            />
          </div>
        </div>

        <div className="bg-white rounded-xl border border-ink-100 p-5">
          <h3 className="text-[14px] font-bold text-ink-800 mb-4">平均评分</h3>
          <div className="flex items-center gap-6">
            <div>
              <div className="text-[48px] font-bold text-brand-600 leading-none">
                {stats?.avg_score?.toFixed(1) ?? "—"}
              </div>
              <div className="text-[12px] text-ink-400 mt-1">满分 10 分</div>
            </div>
            <div className="flex-1 space-y-1.5">
              {[10, 9, 8, 7, 6, 5].map((s) => {
                const count = feedbacks.filter((f) => f.score === s).length;
                const pct = feedbacks.length > 0 ? (count / feedbacks.length) * 100 : 0;
                return (
                  <div key={s} className="flex items-center gap-2">
                    <span className="w-6 text-[10px] text-ink-400 text-right font-mono">{s}</span>
                    <div className="flex-1 h-3 bg-ink-100 rounded-full overflow-hidden">
                      <div
                        className={`h-full rounded-full ${
                          s >= 9 ? "bg-ok-500" : s >= 7 ? "bg-warn-500" : "bg-danger-500"
                        }`}
                        style={{ width: `${pct}%` }}
                      />
                    </div>
                    <span className="w-6 text-[10px] text-ink-400 font-mono">{count}</span>
                  </div>
                );
              })}
            </div>
          </div>
        </div>
      </div>

      {/* 反馈列表 */}
      <DataTable
        title="用户反馈"
        description={`共 ${total} 条反馈记录`}
        columns={columns}
        data={pageData}
        rowKey="id"
        loading={loading}
        currentPage={page}
        totalPages={totalPages}
        totalItems={total}
        pageSize={pageSize}
        onPageChange={setPage}
        filter={
          <div className="flex flex-wrap items-center gap-3">
            <div className="flex flex-col gap-1.5">
              <label className="text-xs text-ink-500 font-medium">评分类型</label>
              <select
                value={scoreFilter}
                onChange={(e) => { setScoreFilter(e.target.value); setPage(1); }}
                className="h-9 min-w-[130px] px-3 rounded-lg border border-ink-200 text-[13px] text-ink-700 bg-white focus:outline-none focus:ring-2 focus:ring-brand-500/20 focus:border-brand-500"
              >
                <option value="">全部评分</option>
                <option value="promoter">推荐者（9-10 分）</option>
                <option value="passive">中立者（7-8 分）</option>
                <option value="detractor">贬损者（0-6 分）</option>
              </select>
            </div>

            <div className="flex flex-col gap-1.5">
              <label className="text-xs text-ink-500 font-medium">文字反馈</label>
              <select
                value={hasComment}
                onChange={(e) => { setHasComment(e.target.value); setPage(1); }}
                className="h-9 min-w-[120px] px-3 rounded-lg border border-ink-200 text-[13px] text-ink-700 bg-white focus:outline-none focus:ring-2 focus:ring-brand-500/20 focus:border-brand-500"
              >
                <option value="">全部</option>
                <option value="true">有文字反馈</option>
                <option value="false">无文字反馈</option>
              </select>
            </div>

            <div className="flex gap-2 self-end">
              <button
                onClick={() => { setScoreFilter(""); setHasComment(""); setPage(1); }}
                className="h-9 px-5 rounded-lg border border-ink-200 text-ink-600 text-[13px] font-medium hover:bg-ink-50 transition-colors"
              >
                重置
              </button>
            </div>
          </div>
        }
      />
    </div>
  );
}

/* ===== 子组件 ===== */

function StatMini({
  label,
  value,
  total,
  color,
  bg,
  icon,
  desc,
}: {
  label: string;
  value: number;
  total: number;
  color: string;
  bg: string;
  icon: string;
  desc: string;
}) {
  void bg; // 预留，当前使用白底
  const pct = total > 0 ? Math.round((value / total) * 100) : 0;
  return (
    <div className="bg-white rounded-xl border border-ink-100 p-5">
      <div className="flex items-center justify-between mb-2">
        <span className="text-[12px] text-ink-500">{label}</span>
        <span className="text-2xl">{icon}</span>
      </div>
      <div className={`text-[28px] font-bold ${color}`}>
        {value}
        <span className="text-[14px] font-normal text-ink-400 ml-1">({pct}%)</span>
      </div>
      <div className="text-[11px] text-ink-400 mt-1">{desc}</div>
    </div>
  );
}

function ValueBar({
  label,
  icon,
  value,
  total,
  color,
  textColor,
}: {
  label: string;
  icon: string;
  value: number;
  total: number;
  color: string;
  textColor: string;
}) {
  const pct = total > 0 ? (value / total) * 100 : 0;
  return (
    <div>
      <div className="flex items-center justify-between mb-1.5">
        <span className="text-[13px] text-ink-700 flex items-center gap-1.5">
          <span>{icon}</span> {label}
        </span>
        <span className={`text-[13px] font-bold ${textColor}`}>
          {value} 人 ({pct.toFixed(0)}%)
        </span>
      </div>
      <div className="h-2.5 bg-ink-100 rounded-full overflow-hidden">
        <div
          className={`h-full ${color} rounded-full transition-all duration-500`}
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  );
}
