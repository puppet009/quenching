// 故障剧本管理页（T4-9）
//
// 功能：
//   - 剧本列表展示（DataTable 通用表格）
//   - 按项目/类型/分类/难度/状态筛选
//   - 分页浏览
//   - 新建 / 编辑 / 启用禁用 / 删除
//   - 配置项表单化编辑（弹窗）

import { useState, useEffect } from "react";
import { DataTable, type DataTableColumn, type Project } from "@quenching/shared";
import {Link, useSearchParams} from "react-router-dom";
import * as api from "../services/data";
import {
  DEFECT_TYPE_LABELS,
  DEFECT_CATEGORY_LABELS,
  DEFECT_CATEGORY_ICONS,
  DEFECT_DIFFICULTY_LABELS,
  DEFECT_DIFFICULTY_COLORS,
  formatFixTime,
  type Defect,
  type DefectType,
  type DefectCategory,
  type DefectDifficulty,
} from "@quenching/shared";

/**
 * 故障剧本管理页（T4-9）
 */
export function DefectAdminPage() {
  const [catalog,setCatalog]=useState<api.Template[]>([]);
  const [projects,setProjects]=useState<Project[]>([]);
  const [searchParams,setSearchParams]=useSearchParams();
  const templateSlug=searchParams.get("template")??"";
  const setTemplateSlug=(slug:string)=>setSearchParams(previous=>{
    const next=new URLSearchParams(previous);
    if(slug)next.set("template",slug);else next.delete("template");
    return next;
  },{replace:true});
  const [error,setError]=useState("");
  const [catalogErrors,setCatalogErrors]=useState<string[]>([]);
  const [catalogRetry,setCatalogRetry]=useState(0);
  const [refresh,setRefresh]=useState(0);
  useEffect(()=>{
    let live=true;
    api.loadDefectCatalog().then(result=>{
      if(live){setCatalog(result.templates);setProjects(result.projects);setCatalogErrors(result.errors)}
    });
    return()=>{live=false};
  },[catalogRetry]);
  // 筛选
  const [projectId, setProjectId] = useState("");
  const [defectType, setDefectType] = useState<DefectType | "">("");
  const [category, setCategory] = useState<DefectCategory | "">("");
  const [difficulty, setDifficulty] = useState<DefectDifficulty | "">("");
  const [isActive, setIsActive] = useState<string>("");

  // 分页
  const [page, setPage] = useState(1);
  const pageSize = 10;

  // 数据
  const [items, setItems] = useState<api.ManagedDefect[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);

  // 弹窗
  const [showForm, setShowForm] = useState(false);
  const [editingId, setEditingId] = useState<number | null>(null);

  // 加载列表
  const loadList=()=>setRefresh(v=>v+1);
  useEffect(()=>{
    const controller=new AbortController();setLoading(true);setError("");
    api.defects({page,page_size:pageSize,project_id:projectId||undefined,template_slug:templateSlug||undefined,defect_type:defectType||undefined,category:category||undefined,difficulty:difficulty||undefined,is_active:isActive||undefined},controller.signal)
    .then(result=>{if(!controller.signal.aborted){setItems(result.items);setTotal(result.total)}})
    .catch(err=>{if(!controller.signal.aborted){setItems([]);setTotal(0);setError(api.errorMessage(err))}})
    .finally(()=>{if(!controller.signal.aborted)setLoading(false)});
    return()=>controller.abort();
  },[page,refresh,projectId,templateSlug,defectType,category,difficulty,isActive]);

  const totalPages = Math.ceil(total / pageSize);

  // 搜索
  const handleSearch = () => {
    setPage(1);
    loadList();
  };

  // 重置
  const handleReset = () => {
    setProjectId("");setTemplateSlug("");
    setDefectType("");
    setCategory("");
    setDifficulty("");
    setIsActive("");
    setPage(1);
    loadList();
  };

  // 切换启用状态
  const handleToggleActive=async(item:Defect)=>{
    if(!confirm(`确定${item.is_active?"禁用":"启用"}「${item.name}」？`))return;
    try{await api.setDefectActive(item.id,!item.is_active);loadList()}catch(err){setError(api.errorMessage(err))}
  };
  const handleDelete=async(item:Defect)=>{
    if(!confirm(`确定删除「${item.name}」？`))return;
    try{await api.deleteDefect(item.id);if(items.length===1&&page>1)setPage(p=>p-1);else loadList()}catch(err){setError(api.errorMessage(err))}
  };

  // 列配置
  const columns: DataTableColumn<api.ManagedDefect>[] = [
    {key:"template",title:"模板 / 验收",render:row=><div>{row.template_bindings?.map(b=><div key={b.template_slug}>{b.template_slug} / {b.target_workload}<span className="block text-xs text-ink-400">{b.status==="verified"?"已验收":b.status==="draft"?"待适配 / 草稿":"待服务器验收"}</span></div>)}</div>},
    {
      key: "id",
      title: "ID",
      width: "60px",
      render: (row) => <span className="text-ink-400 font-mono text-xs">#{row.id}</span>,
    },
    {
      key: "name",
      title: "剧本名称",
      render: (row) => (
        <div>
          <div className="font-medium text-ink-800 flex items-center gap-2">
            <span>{DEFECT_CATEGORY_ICONS[row.category]}</span>
            {row.name}
          </div>
          <div className="text-xs text-ink-400 mt-0.5 font-mono">{row.code}</div>
        </div>
      ),
    },
    {
      key: "type",
      title: "类型",
      width: "90px",
      render: (row) => (
        <span className="text-xs text-ink-600">{DEFECT_TYPE_LABELS[row.defect_type]}</span>
      ),
    },
    {
      key: "category",
      title: "分类",
      width: "90px",
      render: (row) => (
        <span className="text-xs text-ink-600">{DEFECT_CATEGORY_LABELS[row.category]}</span>
      ),
    },
    {
      key: "difficulty",
      title: "难度",
      width: "70px",
      render: (row) => (
        <span className={`inline-block px-2 py-0.5 rounded text-xs font-medium border ${DEFECT_DIFFICULTY_COLORS[row.difficulty]}`}>
          {DEFECT_DIFFICULTY_LABELS[row.difficulty]}
        </span>
      ),
    },
    {
      key: "fix_time",
      title: "预计用时",
      width: "90px",
      render: (row) => (
        <span className="text-xs text-ink-600">{formatFixTime(row.estimated_fix_time)}</span>
      ),
    },
    {
      key: "is_active",
      title: "状态",
      width: "80px",
      render: (row) => (
        <span className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-xs font-medium ${
          row.is_active
            ? "bg-ok-50 text-ok-600"
            : "bg-ink-100 text-ink-500"
        }`}>
          <span className={`w-1.5 h-1.5 rounded-full ${row.is_active ? "bg-ok-500" : "bg-ink-400"}`} />
          {row.is_active ? "已启用" : "已禁用"}
        </span>
      ),
    },
    {
      key: "actions",
      title: "操作",
      width: "210px",
      render: (row) => (
        <div className="flex items-center gap-3">
          <button
            onClick={(e) => { e.stopPropagation(); setEditingId(row.id); setShowForm(true); }}
            className="text-xs text-brand-600 hover:text-brand-700 font-medium"
          >
            编辑
          </button>
          <button
            onClick={(e) => { e.stopPropagation(); handleToggleActive(row); }}
            className={`text-xs font-medium ${
              row.is_active ? "text-warn-600 hover:text-warn-700" : "text-ok-600 hover:text-ok-700"
            }`}
          >
            {row.is_active ? "禁用" : "启用"}
          </button>
          <button
            onClick={(e) => { e.stopPropagation(); handleDelete(row); }}
            className="text-xs text-danger-600 hover:text-danger-700 font-medium"
          >
            删除
          </button>
          <Link to={`/admin/pitfalls?defect_id=${row.id}`} className="text-xs text-brand-600 hover:text-brand-700 whitespace-nowrap">配置坑点</Link>
        </div>
      ),
    },
  ];

  return (
    <div className="space-y-4">
      {catalogErrors.length>0&&<div role="alert" className="p-3 bg-warn-50 text-warn-600 text-sm">{catalogErrors.join("；")}<button className="ml-3 underline" onClick={()=>setCatalogRetry(value=>value+1)}>重试目录</button></div>}
      {error&&<div role="alert" className="p-3 bg-danger-50 text-danger-600">{error}<button className="ml-3 underline" onClick={loadList}>重试</button></div>}
      <DataTable
        title="故障剧本管理"
        description="管理所有训练项目的故障剧本，配置启用/禁用状态"
        columns={columns}
        data={items}
        rowKey="id"
        loading={loading}
        currentPage={page}
        totalPages={totalPages}
        totalItems={total}
        pageSize={pageSize}
        onPageChange={setPage}
        toolbar={
          <button
            onClick={() => { setEditingId(null); setShowForm(true); }}
            className="h-9 px-4 rounded-lg bg-brand-500 hover:bg-brand-600 text-white text-[13px] font-medium transition-colors flex items-center gap-1.5"
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round">
              <path d="M12 5v14M5 12h14" />
            </svg>
            新建剧本
          </button>
        }
        filter={
          <div className="flex flex-wrap items-end gap-3">
            <FilterSelect label="模板" value={templateSlug} onChange={v=>{setTemplateSlug(v);setPage(1)}} options={[{value:"",label:"全部模板"},...catalog.map(t=>({value:t.slug,label:t.name}))]}/>
            <FilterSelect
              label="项目"
              value={projectId}
              onChange={v=>{setProjectId(v);setPage(1)}}
              options={[
                { value: "", label: "全部项目" },
                ...projects.map(p=>({value:String(p.id),label:p.name})),
              ]}
            />
            <FilterSelect
              label="类型"
              value={defectType}
              onChange={(v) => {setDefectType(v as DefectType | "");setPage(1)}}
              options={[
                { value: "", label: "全部类型" },
                { value: "code", label: "代码缺陷" },
                { value: "config", label: "配置错误" },
                { value: "architecture", label: "架构问题" },
                { value: "resource", label: "资源耗尽" },
              ]}
            />
            <FilterSelect
              label="分类"
              value={category}
              onChange={(v) => {setCategory(v as DefectCategory | "");setPage(1)}}
              options={[
                { value: "", label: "全部分类" },
                { value: "database", label: "数据库" },
                { value: "cache", label: "缓存" },
                { value: "mq", label: "消息队列" },
                { value: "network", label: "网络" },
                { value: "cpu", label: "CPU" },
                { value: "memory", label: "内存" },
                { value: "disk", label: "磁盘" },
                { value: "threadpool", label: "线程池" },
              ]}
            />
            <FilterSelect
              label="难度"
              value={difficulty}
              onChange={(v) => {setDifficulty(v as DefectDifficulty | "");setPage(1)}}
              options={[
                { value: "", label: "全部难度" },
                { value: "easy", label: "简单" },
                { value: "medium", label: "中等" },
                { value: "hard", label: "困难" },
              ]}
            />
            <FilterSelect
              label="状态"
              value={isActive}
              onChange={v=>{setIsActive(v);setPage(1)}}
              options={[
                { value: "", label: "全部状态" },
                { value: "true", label: "已启用" },
                { value: "false", label: "已禁用" },
              ]}
            />
            <div className="flex gap-2">
              <button
                onClick={handleSearch}
                className="h-9 px-5 rounded-lg bg-brand-500 hover:bg-brand-600 text-white text-[13px] font-medium transition-colors"
              >
                查询
              </button>
              <button
                onClick={handleReset}
                className="h-9 px-5 rounded-lg border border-ink-200 text-ink-600 text-[13px] font-medium hover:bg-ink-50 transition-colors"
              >
                重置
              </button>
            </div>
          </div>
        }
      />

      {/* 编辑表单弹窗 */}
      {showForm && (
        <DefectFormModal
          catalog={catalog} projects={projects}
          editId={editingId}
          defect={editingId ? items.find((d) => d.id === editingId) : undefined}
          onClose={() => setShowForm(false)}
          onSuccess={() => { setShowForm(false); loadList(); }}
        />
      )}
    </div>
  );
}

/* ===== 子组件 ===== */

function FilterSelect({
  label,
  value,
  onChange,
  options,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  options: { value: string; label: string }[];
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <label className="text-xs text-ink-500 font-medium">{label}</label>
      <select
        aria-label={label}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="h-9 min-w-[120px] px-3 rounded-lg border border-ink-200 text-[13px] text-ink-700 bg-white focus:outline-none focus:ring-2 focus:ring-brand-500/20 focus:border-brand-500"
      >
        {options.map((opt) => (
          <option key={opt.value} value={opt.value}>{opt.label}</option>
        ))}
      </select>
    </div>
  );
}

/* ===== 表单弹窗 ===== */

function DefectFormModal({
  editId,
  defect,
  onClose,
  onSuccess, catalog, projects,
}: {
  catalog:api.Template[];
  projects:Project[];
  editId: number | null;
  defect?: api.ManagedDefect;
  onClose: () => void;
  onSuccess: () => void;
}) {
  const [formData, setFormData] = useState({
    project_id:defect?.project_id??0,
    name: defect?.name || "",
    code: defect?.code || "",
    defect_type: defect?.defect_type || "resource",
    category: defect?.category || "cpu",
    difficulty: defect?.difficulty || "easy",
    description: defect?.description || "",
    root_cause: defect?.root_cause || "",
    estimated_fix_time: defect?.estimated_fix_time || 15,
    score_weight: defect?.score_weight || 1.0,
    is_active: defect?.is_active ?? false,
    sort_order: defect?.sort_order || 1,
  });
  const [submitting, setSubmitting] = useState(false);
  const [activeTab, setActiveTab] = useState<"basic" | "config">("basic");

  const [error,setError]=useState("");
  const [configs,setConfigs]=useState<Record<string,string>>(()=>Object.fromEntries(["provenance","template_bindings","trigger_config","manifestation_config","fix_config","amplifier_config","freeze_rules"].map(key=>[key,JSON.stringify(defect?.[key as keyof api.ManagedDefect]??(key==="template_bindings"?[]:key==="provenance"?{kind:"incident",title:"",url:"",summary:"",reproduction_difference:""}:{}),null,2)])));
  const [bindingSlug,setBindingSlug]=useState(defect?.template_bindings?.[0]?.template_slug??catalog[0]?.slug??"");
  const [bindingTarget,setBindingTarget]=useState(defect?.template_bindings?.[0]?.target_workload??"");
  const [evidence,setEvidence]=useState(JSON.stringify({observed:"",repaired:"",revoked:"",cleanup:"",validated_at:""},null,2));
  const handleSubmit=async(e:React.FormEvent)=>{
    e.preventDefault();setSubmitting(true);setError("");
    try{
      const parsed=Object.fromEntries(Object.entries(configs).map(([key,text])=>[key,JSON.parse(text)]));
      for(const [key,value] of Object.entries(parsed)){if(key==="template_bindings"?!Array.isArray(value):!value||typeof value!=="object"||Array.isArray(value))throw new Error(`${key} 类型错误`)}
      const bound=(parsed.template_bindings as unknown[]).length>0;
      await api.saveDefect(editId,{...formData,...parsed,is_active:bound?false:formData.is_active});
      if(editId&&!bound)await api.setDefectActive(editId,formData.is_active);
      onSuccess();
    }catch(err){setError(api.errorMessage(err))}finally{setSubmitting(false)}
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/40" onClick={onClose}>
      <div
        className="bg-white rounded-xl w-full max-w-2xl max-h-[90vh] flex flex-col overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        {/* 头部 */}
        <div className="px-5 py-4 border-b border-ink-100 flex items-center justify-between">
          <h3 className="text-[16px] font-bold text-ink-800">
            {editId ? "编辑故障剧本" : "新建故障剧本"}
          </h3>
          <button
            onClick={onClose}
            className="w-8 h-8 flex items-center justify-center rounded-lg text-ink-400 hover:bg-ink-100 hover:text-ink-600 transition-colors"
          >
            ✕
          </button>
        </div>

        {/* Tab 切换 */}
        <div className="px-5 border-b border-ink-100 flex gap-4">
          {[
            { key: "basic", label: "基本信息" },
            { key: "config", label: "配置项" },
          ].map((tab) => (
            <button
              key={tab.key}
              onClick={() => setActiveTab(tab.key as typeof activeTab)}
              className={`py-3 px-1 text-[13px] font-medium border-b-2 -mb-px transition-colors ${
                activeTab === tab.key
                  ? "border-brand-500 text-brand-600"
                  : "border-transparent text-ink-500 hover:text-ink-700"
              }`}
            >
              {tab.label}
            </button>
          ))}
        </div>

        {/* 表单内容 */}
        <form id="defect-config-form" onSubmit={handleSubmit} className="flex-1 overflow-y-auto p-5">
          {error&&<div role="alert" className="p-3 text-danger-600">{error}</div>}
          {activeTab === "basic" && (
            <div className="space-y-4">
              <FormRow label="数据库项目（模板故障可不选）"><select disabled={!!editId} value={formData.project_id} onChange={e=>setFormData({...formData,project_id:Number(e.target.value)})} className="w-full border rounded-lg p-2"><option value={0}>仅模板故障</option>{projects.map(p=><option key={p.id} value={p.id}>{p.name}</option>)}</select></FormRow>
              <FormRow label="剧本名称" required>
                <input
                  type="text"
                  value={formData.name}
                  onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                  placeholder="请输入剧本名称"
                  className="w-full h-9 px-3 rounded-lg border border-ink-200 text-[13px] focus:outline-none focus:ring-2 focus:ring-brand-500/20 focus:border-brand-500"
                  required
                />
              </FormRow>

              <div className="grid grid-cols-2 gap-4">
                <FormRow label="剧本编码" required>
                  <input
                    type="text"
                    value={formData.code}
                    onChange={(e) => setFormData({ ...formData, code: e.target.value })}
                    placeholder="如: cpu-stress-01"
                    className="w-full h-9 px-3 rounded-lg border border-ink-200 text-[13px] focus:outline-none focus:ring-2 focus:ring-brand-500/20 focus:border-brand-500 font-mono"
                    required
                  />
                </FormRow>

                <FormRow label="难度等级" required>
                  <select
                    value={formData.difficulty}
                    onChange={(e) => setFormData({ ...formData, difficulty: e.target.value as DefectDifficulty })}
                    className="w-full h-9 px-3 rounded-lg border border-ink-200 text-[13px] focus:outline-none focus:ring-2 focus:ring-brand-500/20 focus:border-brand-500"
                  >
                    <option value="easy">简单</option>
                    <option value="medium">中等</option>
                    <option value="hard">困难</option>
                  </select>
                </FormRow>
              </div>

              <div className="grid grid-cols-2 gap-4">
                <FormRow label="故障类型" required>
                  <select
                    value={formData.defect_type}
                    onChange={(e) => setFormData({ ...formData, defect_type: e.target.value as DefectType })}
                    className="w-full h-9 px-3 rounded-lg border border-ink-200 text-[13px] focus:outline-none focus:ring-2 focus:ring-brand-500/20 focus:border-brand-500"
                  >
                    {Object.entries(DEFECT_TYPE_LABELS).map(([k, v]) => (
                      <option key={k} value={k}>{v}</option>
                    ))}
                  </select>
                </FormRow>

                <FormRow label="故障分类" required>
                  <select
                    value={formData.category}
                    onChange={(e) => setFormData({ ...formData, category: e.target.value as DefectCategory })}
                    className="w-full h-9 px-3 rounded-lg border border-ink-200 text-[13px] focus:outline-none focus:ring-2 focus:ring-brand-500/20 focus:border-brand-500"
                  >
                    {Object.entries(DEFECT_CATEGORY_LABELS).map(([k, v]) => (
                      <option key={k} value={k}>{v}</option>
                    ))}
                  </select>
                </FormRow>
              </div>

              <div className="grid grid-cols-2 gap-4">
                <FormRow label="预计修复时间（分钟）">
                  <input
                    type="number"
                    value={formData.estimated_fix_time}
                    onChange={(e) => setFormData({ ...formData, estimated_fix_time: Number(e.target.value) })}
                    min={1}
                    className="w-full h-9 px-3 rounded-lg border border-ink-200 text-[13px] focus:outline-none focus:ring-2 focus:ring-brand-500/20 focus:border-brand-500"
                  />
                </FormRow>

                <FormRow label="权重">
                  <input
                    type="number"
                    value={formData.score_weight}
                    onChange={(e) => setFormData({ ...formData, score_weight: Number(e.target.value) })}
                    step={0.1}
                    min={0}
                    className="w-full h-9 px-3 rounded-lg border border-ink-200 text-[13px] focus:outline-none focus:ring-2 focus:ring-brand-500/20 focus:border-brand-500"
                  />
                </FormRow>
              </div>

              <FormRow label="故障描述">
                <textarea
                  value={formData.description}
                  onChange={(e) => setFormData({ ...formData, description: e.target.value })}
                  placeholder="描述故障的现象和影响"
                  rows={3}
                  className="w-full px-3 py-2 rounded-lg border border-ink-200 text-[13px] focus:outline-none focus:ring-2 focus:ring-brand-500/20 focus:border-brand-500 resize-none"
                />
              </FormRow>

              <FormRow label="根本原因">
                <textarea
                  value={formData.root_cause}
                  onChange={(e) => setFormData({ ...formData, root_cause: e.target.value })}
                  placeholder="描述故障的根本原因分析"
                  rows={3}
                  className="w-full px-3 py-2 rounded-lg border border-ink-200 text-[13px] focus:outline-none focus:ring-2 focus:ring-brand-500/20 focus:border-brand-500 resize-none"
                />
              </FormRow>

              <FormRow label="排序">
                <input
                  type="number"
                  value={formData.sort_order}
                  onChange={(e) => setFormData({ ...formData, sort_order: Number(e.target.value) })}
                  className="w-32 h-9 px-3 rounded-lg border border-ink-200 text-[13px] focus:outline-none focus:ring-2 focus:ring-brand-500/20 focus:border-brand-500"
                />
              </FormRow>

              <FormRow label="启用状态">
                <label className="flex items-center gap-2 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={formData.is_active}
                    onChange={(e) => setFormData({ ...formData, is_active: e.target.checked })}
                    className="w-4 h-4 rounded border-ink-300 text-brand-500 focus:ring-brand-500"
                  />
                  <span className="text-[13px] text-ink-700">启用此剧本</span>
                </label>
              </FormRow>
            </div>
          )}

          {activeTab === "config" && (
            <div className="space-y-4">
              <p className="text-sm text-ink-500">模板故障保存后待验收且禁用。修改配置会使旧验收失效。</p>
              <div className="flex flex-wrap gap-2"><select value={bindingSlug} onChange={e=>{setBindingSlug(e.target.value);setBindingTarget("")}}><option value="">选择模板</option>{catalog.map(t=><option key={t.slug} value={t.slug}>{t.name}</option>)}</select><select value={bindingTarget} onChange={e=>setBindingTarget(e.target.value)}><option value="">选择工作负载</option>{catalog.find(t=>t.slug===bindingSlug)?.services.map(t=><option key={t.name} value={t.workload}>{t.name}</option>)}</select><button type="button" disabled={!bindingSlug||!bindingTarget} onClick={()=>{try{const b=JSON.parse(configs.template_bindings) as api.Binding[];if(!Array.isArray(b))throw new Error("绑定必须为数组");setConfigs({...configs,template_bindings:JSON.stringify([...b.filter(b=>b.template_slug!==bindingSlug),{template_slug:bindingSlug,target_workload:bindingTarget,status:"pending_validation",executor:"pod_chaosblade",overrides:{},evidence:{}}],null,2)})}catch(e){setError(api.errorMessage(e))}}}>添加/替换此模板绑定</button></div>
              <ConfigSection title="真实来源" description="事故/Issue 标题、URL、摘要及复现差异" value={configs.provenance} onChange={v=>setConfigs({...configs,provenance:v})}/>
              <ConfigSection title="模板绑定" description="每个模板独立维护目标和验收状态" value={configs.template_bindings} onChange={v=>setConfigs({...configs,template_bindings:v})}/>
              <div className="p-4 bg-ink-50 rounded-lg text-[12px] text-ink-500">
                ⚙️ 配置项表单化编辑（触发器 / 表现特征 / 修复验证 / 放大器 / 冻结规则）
                <br />
                <span className="text-ink-400">结构化配置通过 JSON Schema 渲染表单，MVP 阶段先展示原始 JSON 编辑器。</span>
              </div>

              <ConfigSection
                title="触发配置"
                description="故障触发条件和延迟"
                value={configs.trigger_config} onChange={v=>setConfigs({...configs,trigger_config:v})}
              />

              <ConfigSection
                title="表现特征"
                description="指标、日志、告警等表现形式"
                value={configs.manifestation_config} onChange={v=>setConfigs({...configs,manifestation_config:v})}
              />

              <ConfigSection
                title="修复验证"
                description="修复步骤和验证条件"
                value={configs.fix_config} onChange={v=>setConfigs({...configs,fix_config:v})}
              />

              <ConfigSection
                title="放大器配置"
                description="ChaosBlade 故障注入配置"
                value={configs.amplifier_config} onChange={v=>setConfigs({...configs,amplifier_config:v})}
              />

              <ConfigSection
                title="冻结规则"
                description="弹性冻结阈值"
                value={configs.freeze_rules} onChange={v=>setConfigs({...configs,freeze_rules:v})}
              />
            </div>
          )}
        </form>

        {editId&&<div className="px-5 py-3 border-t space-y-2"><p className="text-xs text-ink-500">保存配置后，在服务器逐条验证再填写验收证据。验收提交不会保存未保存的编辑。</p><textarea className="w-full font-mono border rounded p-2" rows={5} value={evidence} onChange={e=>setEvidence(e.target.value)}/><button type="button" disabled={submitting||!bindingSlug} onClick={async()=>{setSubmitting(true);try{await api.acceptBinding(editId,bindingSlug,JSON.parse(evidence));onSuccess()}catch(err){setError(api.errorMessage(err))}finally{setSubmitting(false)}}}>提交 {bindingSlug} 服务器验收</button></div>}
        {/* 底部按钮 */}
        <div className="px-5 py-3 border-t border-ink-100 flex items-center justify-end gap-3">
          <button
            type="button"
            onClick={onClose}
            className="h-9 px-5 rounded-lg border border-ink-200 text-ink-600 text-[13px] font-medium hover:bg-ink-50 transition-colors"
          >
            取消
          </button>
          <button
            type="submit" form="defect-config-form"
            disabled={submitting}
            className="h-9 px-5 rounded-lg bg-brand-500 hover:bg-brand-600 disabled:bg-brand-300 text-white text-[13px] font-medium transition-colors"
          >
            {submitting ? "保存中..." : "保存"}
          </button>
        </div>
      </div>
    </div>
  );
}

function FormRow({ label, required, children }: { label: string; required?: boolean; children: React.ReactNode }) {
  return (
    <div>
      <label className="block text-[12px] font-medium text-ink-700 mb-1.5">
        {label}
        {required && <span className="text-danger-500 ml-0.5">*</span>}
      </label>
      {children}
    </div>
  );
}

function ConfigSection({
  title,
  description,
  value, onChange,
}: {
  title: string;
  description: string;
  value:string;
  onChange:(value:string)=>void;
}) {
  const jsonText=value;
  const [error, setError] = useState("");

  const handleChange = (value: string) => {
    onChange(value);
    try {
      JSON.parse(value);
      setError("");
    } catch (e) {
      setError("JSON 格式错误");
    }
  };

  return (
    <div className="border border-ink-100 rounded-lg overflow-hidden">
      <div className="px-4 py-2.5 bg-ink-50 border-b border-ink-100">
        <div className="text-[13px] font-semibold text-ink-700">{title}</div>
        <div className="text-[11px] text-ink-500 mt-0.5">{description}</div>
      </div>
      <div className="p-3">
        <textarea
          value={jsonText}
          onChange={(e) => handleChange(e.target.value)}
          rows={6}
          className={`w-full px-3 py-2 rounded-lg border text-[12px] font-mono focus:outline-none focus:ring-2 focus:ring-brand-500/20 resize-none ${
            error ? "border-danger-300" : "border-ink-200"
          }`}
        />
        {error && <div className="text-[11px] text-danger-500 mt-1">{error}</div>}
      </div>
    </div>
  );
}
