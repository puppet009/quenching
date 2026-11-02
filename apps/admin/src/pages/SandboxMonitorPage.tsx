import {nowMs, useServerClock} from "@quenching/shared";
// 沙箱监控页（T4-12）
//
// 功能：
//   - 沙箱实例列表（状态、资源占用、用户信息）
//   - 资源占用可视化（CPU/内存/磁盘进度条）
//   - 手动释放按钮（单台/批量）
//   - 异常告警展示（高资源/超时/错误状态）
//   - 总览统计卡片

import { useState, useEffect } from "react";
import { DataTable, type DataTableColumn } from "@quenching/shared";
import * as api from "../services/data";

type SandboxStatus = "queued" | "creating" | "running" | "starting" | "stopping" | "error" | "idle" | "released";

interface SandboxInstance {
  id: string;
  name: string;
  project_id: number;
  project_name: string;
  defect_name: string;
  user_id: number;
  user_name: string;
  status: SandboxStatus;
  cpu_usage: number | null;
  memory_usage: number | null;
  disk_usage: number | null;
  pod_name: string;
  namespace: string;
  node_name: string;
  created_at: number;
  last_activity_at: number;
  duration_seconds: number | null;
  alerts: SandboxAlert[];
}

interface SandboxAlert {
  id: string;
  level: "warning" | "critical" | "info";
  type: string;
  message: string;
  created_at: number;
}

function mapSandbox(row:api.Sandbox,detail:api.SandboxDetail|undefined,alerts:api.Anomaly[]):SandboxInstance {
  const statuses:Record<string,SandboxStatus>={queued:"queued",creating:"creating",deploying:"starting",ready:"idle",running:"running",releasing:"stopping",released:"released",failed:"error"};
  return {id:String(row.session_id),name:row.namespace,project_id:row.project_id,project_name:row.project_id?`项目 #${row.project_id}`:"文件模板沙箱",defect_name:"",user_id:row.user_id,user_name:`UID ${row.user_id}`,status:statuses[row.status]??"error",cpu_usage:detail?.metrics?.cpu_percent??null,memory_usage:detail?.metrics?.memory_percent??null,disk_usage:null,pod_name:"未提供",namespace:row.namespace,node_name:"未提供",created_at:Date.parse(row.created_at),last_activity_at:Date.parse(row.last_activity_at??row.created_at),duration_seconds:detail?.runtime_seconds??null,alerts:alerts.filter(a=>a.session_id===row.session_id).map(a=>({id:String(a.id),level:a.level==="error"?"critical":"warning",type:"server",message:a.message,created_at:Date.parse(a.detected_at)}))};
}

const STATUS_CONFIG: Record<SandboxStatus, { label: string; color: string; dot: string; bg: string }> = {
 queued: {label:"等待资源",color:"text-amber-700",dot:"bg-amber-500",bg:"bg-amber-50"},
  creating:{label:"创建中",color:"text-warn-600",dot:"bg-warn-500",bg:"bg-warn-50"},
  running: { label: "运行中", color: "text-ok-600", dot: "bg-ok-500", bg: "bg-ok-50" },
  starting: { label: "部署中", color: "text-warn-600", dot: "bg-warn-500 animate-pulse", bg: "bg-warn-50" },
  stopping: { label: "停止中", color: "text-ink-500", dot: "bg-ink-400 animate-pulse", bg: "bg-ink-50" },
  error: { label: "异常", color: "text-danger-600", dot: "bg-danger-500", bg: "bg-danger-50" },
  released:{label:"已释放",color:"text-ink-400",dot:"bg-ink-300",bg:"bg-ink-50"},
  idle: { label: "就绪", color: "text-ink-500", dot: "bg-ink-400", bg: "bg-ink-50" },
};

/**
 * 沙箱监控页（T4-12）
 */
