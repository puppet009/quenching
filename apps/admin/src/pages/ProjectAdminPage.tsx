import { useEffect, useState, useRef } from "react";
import {errorMessage} from "../services/data";
import {OfficialTemplatesPanel} from "../components/OfficialTemplatesPanel";
import {
  listProjects,
  createProject,
  updateProject,
  deleteProject,
  updateProjectStatus,
  DOMAIN_LABELS,
  DIFFICULTY_LABELS,
  DIFFICULTY_COLORS,
  STATUS_LABELS,
  STATUS_COLORS,
  type Project,
  type ProjectStatus,
  type DifficultyLevel,
  type CreateProjectParams,
} from "@quenching/shared";

/**
 * 项目管理后台页（T4-10）
 *
 * 功能：
 * - 项目列表（筛选 + 分页）
 * - 项目上下线
 * - 新建/编辑项目（信息、技术栈、架构图、难度、排序）
 * - 删除项目
 *
 * 访问路径: /admin/projects
 */
export function ProjectAdminPage() {
  // 筛选
  const [keyword, setKeyword] = useState("");
  const [domain, setDomain] = useState("");
  const [status, setStatus] = useState("");
  const [difficulty, setDifficulty] = useState("");

  // 分页
  const [page, setPage] = useState(1);
  const [pageSize] = useState(10);
  const [total, setTotal] = useState(0);

  // 数据
  const [items, setItems] = useState<Project[]>([]);
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
      const params={page,size:pageSize,keyword:keyword||undefined,domain:domain||undefined,status:status||undefined,difficulty:difficulty||undefined};
      const result=await listProjects(params);
      if(live&&version===requestVersion.current){setItems(result.list);setTotal(result.total)}
    }catch(err){if(live&&version===requestVersion.current){setItems([]);setTotal(0);setListError(errorMessage(err))}}
    finally{if(live&&version===requestVersion.current)setLoading(false)}};
    void read();return()=>{live=false};
  },[page,domain,status,difficulty,keyword,refresh]);
  const handleFilterChange=()=>{setPage(1);loadList()};

  // 新建
  const handleCreate = () => {
    setEditingId(null);
    setShowForm(true);
  };

  // 编辑
  const handleEdit = (item: Project) => {
    setEditingId(item.id);
    setShowForm(true);
  };

  // 删除确认
  const handleDelete = async (id: number) => {
    if (!confirm("确定要删除这个项目吗？此操作不可撤销。")) return;
    try {
      await deleteProject(id);
      loadList();
    } catch (err) {
      console.error("删除失败:", err);
      alert("删除失败，请重试");
    }
  };

  // 上下线切换
  const handleToggleStatus = async (item: Project) => {
    const newStatus: ProjectStatus = item.status === "online" ? "offline" : "online";
    try {
      await updateProjectStatus(item.id, newStatus);
      loadList();
    } catch (err) {
      console.error("状态更新失败:", err);
      alert("状态更新失败，请重试");
    }
  };

  const totalPages = Math.ceil(total / pageSize);

  return (
    <div className="min-h-screen bg-ink-50">
      {listError&&<div role="alert" className="p-3 bg-danger-50 text-danger-600">{listError}<button className="ml-3 underline" onClick={loadList}>重试</button></div>}
      {/* 顶部栏 */}
      <div className="bg-white border-b border-ink-100 sticky top-0 z-10">
        <div className="max-w-7xl mx-auto px-6 py-4">
          <div className="flex items-center justify-between">
            <div>
              <h1 className="text-lg font-bold text-ink-900">项目管理</h1>
              <p className="text-xs text-ink-400 mt-0.5">
                查看官方模板与维护登记项目
              </p>
            </div>
            <button
              onClick={handleCreate}
              className="h-10 px-4 rounded-xl bg-brand-500 hover:bg-brand-600 text-white text-sm font-semibold transition-colors flex items-center gap-2"
            >
              <span>+</span>
              新建项目
            </button>
          </div>
        </div>
      </div>

      <div className="max-w-7xl mx-auto px-6 py-6">
        <OfficialTemplatesPanel refresh={refresh} />
        <div className="mb-3">
          <h2 className="font-semibold text-ink-900">登记项目</h2>
          <p className="mt-1 text-xs text-ink-500">{listError ? "列表加载失败" : loading ? "加载中…" : `共 ${total} 个登记项目`}</p>
        </div>
        {/* 筛选栏 */}
        <div className="bg-white rounded-2xl border border-ink-100 shadow-sm p-4 mb-5">
          <div className="flex flex-wrap items-center gap-3">
            {/* 搜索 */}
            <div className="relative flex-1 min-w-[200px]">
              <span className="absolute left-3 top-1/2 -translate-y-1/2 text-ink-400">
                🔍
              </span>
              <input
                type="text"
                placeholder="搜索项目名称..."
                value={keyword}
                onChange={(e) => {setKeyword(e.target.value);setPage(1)}}
                onKeyDown={(e) => e.key === "Enter" && handleFilterChange()}
                className="w-full h-10 pl-10 pr-3 rounded-xl border border-ink-200 text-sm text-ink-700 focus:outline-none focus:border-brand-400 focus:ring-2 focus:ring-brand-100 transition-all"
              />
            </div>

            {/* 领域筛选 */}
            <select
              value={domain}
              onChange={(e) => {
                setDomain(e.target.value);
                setPage(1);
              }}
              className="h-10 px-3 rounded-xl border border-ink-200 text-sm text-ink-700 focus:outline-none focus:border-brand-400 bg-white"
            >
              <option value="">全部领域</option>
              {Object.entries(DOMAIN_LABELS).map(([k, v]) => (
                <option key={k} value={k}>
                  {v}
                </option>
              ))}
            </select>

            {/* 难度筛选 */}
            <select
              value={difficulty}
              onChange={(e) => {
                setDifficulty(e.target.value);
                setPage(1);
              }}
              className="h-10 px-3 rounded-xl border border-ink-200 text-sm text-ink-700 focus:outline-none focus:border-brand-400 bg-white"
            >
              <option value="">全部难度</option>
              {Object.entries(DIFFICULTY_LABELS).map(([k, v]) => (
                <option key={k} value={k}>
                  {v}
                </option>
              ))}
            </select>

            {/* 状态筛选 */}
            <select
              value={status}
              onChange={(e) => {
                setStatus(e.target.value);
                setPage(1);
              }}
              className="h-10 px-3 rounded-xl border border-ink-200 text-sm text-ink-700 focus:outline-none focus:border-brand-400 bg-white"
            >
              <option value="">全部状态</option>
              {Object.entries(STATUS_LABELS).map(([k, v]) => (
                <option key={k} value={k}>
                  {v}
                </option>
              ))}
            </select>
          </div>
        </div>

        {/* 列表 */}
        <div className="bg-white rounded-2xl border border-ink-100 shadow-sm overflow-hidden">
          {/* 表头 */}
          <div className="grid grid-cols-12 gap-3 px-5 py-3 bg-ink-50 border-b border-ink-100 text-xs font-medium text-ink-500">
            <div className="col-span-3">项目名称</div>
            <div className="col-span-2">领域</div>
            <div className="col-span-1 text-center">难度</div>
            <div className="col-span-2">技术栈</div>
            <div className="col-span-1 text-center">状态</div>
            <div className="col-span-1 text-center">排序</div>
            <div className="col-span-2 text-right">操作</div>
          </div>

          {/* 列表项 */}
          {loading ? (
            <div className="p-10 text-center text-ink-400">加载中...</div>
          ) : items.length > 0 ? (
            <div className="divide-y divide-ink-50">
              {items.map((item) => (
                <ProjectRow
                  key={item.id}
                  item={item}
                  onEdit={handleEdit}
                  onDelete={handleDelete}
                  onToggleStatus={handleToggleStatus}
                />
              ))}
            </div>
          ) : (
            <div className="p-10 text-center">
              <div className="text-4xl mb-3">📭</div>
              <div className="text-sm text-ink-500">{listError ? "登记项目列表加载失败，请重试" : "暂无登记项目"}</div>
            </div>
          )}
        </div>

        {/* 分页 */}
        {total > 0 && (
          <div className="flex items-center justify-between mt-6">
            <div className="text-xs text-ink-400">
              共 <span className="font-semibold text-ink-600">{total}</span> 条
            </div>
            <div className="flex items-center gap-1">
              <button
                onClick={() => setPage((p) => Math.max(1, p - 1))}
                disabled={page <= 1}
                className="w-8 h-8 rounded-lg border border-ink-200 text-ink-500 text-sm hover:bg-ink-50 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
              >
                ←
              </button>
              {Array.from({ length: Math.min(totalPages, 7) }, (_, i) => {
                let p: number;
                if (totalPages <= 7) {
                  p = i + 1;
                } else if (page <= 4) {
                  p = i + 1;
                } else if (page >= totalPages - 3) {
                  p = totalPages - 6 + i;
                } else {
                  p = page - 3 + i;
                }
                return (
                  <button
                    key={p}
                    onClick={() => setPage(p)}
                    className={`w-8 h-8 rounded-lg text-sm font-medium transition-colors ${
                      p === page
                        ? "bg-brand-500 text-white"
                        : "border border-ink-200 text-ink-600 hover:bg-ink-50"
                    }`}
                  >
                    {p}
                  </button>
                );
              })}
              <button
                onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                disabled={page >= totalPages}
                className="w-8 h-8 rounded-lg border border-ink-200 text-ink-500 text-sm hover:bg-ink-50 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
              >
                →
              </button>
            </div>
          </div>
        )}
      </div>

      {/* 新建/编辑弹窗 */}
      {showForm && (
        <ProjectFormModal
          editId={editingId}
          items={items}
          onClose={() => setShowForm(false)}
          onSuccess={() => {
            setShowForm(false);
            loadList();
          }}
        />
      )}
    </div>
  );
}

