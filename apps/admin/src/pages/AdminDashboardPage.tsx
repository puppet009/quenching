// Admin 数据概览页（T4-7）
//
// 后台首页，展示核心指标卡片 + 趋势图表占位。
// 统计由网关聚合领域服务，缺失数据保持不可用。

import { useState, useEffect } from "react";
import { RealtimeChart } from "@quenching/shared";
import {dashboard,errorMessage,type Dashboard} from "../services/data";

/**
 * 管理后台首页 - 数据概览
 */
export function AdminDashboardPage() {
  const [stats,setStats]=useState<Dashboard["stats"]>({});
  const [trendData,setTrendData]=useState<{ts:number;value:number}[]>([]);
  const [loading,setLoading]=useState(true),[error,setError]=useState(""),[refresh,setRefresh]=useState(0);
  useEffect(()=>{let live=true;setLoading(true);setError("");dashboard().then(result=>{if(live){setStats(result.stats);setTrendData(Array.isArray(result.stats.trend)?result.stats.trend:[]);if(result.unavailable.length)setError(`部分统计不可用：${result.unavailable.join("、")}`)}}).catch(e=>{if(live){setStats({});setTrendData([]);setError(errorMessage(e))}}).finally(()=>{if(live)setLoading(false)});return()=>{live=false}},[refresh]);
  const value=(key:string)=>stats[key]===null?"暂无数据":typeof stats[key]==="number"?(stats[key] as number).toLocaleString(undefined,{maximumFractionDigits:1}):"不可用";
  const statCards=[
    {label:"总用户数",key:"totalUsers",icon:"👥",color:"bg-brand-50 text-brand-600"},
    {label:"数据库项目",key:"totalProjects",icon:"📦",color:"bg-ok-50 text-ok-600"},
    {label:"目录模板",key:"totalTemplates",icon:"📁",color:"bg-ok-50 text-ok-600"},
    {label:"故障配置",key:"totalDefects",icon:"🐛",color:"bg-warn-50 text-warn-600"},
    {label:"活跃沙箱",key:"activeSandboxes",icon:"🖥️",color:"bg-ai/10 text-ai"},
    {label:"今日创建训练",key:"todayTrainings",icon:"⚡",color:"bg-danger-50 text-danger-600"},
    {label:"累计训练",key:"totalTrainings",icon:"📊",color:"bg-purple-50 text-purple-600"},
    {label:"已评分训练平均分",key:"avgScore",icon:"🎯",color:"bg-ink-50 text-ink-600"},
    {label:"NPS",key:"npsScore",icon:"⭐",color:"bg-amber-50 text-amber-600"},
  ].map(card=>({...card,value:value(card.key),trend:""}));
  const sb=stats.sandboxStats as Record<string,unknown>|undefined;

  return (
    <div className="space-y-6">
      {error&&<div role="alert" className="p-3 bg-danger-50 text-danger-600">{error}<button className="ml-3 underline" onClick={()=>setRefresh(v=>v+1)}>刷新</button></div>}
      {/* 欢迎条 */}
      <div className="bg-gradient-to-r from-brand-500 to-brand-600 rounded-xl p-5 text-white">
        <div className="flex items-center justify-between">
          <div>
            <h2 className="text-[18px] font-bold mb-1">欢迎回来，管理员 👋</h2>
            <p className="text-[13px] text-white/80">
              今日创建训练 {value("todayTrainings")} 次，活跃沙箱 {value("activeSandboxes")} 个
            </p>
          </div>
          <div className="hidden sm:block text-right">
            <div className="text-[11px] text-white/60 uppercase tracking-wider">系统状态</div>
            <div className="text-[13px] font-medium flex items-center gap-2 justify-end">
              <span className="w-2 h-2 rounded-full bg-ok-400 animate-pulse" />
              {error?"部分数据不可用":"统计已更新"}
            </div>
          </div>
        </div>
      </div>

      {/* 统计卡片 */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        {loading
          ? Array(8).fill(0).map((_, i) => (
              <div key={i} className="bg-white rounded-xl border border-ink-100 p-4 animate-pulse">
                <div className="h-4 w-20 bg-ink-100 rounded mb-3" />
                <div className="h-7 w-24 bg-ink-100 rounded" />
              </div>
            ))
          : statCards.map((card) => (
              <div
                key={card.label}
                className="bg-white rounded-xl border border-ink-100 p-4 hover:shadow-md transition-shadow"
              >
                <div className="flex items-start justify-between mb-2">
                  <span className={`w-9 h-9 rounded-lg flex items-center justify-center text-lg ${card.color}`}>
                    {card.icon}
                  </span>
                  <span className="text-[11px] text-ok-500 font-medium">{card.trend}</span>
                </div>
                <div className="text-[22px] font-bold text-ink-800">{card.value}</div>
                <div className="text-[12px] text-ink-500 mt-0.5">{card.label}</div>
              </div>
            ))}
      </div>

      {/* 图表区 */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <div className="bg-white rounded-xl border border-ink-100 p-5">
          <div className="flex items-center justify-between mb-4">
            <div>
              <h3 className="text-[15px] font-bold text-ink-800">训练趋势</h3>
              <p className="text-[12px] text-ink-500">近 7 天训练次数</p>
            </div>
            <select className="h-8 px-3 text-[12px] border border-ink-200 rounded-lg text-ink-600 focus:outline-none">
              <option>近 7 天</option>


            </select>
          </div>
          {loading ? (
            <div className="h-56 flex items-center justify-center text-ink-400 text-[13px]">
              加载中...
            </div>
          ) : !Array.isArray(stats.trend) ? <div className="h-56 flex items-center justify-center text-ink-400">趋势不可用</div> : (
            <RealtimeChart
              data={trendData}
              metric="qps"
              height={220}
              filled
            />
          )}
        </div>

        <div className="bg-white rounded-xl border border-ink-100 p-5">
          <div className="flex items-center justify-between mb-4">
            <div>
              <h3 className="text-[15px] font-bold text-ink-800">沙箱资源使用</h3>
              <p className="text-[12px] text-ink-500">实时 CPU / 内存占用</p>
            </div>
            <span className="flex items-center gap-1.5 text-[11px] text-ok-600">
              <span className="w-2 h-2 rounded-full bg-ok-500 animate-pulse" />
              实时
            </span>
          </div>
          {loading ? (
            <div className="h-56 flex items-center justify-center text-ink-400 text-[13px]">
              加载中...
            </div>
          ) : (
            <div className="space-y-4">
              <p className="text-sm text-ink-500">各沙箱实际 CPU、内存及采样时间见沙箱监控。未提供磁盘指标。</p>
              <div className="pt-4 border-t border-ink-50 grid grid-cols-3 gap-4 text-center">
                <div>
                  <div className="text-[18px] font-bold text-ink-800">{value("activeSandboxes")}</div>
                  <div className="text-[11px] text-ink-500">在线沙箱</div>
                </div>
                <div>
                  <div className="text-[18px] font-bold text-ok-600">{typeof sb?.total==="number"?sb.total:"不可用"}</div>
                  <div className="text-[11px] text-ink-500">沙箱总数</div>
                </div>
                <div>
                  <div className="text-[18px] font-bold text-warn-600">{typeof sb?.unresolved_anomalies==="number"?sb.unresolved_anomalies:"不可用"}</div>
                  <div className="text-[11px] text-ink-500">告警数</div>
                </div>
              </div>
            </div>
          )}
        </div>
      </div>

      {/* 快捷操作 */}
      <div className="bg-white rounded-xl border border-ink-100 p-5">
        <h3 className="text-[15px] font-bold text-ink-800 mb-4">快捷操作</h3>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
          <QuickAction icon="➕" label="新建项目" to="/admin/projects" />
          <QuickAction icon="🐛" label="故障剧本" to="/admin/defects" />
          <QuickAction icon="🎭" label="坑点配置" to="/admin/pitfalls" />
          <QuickAction icon="🖥️" label="沙箱监控" to="/admin/sandbox" />
        </div>
      </div>
    </div>
  );
}

function QuickAction({ icon, label, to }: { icon: string; label: string; to: string }) {
  return (
    <a
      href={to}
      className="flex flex-col items-center gap-2 p-4 rounded-xl border border-ink-100 hover:border-brand-200 hover:bg-brand-50/30 transition-all"
    >
      <span className="text-2xl">{icon}</span>
      <span className="text-[12px] font-medium text-ink-700">{label}</span>
    </a>
  );
}
