import { useEffect, useState, useRef } from "react";
import {errorMessage} from "../services/data";
import { Pagination } from "@quenching/shared";
import {useSearchParams} from "react-router-dom";
import {
  listPitfallConfigs,
  updatePitfallStatus,
  deletePitfallConfig,
  PITFALL_TYPE_LABELS,
  PITFALL_TYPE_COLORS,
  PITFALL_STATUS_LABELS,
  PITFALL_STATUS_COLORS,
  CONFIDENCE_LABELS,
  SEVERITY_LABELS,
  TRIGGER_WHEN_LABELS,
  type PitfallConfig,
  type PitfallConfigStatus,
  type PitfallType,
} from "@quenching/shared";
import { PitfallConfigForm } from "../components/PitfallConfigForm";

/**
 * 坑点配置管理页（T3-19 坑点配置后台页）
 *
 * 功能：
 * - 表格展示坑点配置列表
 * - 按缺陷ID / 类型 / 状态筛选
 * - 分页浏览
 * - 新建 / 编辑 / 启用 / 禁用 / 删除
 * - 弹窗形式编辑配置（结构化表单）
 */
export function PitfallAdminPage() {
  // 筛选条件
  const [searchParams,setSearchParams]=useSearchParams();
  const defectId=searchParams.get("defect_id")??"";
  const setDefectId=(id:string)=>setSearchParams(previous=>{
    const next=new URLSearchParams(previous);
    if(id)next.set("defect_id",id);else next.delete("defect_id");
    return next;
  },{replace:true});
  const [pitfallType, setPitfallType] = useState<PitfallType | "">("");
  const [status, setStatus] = useState<PitfallConfigStatus | "">("");

  // 分页
  const [page, setPage] = useState(1);
  const [pageSize] = useState(10);
  const [total, setTotal] = useState(0);

  // 数据
  const [items, setItems] = useState<PitfallConfig[]>([]);
  const [loading, setLoading] = useState(false);
  const [listError,setListError]=useState("");
  const [refresh,setRefresh]=useState(0);
  const requestVersion=useRef(0);

  // 弹窗
  const [showForm, setShowForm] = useState(false);
  const [editingId, setEditingId] = useState<number | null>(null);

  // 加载列表
  const loadList=()=>setRefresh(v=>v+1);
  useEffect(()=>{
    const version=++requestVersion.current;let live=true;setLoading(true);setListError("");
    const read=async()=>{try{
      const result=await listPitfallConfigs({page,page_size:pageSize,defect_id:defectId?Number(defectId):undefined,pitfall_type:pitfallType||undefined,status:status||undefined});
      if(live&&version===requestVersion.current){setItems(result.items);setTotal(result.total)}
    }catch(err){if(live&&version===requestVersion.current){setItems([]);setTotal(0);setListError(errorMessage(err))}}
    finally{if(live&&version===requestVersion.current)setLoading(false)}};
    void read();return()=>{live=false};
  },[page,defectId,pitfallType,status,refresh]);

  const totalPages = Math.ceil(total / pageSize);

  // 搜索
  const handleSearch = () => {
    setPage(1);
    loadList();
  };

  // 重置筛选
  const handleReset = () => {
    setDefectId("");
    setPitfallType("");
    setStatus("");
    setPage(1);
    loadList();
  };

  // 新建
  const handleCreate = () => {
    setEditingId(null);
    setShowForm(true);
  };

  // 编辑
  const handleEdit = (item: PitfallConfig) => {
    setEditingId(item.id);
    setShowForm(true);
  };

  // 状态切换
  const handleToggleStatus = async (item: PitfallConfig) => {
    const newStatus: PitfallConfigStatus = item.status === "active" ? "inactive" : "active";
    const action = newStatus === "active" ? "启用" : "禁用";
    if (!confirm(`确定要${action}「${item.name}」吗？`)) return;

    try {
      await updatePitfallStatus(item.id, newStatus);
      loadList();
    } catch (err) {
      console.error(`${action}失败:`, err);
      alert("操作失败，请稍后重试");
    }
  };

  // 删除
  const handleDelete = async (item: PitfallConfig) => {
    if (!confirm(`确定要删除「${item.name}」吗？删除后不可恢复。`)) return;

    try {
      await deletePitfallConfig(item.id);
      loadList();
    } catch (err) {
      console.error("删除失败:", err);
      alert("删除失败，请稍后重试");
    }
  };

  // 表单提交成功回调
  const handleFormSuccess = () => {
    setShowForm(false);
    loadList();
  };

  return (
    <div className="min-h-screen bg-ink-50">
      {listError&&<div role="alert" className="p-3 bg-danger-50 text-danger-600">{listError}<button className="ml-3 underline" onClick={loadList}>重试</button></div>}
      {/* 顶部栏 */}
      <div className="bg-white border-b border-ink-100 sticky top-0 z-10">
        <div className="max-w-7xl mx-auto px-6 py-4 flex items-center justify-between">
          <div>
            <h1 className="text-lg font-bold text-ink-900">🎭 坑点配置管理</h1>
            <p className="text-sm text-ink-500 mt-0.5">管理 AI 坑点配置，运营无需改代码即可调整坑点内容</p>
          </div>
          <button
            onClick={handleCreate}
            className="h-10 px-5 rounded-lg bg-brand-500 hover:bg-brand-600 text-white text-sm font-medium transition-colors shadow-md shadow-brand-500/20 flex items-center gap-2"
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round">
              <path d="M12 5v14M5 12h14" />
            </svg>
            新建坑点
          </button>
        </div>
      </div>

      <div className="max-w-7xl mx-auto px-6 py-6">
        {/* 筛选栏 */}
        <div className="bg-white rounded-xl border border-ink-100 p-4 mb-4">
          <div className="flex flex-wrap items-end gap-4">
            <div className="flex flex-col gap-1.5">
              <label className="text-xs text-ink-500 font-medium">缺陷 ID</label>
              <input
                type="number"
                value={defectId}
                onChange={(e) => {setDefectId(e.target.value);setPage(1)}}
                placeholder="输入缺陷ID"
                className="h-9 w-36 px-3 rounded-lg border border-ink-200 text-sm focus:outline-none focus:ring-2 focus:ring-brand-500/20 focus:border-brand-500"
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <label className="text-xs text-ink-500 font-medium">坑点类型</label>
              <select
                value={pitfallType}
                onChange={(e) => {setPitfallType(e.target.value as PitfallType | "");setPage(1)}}
                className="h-9 w-36 px-3 rounded-lg border border-ink-200 text-sm focus:outline-none focus:ring-2 focus:ring-brand-500/20 focus:border-brand-500"
              >
                <option value="">全部类型</option>
                <option value="causal_misjudgment">因果误判</option>
                <option value="info_omission">信息遗漏</option>
                <option value="overconfidence">过度自信</option>
              </select>
            </div>
            <div className="flex flex-col gap-1.5">
              <label className="text-xs text-ink-500 font-medium">状态</label>
              <select
                value={status}
                onChange={(e) => {setStatus(e.target.value as PitfallConfigStatus | "");setPage(1)}}
                className="h-9 w-36 px-3 rounded-lg border border-ink-200 text-sm focus:outline-none focus:ring-2 focus:ring-brand-500/20 focus:border-brand-500"
              >
                <option value="">全部状态</option>
                <option value="draft">草稿</option>
                <option value="active">已启用</option>
                <option value="inactive">已禁用</option>
              </select>
            </div>
            <div className="flex gap-2">
              <button
                onClick={handleSearch}
                className="h-9 px-5 rounded-lg bg-brand-500 hover:bg-brand-600 text-white text-sm font-medium transition-colors"
              >
                查询
              </button>
              <button
                onClick={handleReset}
                className="h-9 px-5 rounded-lg border border-ink-200 text-ink-600 text-sm font-medium hover:bg-ink-50 transition-colors"
              >
                重置
              </button>
            </div>
          </div>
        </div>

        {/* 表格 */}
        <div className="bg-white rounded-xl border border-ink-100 overflow-hidden">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="bg-ink-50 border-b border-ink-100">
                  <th className="text-left px-4 py-3 text-xs font-semibold text-ink-500">ID</th>
                  <th className="text-left px-4 py-3 text-xs font-semibold text-ink-500">名称</th>
                  <th className="text-left px-4 py-3 text-xs font-semibold text-ink-500">类型</th>
                  <th className="text-left px-4 py-3 text-xs font-semibold text-ink-500">触发方式</th>
                  <th className="text-left px-4 py-3 text-xs font-semibold text-ink-500">置信度</th>
                  <th className="text-left px-4 py-3 text-xs font-semibold text-ink-500">严重度</th>
                  <th className="text-left px-4 py-3 text-xs font-semibold text-ink-500">排序</th>
                  <th className="text-left px-4 py-3 text-xs font-semibold text-ink-500">状态</th>
                  <th className="text-left px-4 py-3 text-xs font-semibold text-ink-500">操作</th>
                </tr>
              </thead>
              <tbody>
                {loading ? (
                  <tr>
                    <td colSpan={9} className="text-center py-12 text-ink-400">
                      加载中...
                    </td>
                  </tr>
                ) : items.length === 0 ? (
                  <tr>
                    <td colSpan={9} className="text-center py-12 text-ink-400">
                      暂无数据
                    </td>
                  </tr>
                ) : (
                  items.map((item) => {
                    const typeColor = PITFALL_TYPE_COLORS[item.pitfall_type];
                    const statusColor = PITFALL_STATUS_COLORS[item.status];
                    return (
                      <tr key={item.id} className="border-b border-ink-50 hover:bg-ink-50/50 transition-colors">
                        <td className="px-4 py-3 text-ink-500 font-mono text-xs">#{item.id}</td>
                        <td className="px-4 py-3">
                          <div className="font-medium text-ink-800">{item.name}</div>
                          {item.description && (
                            <div className="text-xs text-ink-400 mt-0.5 line-clamp-1">{item.description}</div>
                          )}
                        </td>
                        <td className="px-4 py-3">
                          <span className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-md text-xs font-medium ${typeColor.bg} ${typeColor.text}`}>
                            <span className={`w-1.5 h-1.5 rounded-full ${typeColor.border.replace("border-", "bg-")}`} />
                            {PITFALL_TYPE_LABELS[item.pitfall_type]}
                          </span>
                        </td>
                        <td className="px-4 py-3 text-ink-600 text-xs">
                          {TRIGGER_WHEN_LABELS[item.trigger_condition.when]}
                          <span className="text-ink-400 ml-1">
                            ({Math.round((item.trigger_condition.probability || 1) * 100)}%)
                          </span>
                        </td>
                        <td className="px-4 py-3 text-ink-600 text-xs">
                          {CONFIDENCE_LABELS[item.confidence_level]}
                        </td>
                        <td className="px-4 py-3 text-ink-600 text-xs">
                          {SEVERITY_LABELS[item.severity]}
                        </td>
                        <td className="px-4 py-3 text-ink-500 text-xs">{item.sort_order}</td>
                        <td className="px-4 py-3">
                          <span className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-md text-xs font-medium ${statusColor.bg} ${statusColor.text}`}>
                            <span className={`w-1.5 h-1.5 rounded-full ${statusColor.dot}`} />
                            {PITFALL_STATUS_LABELS[item.status]}
                          </span>
                        </td>
                        <td className="px-4 py-3">
                          <div className="flex items-center gap-3">
                            <button
                              onClick={() => handleEdit(item)}
                              className="text-xs text-brand-600 hover:text-brand-700 font-medium transition-colors"
                            >
                              编辑
                            </button>
                            <button
                              onClick={() => handleToggleStatus(item)}
                              className={`text-xs font-medium transition-colors ${
                                item.status === "active"
                                  ? "text-warn-600 hover:text-warn-700"
                                  : "text-ok-600 hover:text-ok-700"
                              }`}
                            >
                              {item.status === "active" ? "禁用" : "启用"}
                            </button>
                            <button
                              onClick={() => handleDelete(item)}
                              className="text-xs text-danger-600 hover:text-danger-700 font-medium transition-colors"
                            >
                              删除
                            </button>
                          </div>
                        </td>
                      </tr>
                    );
                  })
                )}
              </tbody>
            </table>
          </div>

          {/* 分页 */}
          <div className="px-4 py-4 border-t border-ink-50">
            <Pagination
              currentPage={page}
              totalPages={totalPages}
              totalItems={total}
              pageSize={pageSize}
              onPageChange={setPage}
            />
          </div>
        </div>
      </div>

      {/* 表单弹窗 */}
      {showForm && (
        <PitfallConfigForm
          editId={editingId}
          initialDefectId={Number(defectId)||undefined}
          onClose={() => setShowForm(false)}
          onSuccess={handleFormSuccess}
        />
      )}
    </div>
  );
}