/* ===== 子组件 ===== */

function ProjectRow({
  item,
  onEdit,
  onDelete,
  onToggleStatus,
}: {
  item: Project;
  onEdit: (item: Project) => void;
  onDelete: (id: number) => void;
  onToggleStatus: (item: Project) => void;
}) {
  return (
    <div className="grid grid-cols-12 gap-3 px-5 py-4 items-center hover:bg-ink-50/60 transition-colors">
      {/* 项目名 */}
      <div className="col-span-3">
        <div className="text-sm font-semibold text-ink-800">{item.name}</div>
        {item.description && (
          <div className="text-xs text-ink-400 mt-0.5 truncate">
            {item.description}
          </div>
        )}
      </div>

      {/* 领域 */}
      <div className="col-span-2 text-sm text-ink-600">
        {DOMAIN_LABELS[item.domain as keyof typeof DOMAIN_LABELS] || item.domain}
      </div>

      {/* 难度 */}
      <div className="col-span-1 text-center">
        <span
          className={`inline-block px-2 py-0.5 rounded text-[11px] font-medium ${DIFFICULTY_COLORS[item.difficulty]}`}
        >
          {DIFFICULTY_LABELS[item.difficulty]}
        </span>
      </div>

      {/* 技术栈 */}
      <div className="col-span-2">
        <div className="flex flex-wrap gap-1">
          {item.tech_stack?.slice(0, 3).map((t) => (
            <span
              key={t}
              className="px-1.5 py-0.5 rounded bg-ink-100 text-ink-600 text-[10px]"
            >
              {t}
            </span>
          ))}
          {(item.tech_stack?.length ?? 0) > 3 && (
            <span className="px-1.5 py-0.5 text-[10px] text-ink-400">
              +{(item.tech_stack?.length ?? 0) - 3}
            </span>
          )}
        </div>
      </div>

      {/* 状态 */}
      <div className="col-span-1 text-center">
        <span
          className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-medium ${STATUS_COLORS[item.status]}`}
        >
          <span className="w-1.5 h-1.5 rounded-full bg-current" />
          {STATUS_LABELS[item.status]}
        </span>
      </div>

      {/* 排序 */}
      <div className="col-span-1 text-center text-sm text-ink-500">
        #{item.sort_order}
      </div>

      {/* 操作 */}
      <div className="col-span-2 flex justify-end gap-2">
        <button
          onClick={() => onToggleStatus(item)}
          className={`h-8 px-3 rounded-lg text-xs font-medium transition-colors ${
            item.status === "online"
              ? "text-ink-500 hover:bg-ink-100"
              : "text-ok-600 hover:bg-ok-50"
          }`}
        >
          {item.status === "online" ? "下线" : "上线"}
        </button>
        <button
          onClick={() => onEdit(item)}
          className="h-8 px-3 rounded-lg border border-ink-200 text-ink-600 text-xs font-medium hover:bg-ink-50 transition-colors"
        >
          编辑
        </button>
        <button
          onClick={() => onDelete(item.id)}
          className="h-8 px-3 rounded-lg text-danger-600 text-xs font-medium hover:bg-danger-50 transition-colors"
        >
          删除
        </button>
      </div>
    </div>
  );
}

/* ===== 新建/编辑弹窗 ===== */

function ProjectFormModal({
  editId,
  items,
  onClose,
  onSuccess,
}: {
  editId: number | null;
  items: Project[];
  onClose: () => void;
  onSuccess: () => void;
}) {
  const existing = editId ? items.find((p) => p.id === editId) : null;

  const [form, setForm] = useState<CreateProjectParams & { id?: number }>(
    existing
      ? {
          id: existing.id,
          name: existing.name,
          domain: existing.domain,
          description: existing.description,
          tech_stack: existing.tech_stack,
          architecture_image: existing.architecture_image,
          difficulty: existing.difficulty,
          sort_order: existing.sort_order,
        }
      : {
          name: "",
          domain: "ecommerce",
          description: "",
          tech_stack: [],
          architecture_image: "",
          difficulty: "medium",
          sort_order: 0,
        },
  );
  const [techInput, setTechInput] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const isEdit = editId !== null;

  // 添加技术栈
  const addTech = () => {
    const t = techInput.trim();
    if (!t) return;
    if (form.tech_stack?.includes(t)) {
      setTechInput("");
      return;
    }
    setForm((f) => ({ ...f, tech_stack: [...(f.tech_stack ?? []), t] }));
    setTechInput("");
  };

  // 移除技术栈
  const removeTech = (t: string) => {
    setForm((f) => ({
      ...f,
      tech_stack: (f.tech_stack ?? []).filter((x) => x !== t),
    }));
  };

  // 提交
  const handleSubmit = async () => {
    if (!form.name.trim()) {
      setError("请输入项目名称");
      return;
    }
    if (!form.domain) {
      setError("请选择业务领域");
      return;
    }

    setSaving(true);
    setError("");

    try {
      if (isEdit && form.id) {
        await updateProject(form.id, {
          name: form.name,
          domain: form.domain,
          description: form.description,
          tech_stack: form.tech_stack,
          architecture_image: form.architecture_image,
          difficulty: form.difficulty as DifficultyLevel,
          sort_order: form.sort_order,
        });
      } else {
        await createProject({
          name: form.name,
          domain: form.domain,
          description: form.description,
          tech_stack: form.tech_stack,
          architecture_image: form.architecture_image,
          difficulty: form.difficulty as DifficultyLevel,
          sort_order: form.sort_order,
        });
      }
      onSuccess();
    } catch (err) {
      console.error("保存失败:", err);
      setError(errorMessage(err));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      {/* 遮罩 */}
      <div
        className="absolute inset-0 bg-black/40 backdrop-blur-sm"
        onClick={onClose}
      />

      {/* 弹窗 */}
      <div className="relative bg-white rounded-2xl shadow-2xl w-full max-w-2xl max-h-[90vh] flex flex-col overflow-hidden">
        {/* 头部 */}
        <div className="px-6 py-4 border-b border-ink-100 flex items-center justify-between">
          <h2 className="text-base font-bold text-ink-900">
            {isEdit ? "编辑项目" : "新建项目"}
          </h2>
          <button
            onClick={onClose}
            className="w-8 h-8 rounded-lg hover:bg-ink-50 flex items-center justify-center text-ink-400 text-lg"
          >
            ×
          </button>
        </div>

        {/* 表单内容 */}
        <div className="flex-1 overflow-y-auto p-6 space-y-5">
          {error && (
            <div className="p-3 bg-danger-50 text-danger-600 rounded-xl text-sm">
              {error}
            </div>
          )}

          {/* 项目名称 */}
          <div>
            <label className="block text-sm font-medium text-ink-700 mb-1.5">
              项目名称 <span className="text-danger-500">*</span>
            </label>
            <input
              type="text"
              value={form.name}
              onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
              placeholder="请输入项目名称"
              className="w-full h-10 px-3 rounded-xl border border-ink-200 text-sm text-ink-700 focus:outline-none focus:border-brand-400 focus:ring-2 focus:ring-brand-100 transition-all"
            />
          </div>

          <div className="grid grid-cols-3 gap-4">
            {/* 业务领域 */}
            <div>
              <label className="block text-sm font-medium text-ink-700 mb-1.5">
                业务领域
              </label>
              <select
                value={form.domain}
                onChange={(e) =>
                  setForm((f) => ({ ...f, domain: e.target.value }))
                }
                className="w-full h-10 px-3 rounded-xl border border-ink-200 text-sm text-ink-700 focus:outline-none focus:border-brand-400 bg-white"
              >
                {Object.entries(DOMAIN_LABELS).map(([k, v]) => (
                  <option key={k} value={k}>
                    {v}
                  </option>
                ))}
              </select>
            </div>

            {/* 难度 */}
            <div>
              <label className="block text-sm font-medium text-ink-700 mb-1.5">
                难度等级
              </label>
              <select
                value={form.difficulty}
                onChange={(e) =>
                  setForm((f) => ({
                    ...f,
                    difficulty: e.target.value as DifficultyLevel,
                  }))
                }
                className="w-full h-10 px-3 rounded-xl border border-ink-200 text-sm text-ink-700 focus:outline-none focus:border-brand-400 bg-white"
              >
                {Object.entries(DIFFICULTY_LABELS).map(([k, v]) => (
                  <option key={k} value={k}>
                    {v}
                  </option>
                ))}
              </select>
            </div>

            {/* 排序 */}
            <div>
              <label className="block text-sm font-medium text-ink-700 mb-1.5">
                排序权重
              </label>
              <input
                type="number"
                value={form.sort_order ?? 0}
                onChange={(e) =>
                  setForm((f) => ({
                    ...f,
                    sort_order: parseInt(e.target.value) || 0,
                  }))
                }
                className="w-full h-10 px-3 rounded-xl border border-ink-200 text-sm text-ink-700 focus:outline-none focus:border-brand-400 transition-all"
              />
            </div>
          </div>

          {/* 项目简介 */}
          <div>
            <label className="block text-sm font-medium text-ink-700 mb-1.5">
              项目简介
            </label>
            <textarea
              value={form.description}
              onChange={(e) =>
                setForm((f) => ({ ...f, description: e.target.value }))
              }
              placeholder="简要描述项目背景和技术特点"
              rows={3}
              className="w-full px-3 py-2 rounded-xl border border-ink-200 text-sm text-ink-700 focus:outline-none focus:border-brand-400 focus:ring-2 focus:ring-brand-100 transition-all resize-none"
            />
          </div>

          {/* 技术栈 */}
          <div>
            <label className="block text-sm font-medium text-ink-700 mb-1.5">
              技术栈
            </label>
            <div className="flex gap-2 mb-2">
              <input
                type="text"
                value={techInput}
                onChange={(e) => setTechInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    e.preventDefault();
                    addTech();
                  }
                }}
                placeholder="输入技术栈，回车添加"
                className="flex-1 h-10 px-3 rounded-xl border border-ink-200 text-sm text-ink-700 focus:outline-none focus:border-brand-400 focus:ring-2 focus:ring-brand-100 transition-all"
              />
              <button
                onClick={addTech}
                className="h-10 px-4 rounded-xl bg-ink-100 text-ink-600 text-sm font-medium hover:bg-ink-200 transition-colors"
              >
                添加
              </button>
            </div>
            <div className="flex flex-wrap gap-2">
              {form.tech_stack?.map((t) => (
                <span
                  key={t}
                  className="inline-flex items-center gap-1 px-2.5 py-1 rounded-lg bg-brand-50 text-brand-700 text-xs font-medium"
                >
                  {t}
                  <button
                    onClick={() => removeTech(t)}
                    className="w-4 h-4 rounded-full hover:bg-brand-100 flex items-center justify-center text-brand-500"
                  >
                    ×
                  </button>
                </span>
              ))}
              {(!form.tech_stack || form.tech_stack.length === 0) && (
                <span className="text-xs text-ink-400">暂未添加技术栈</span>
              )}
            </div>
          </div>

          {/* 架构图 URL */}
          <div>
            <label className="block text-sm font-medium text-ink-700 mb-1.5">
              架构图 URL
            </label>
            <input
              type="text"
              value={form.architecture_image}
              onChange={(e) =>
                setForm((f) => ({ ...f, architecture_image: e.target.value }))
              }
              placeholder="https://..."
              className="w-full h-10 px-3 rounded-xl border border-ink-200 text-sm text-ink-700 focus:outline-none focus:border-brand-400 focus:ring-2 focus:ring-brand-100 transition-all"
            />
            {form.architecture_image && (
              <div className="mt-2 p-3 bg-ink-50 rounded-xl border border-ink-100">
                <img
                  src={form.architecture_image}
                  alt="架构图预览"
                  className="max-h-40 mx-auto rounded"
                  onError={(e) => {
                    (e.target as HTMLImageElement).style.display = "none";
                  }}
                />
              </div>
            )}
          </div>
        </div>

        {/* 底部操作 */}
        <div className="px-6 py-4 border-t border-ink-100 flex justify-end gap-3">
          <button
            onClick={onClose}
            className="h-10 px-5 rounded-xl border border-ink-200 text-ink-600 text-sm font-medium hover:bg-ink-50 transition-colors"
          >
            取消
          </button>
          <button
            onClick={handleSubmit}
            disabled={saving}
            className="h-10 px-5 rounded-xl bg-brand-500 hover:bg-brand-600 text-white text-sm font-semibold transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {saving ? "保存中..." : isEdit ? "保存修改" : "创建项目"}
          </button>
        </div>
      </div>
    </div>
  );
}