export function SandboxMonitorPage() {
  useServerClock();
  const [sandboxes, setSandboxes] = useState<SandboxInstance[]>([]);
  const [loading, setLoading] = useState(true);
  const [error,setError]=useState("");
  const [message,setMessage]=useState("");
  const [total,setTotal]=useState(0);
  const [serverStats,setServerStats]=useState<api.SandboxStats|null>(null);
  const [refresh,setRefresh]=useState(0);
  const [statusFilter, setStatusFilter] = useState<string>("");
  const [page, setPage] = useState(1);
  const pageSize = 10;
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());

  useEffect(()=>{
    const controller=new AbortController();setLoading(true);setError("");
    const backend:Record<string,string>={queued:"queued",creating:"creating",running:"running",starting:"deploying",stopping:"releasing",idle:"ready",released:"released",error:"failed"};
    Promise.all([api.sandboxes({page,page_size:pageSize,status:backend[statusFilter]},controller.signal),api.sandboxStats(),api.anomalies({page:1,page_size:100,resolved:false},controller.signal)])
    .then(async([list,stats,alerts])=>{
      const details=await Promise.allSettled(list.items.map(sb=>api.sandboxDetail(sb.session_id,controller.signal)));
      if(controller.signal.aborted)return;
      setSandboxes(list.items.map((sb,i)=>mapSandbox(sb,details[i].status==="fulfilled"?details[i].value:undefined,alerts.items)));setTotal(list.total);setServerStats(stats);
      setSelectedIds(new Set());
      if(details.some(d=>d.status==="rejected"))setError("部分沙箱详情不可用；相应指标显示未采集");
    }).catch(e=>{if(!controller.signal.aborted){setSandboxes([]);setTotal(0);setServerStats(null);setError(api.errorMessage(e))}}).finally(()=>{if(!controller.signal.aborted)setLoading(false)});
    return()=>controller.abort();
  },[page,statusFilter,refresh]);
  useEffect(()=>{const timer=setInterval(()=>setRefresh(v=>v+1),15000);return()=>clearInterval(timer)},[]);
  const totalPages=Math.ceil(total/pageSize);
  const pageData=sandboxes;
  const cpu=sandboxes.flatMap(s=>s.cpu_usage===null?[]:[s.cpu_usage]),mem=sandboxes.flatMap(s=>s.memory_usage===null?[]:[s.memory_usage]);
  const stats={total:serverStats?.total??0,running:serverStats?.by_status.running??0,starting:(serverStats?.by_status.creating??0)+(serverStats?.by_status.deploying??0),error:serverStats?.by_status.failed??0,highAlert:serverStats?.unresolved_anomalies??0,avgCpu:cpu.length?Math.round(cpu.reduce((a,b)=>a+b,0)/cpu.length):null,avgMem:mem.length?Math.round(mem.reduce((a,b)=>a+b,0)/mem.length):null};

  // 全选
  const allSelected = pageData.length > 0 && pageData.every((s) => selectedIds.has(s.id));
  const handleSelectAll = () => {
    if (allSelected) {
      const newSelected = new Set(selectedIds);
      pageData.forEach((s) => newSelected.delete(s.id));
      setSelectedIds(newSelected);
    } else {
      const newSelected = new Set(selectedIds);
      pageData.forEach((s) => newSelected.add(s.id));
      setSelectedIds(newSelected);
    }
  };

  const handleSelectOne = (id: string) => {
    const newSelected = new Set(selectedIds);
    if (newSelected.has(id)) {
      newSelected.delete(id);
    } else {
      newSelected.add(id);
    }
    setSelectedIds(newSelected);
  };

  // 释放沙箱
  const handleRelease=async(sandbox:SandboxInstance)=>{
    if(!confirm(`确定释放「${sandbox.name}」？`))return;
    try{await api.releaseSandbox(Number(sandbox.id));setMessage("释放已提交，等待服务器完成回收");setRefresh(v=>v+1)}catch(e){setError(api.errorMessage(e))}
  };
  const handleBatchRelease=async()=>{
    if(!selectedIds.size||!confirm(`释放选中的 ${selectedIds.size} 个沙箱？`))return;
    const ids=[...selectedIds];
    const results=await Promise.allSettled(ids.map(id=>api.releaseSandbox(Number(id))));
    const failures=results.filter(r=>r.status==="rejected").length;
    setMessage(`已提交 ${results.length-failures} 个释放请求${failures?`；失败：${results.flatMap((r,i)=>r.status==="rejected"?[`会话 #${ids[i]}：${api.errorMessage(r.reason)}`]:[]).join("；")}`:""}`);setRefresh(v=>v+1);
  };

  // 列配置
  const columns: DataTableColumn<SandboxInstance>[] = [
    {
      key: "select",
      title: "",
      width: "40px",
      render: (row) => (
        <input
          type="checkbox"
          checked={selectedIds.has(row.id)}
          onChange={() => handleSelectOne(row.id)}
          onClick={(e) => e.stopPropagation()}
          className="w-4 h-4 rounded border-ink-300 text-brand-500 focus:ring-brand-500"
        />
      ),
    },
    {
      key: "name",
      title: "沙箱名称",
      render: (row) => (
        <div>
          <div className="font-medium text-ink-800 font-mono text-[13px]">{row.name}</div>
          <div className="text-xs text-ink-400 mt-0.5">
            {row.project_name} · {row.defect_name}
          </div>
        </div>
      ),
    },
    {
      key: "user",
      title: "使用用户",
      width: "100px",
      render: (row) => (
        <div className="flex items-center gap-2">
          <div className="w-7 h-7 rounded-full bg-brand-100 text-brand-600 flex items-center justify-center text-[11px] font-bold">
            {row.user_name.charAt(0)}
          </div>
          <span className="text-[13px] text-ink-700">{row.user_name}</span>
        </div>
      ),
    },
    {
      key: "status",
      title: "状态",
      width: "80px",
      render: (row) => {
        const cfg = STATUS_CONFIG[row.status];
        return (
          <span className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-xs font-medium ${cfg.bg} ${cfg.color}`}>
            <span className={`w-1.5 h-1.5 rounded-full ${cfg.dot}`} />
            {cfg.label}
          </span>
        );
      },
    },
    {
      key: "cpu",
      title: "CPU",
      width: "110px",
      render: (row) => <ResourceBar value={row.cpu_usage} warnAt={70} criticalAt={85} />,
    },
    {
      key: "memory",
      title: "内存",
      width: "110px",
      render: (row) => <ResourceBar value={row.memory_usage} warnAt={75} criticalAt={85} />,
    },
    {
      key: "disk",
      title: "磁盘",
      width: "110px",
      render: (row) => <ResourceBar value={row.disk_usage} warnAt={70} criticalAt={85} />,
    },
    {
      key: "duration",
      title: "运行时长",
      width: "90px",
      render: (row) => (
        <span className="text-xs text-ink-600 font-mono">
          {formatDuration(row.duration_seconds ?? Math.max(0, Math.floor((nowMs() - row.created_at) / 1000)))}
        </span>
      ),
    },
    {
      key: "alerts",
      title: "告警",
      width: "60px",
      align: "center",
      render: (row) => {
        const criticalCount = row.alerts.filter((a) => a.level === "critical").length;
        const warnCount = row.alerts.filter((a) => a.level === "warning").length;
        if (criticalCount > 0) {
          return (
            <span className="inline-flex items-center justify-center w-6 h-6 rounded-full bg-danger-100 text-danger-600 text-[11px] font-bold">
              {criticalCount}
            </span>
          );
        }
        if (warnCount > 0) {
          return (
            <span className="inline-flex items-center justify-center w-6 h-6 rounded-full bg-warn-100 text-warn-600 text-[11px] font-bold">
              {warnCount}
            </span>
          );
        }
        return <span className="text-ink-300">—</span>;
      },
    },
    {
      key: "actions",
      title: "操作",
      width: "100px",
      render: (row) => (
        <div className="flex items-center gap-2">
          <button
            onClick={(e) => { e.stopPropagation(); window.open(`/training/${row.project_id}`, "_blank"); }}
            className="text-xs text-brand-600 hover:text-brand-700 font-medium"
          >
            查看
          </button>
          <button
            onClick={(e) => { e.stopPropagation(); handleRelease(row); }}
            className="text-xs text-danger-600 hover:text-danger-700 font-medium"
          >
            释放
          </button>
        </div>
      ),
    },
  ];

  // 告警列表（前 5 条严重告警）
  const criticalAlerts = sandboxes
    .flatMap((s) => s.alerts.map((a) => ({ ...a, sandbox_name: s.name, sandbox_id: s.id })))
    .filter((a) => a.level === "critical")
    .sort((a, b) => b.created_at - a.created_at)
    .slice(0, 5);

  return (
    <div className="space-y-6">
      {error&&<div role="alert" className="p-3 bg-danger-50 text-danger-600">{error}<button className="ml-3 underline" onClick={()=>setRefresh(v=>v+1)}>重试</button></div>}
      {message&&<div role="status" className="p-3 bg-brand-50">{message}</div>}
      <p className="text-xs text-ink-500">每 15 秒更新。告警展示最近 100 条未解决记录；释放状态以服务器为准。未采样指标不会填零。</p>
      {/* 统计卡片 */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <StatCard
          label="总沙箱数"
          value={serverStats?stats.total:"不可用"}
          icon="🖥️"
          color="bg-brand-50 text-brand-600"
          sub={serverStats?`运行 ${stats.running} / 启动 ${stats.starting}`:"统计不可用"}
        />
        <StatCard
          label="平均 CPU"
          value={stats.avgCpu===null?"未采集":`${stats.avgCpu}%`}
          icon="💻"
          color={stats.avgCpu!==null && stats.avgCpu > 70 ? "bg-warn-50 text-warn-600" : "bg-ok-50 text-ok-600"}
          sub="本页已采样沙箱平均值"
        />
        <StatCard
          label="平均内存"
          value={stats.avgMem===null?"未采集":`${stats.avgMem}%`}
          icon="🧠"
          color={stats.avgMem!==null && stats.avgMem > 75 ? "bg-warn-50 text-warn-600" : "bg-ok-50 text-ok-600"}
          sub="本页已采样沙箱平均值"
        />
        <StatCard
          label="异常告警"
          value={serverStats?stats.highAlert:"不可用"}
          icon="⚠️"
          color={stats.highAlert > 0 ? "bg-danger-50 text-danger-600" : "bg-ok-50 text-ok-600"}
          sub={serverStats?`${stats.error} 台异常`:"统计不可用"}
          highlight={stats.highAlert > 0}
        />
      </div>

      {/* 告警面板 */}
      {criticalAlerts.length > 0 && (
        <div className="bg-danger-50/50 border border-danger-200 rounded-xl p-4">
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-2">
              <span className="text-lg">🚨</span>
              <span className="text-[14px] font-bold text-danger-700">
                严重告警（{criticalAlerts.length}）
              </span>
            </div>
            <span className="text-[11px] text-danger-500">实时</span>
          </div>
          <div className="space-y-2">
            {criticalAlerts.map((alert) => (
              <div
                key={alert.id}
                className="flex items-center gap-3 p-2.5 bg-white rounded-lg border border-danger-100"
              >
                <span className="w-2 h-2 rounded-full bg-danger-500 flex-shrink-0" />
                <div className="flex-1 min-w-0">
                  <div className="text-[13px] text-ink-700 font-medium truncate">
                    [{alert.sandbox_name}] {alert.message}
                  </div>
                </div>
                <span className="text-[11px] text-ink-400 flex-shrink-0">
                  {formatTimeAgo(alert.created_at)}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* 沙箱列表 */}
      <DataTable
        title="沙箱实例"
        description={`共 ${stats.total} 个沙箱实例，${stats.running} 台运行中`}
        columns={columns}
        data={pageData}
        rowKey="id"
        loading={loading}
        currentPage={page}
        totalPages={totalPages}
        totalItems={total}
        pageSize={pageSize}
        onPageChange={setPage}
        toolbar={
          <div className="flex items-center gap-2">
            {selectedIds.size > 0 && (
              <button
                onClick={handleBatchRelease}
                className="h-9 px-4 rounded-lg bg-danger-500 hover:bg-danger-600 text-white text-[13px] font-medium transition-colors flex items-center gap-1.5"
              >
                批量释放 ({selectedIds.size})
              </button>
            )}
            <button
              onClick={() => window.location.reload()}
              className="h-9 px-4 rounded-lg border border-ink-200 text-ink-600 text-[13px] font-medium hover:bg-ink-50 transition-colors flex items-center gap-1.5"
            >
              🔄 刷新
            </button>
          </div>
        }
        filter={
          <div className="flex flex-wrap items-center gap-3">
            {/* 全选 */}
            <label className="flex items-center gap-2 cursor-pointer">
              <input
                type="checkbox"
                checked={allSelected}
                onChange={handleSelectAll}
                className="w-4 h-4 rounded border-ink-300 text-brand-500 focus:ring-brand-500"
              />
              <span className="text-[12px] text-ink-600">全选当前页</span>
            </label>

            <div className="h-5 w-px bg-ink-200" />

            {/* 状态筛选 */}
            <div className="flex items-center gap-1">
              {[
                { value: "", label: "全部" },
                { value: "running", label: "运行中" },
                { value: "creating", label: "创建中" },
                { value: "starting", label: "部署中" },
                { value: "error", label: "异常" },
              ].map((opt) => (
                <button
                  key={opt.value}
                  onClick={() => { setStatusFilter(opt.value); setPage(1); }}
                  className={`px-3 py-1.5 rounded-lg text-[12px] font-medium transition-colors ${
                    statusFilter === opt.value
                      ? "bg-brand-500 text-white"
                      : "text-ink-600 hover:bg-ink-100"
                  }`}
                >
                  {opt.label}
                </button>
              ))}
            </div>
          </div>
        }
      />
    </div>
  );
}

/* ===== 子组件 ===== */

function StatCard({
  label,
  value,
  icon,
  color,
  sub,
  highlight = false,
}: {
  label: string;
  value: number | string;
  icon: string;
  color: string;
  sub: string;
  highlight?: boolean;
}) {
  return (
    <div className={`bg-white rounded-xl border ${highlight ? "border-danger-200" : "border-ink-100"} p-4 hover:shadow-md transition-shadow`}>
      <div className="flex items-start justify-between mb-2">
        <span className={`w-9 h-9 rounded-lg flex items-center justify-center text-lg ${color}`}>
          {icon}
        </span>
      </div>
      <div className="text-[22px] font-bold text-ink-800">{value}</div>
      <div className="flex items-center justify-between mt-1">
        <span className="text-[12px] text-ink-500">{label}</span>
        <span className="text-[11px] text-ink-400">{sub}</span>
      </div>
    </div>
  );
}

function ResourceBar({
  value,
  warnAt,
  criticalAt,
}: {
  value: number | null;
  warnAt: number;
  criticalAt: number;
}) {
  if(value===null)return <span className="text-xs text-ink-400">未采集</span>;
  const color = value >= criticalAt
    ? "bg-danger-500"
    : value >= warnAt
      ? "bg-warn-500"
      : "bg-ok-500";
  const textColor = value >= criticalAt
    ? "text-danger-600"
    : value >= warnAt
      ? "text-warn-600"
      : "text-ok-600";

  return (
    <div className="w-full">
      <div className="flex items-center justify-between mb-1">
        <div className="h-1.5 flex-1 bg-ink-100 rounded-full overflow-hidden mr-2">
          <div
            className={`h-full ${color} rounded-full`}
            style={{ width: `${Math.min(value, 100)}%` }}
          />
        </div>
        <span className={`text-[11px] font-mono font-medium ${textColor} w-9 text-right`}>
          {value}%
        </span>
      </div>
    </div>
  );
}

function formatDuration(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  const mins = Math.floor(seconds / 60);
  const secs = seconds % 60;
  if (mins < 60) return `${mins}m${secs > 0 ? secs + "s" : ""}`;
  const hours = Math.floor(mins / 60);
  const m = mins % 60;
  return `${hours}h${m > 0 ? m + "m" : ""}`;
}

function formatTimeAgo(timestamp: number): string {
  const diff = Math.floor((nowMs() - timestamp) / 1000);
  if (diff < 60) return `${diff}秒前`;
  if (diff < 3600) return `${Math.floor(diff / 60)}分钟前`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}小时前`;
  return `${Math.floor(diff / 86400)}天前`;
}
