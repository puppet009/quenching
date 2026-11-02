import { useEffect, useState } from "react";
import {
  createPitfallConfig,
  updatePitfallConfig,
  getPitfallConfig,
  PITFALL_TYPE_LABELS,
  type PitfallConfig,
  type PitfallType,
  type PitfallConfigStatus,
  type ConfidenceLevel,
  type SeverityLevel,
  type TriggerWhen,
} from "@quenching/shared";

interface PitfallConfigFormProps {
  editId: number | null; // null 表示新建
  initialDefectId?: number;
  onClose: () => void;
  onSuccess: () => void;
}

/**
 * 坑点配置表单弹窗（T3-19）
 *
 * 支持新建和编辑两种模式。
 * 表单分为四个 Tab：
 * - 基础信息：名称、类型、描述、严重度、排序
 * - 触发条件：时机、概率、前置状态
 * - 错误建议：错误内容、置信度、正确做法
 * - 判定规则：识别规则 + 踩坑规则
 */
export function PitfallConfigForm({ editId, initialDefectId, onClose, onSuccess }: PitfallConfigFormProps) {
  const [activeTab, setActiveTab] = useState<"basic" | "trigger" | "content" | "rules">("basic");
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  // 表单数据
  const [form, setForm] = useState<PitfallConfig>(() => ({...defaultConfig(), defect_id: initialDefectId ?? 0}));

  // 编辑模式下加载详情
  useEffect(() => {
    if (editId) {
      setLoading(true);
      setError("");
      getPitfallConfig(editId)
        .then((data) => {
          setForm(data);
        })
        .catch((err) => {
          console.error("加载配置详情失败:", err);
          setError("加载失败，请刷新重试");
        })
        .finally(() => setLoading(false));
    }
  }, [editId]);

  // 更新基础字段
  const updateField = <K extends keyof PitfallConfig>(key: K, value: PitfallConfig[K]) => {
    setForm((prev) => ({ ...prev, [key]: value }));
  };

  // 更新触发条件
  const updateTrigger = <K extends keyof PitfallConfig["trigger_condition"]>(
    key: K,
    value: PitfallConfig["trigger_condition"][K],
  ) => {
    setForm((prev) => ({
      ...prev,
      trigger_condition: { ...prev.trigger_condition, [key]: value },
    }));
  };

  // 更新识别规则
  const updateRecognition = <K extends keyof PitfallConfig["recognition_rules"]>(
    key: K,
    value: PitfallConfig["recognition_rules"][K],
  ) => {
    setForm((prev) => ({
      ...prev,
      recognition_rules: { ...prev.recognition_rules, [key]: value },
    }));
  };

  // 更新踩坑规则
  const updatePitfallRules = <K extends keyof PitfallConfig["pitfall_rules"]>(
    key: K,
    value: PitfallConfig["pitfall_rules"][K],
  ) => {
    setForm((prev) => ({
      ...prev,
      pitfall_rules: { ...prev.pitfall_rules, [key]: value },
    }));
  };

  // 数组字段（逗号分隔字符串）
  const arrayToStr = (arr?: string[]) => (arr ? arr.join(", ") : "");
  const strToArray = (str: string) =>
    str
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean);

  // 提交
  const handleSubmit = async () => {
    setSaving(true);
    setError("");
    try {
      // 基本校验
      if (!form.name.trim()) {
        setError("请输入坑点名称");
        return;
      }
      if (!form.defect_id || form.defect_id <= 0) {
        setError("请输入有效的缺陷 ID");
        return;
      }
      if (!form.error_content.trim()) {
        setError("请输入 AI 错误建议内容");
        return;
      }
      if (form.trigger_condition.probability < 0 || form.trigger_condition.probability > 1) {
        setError("触发概率必须在 0 到 1 之间");
        return;
      }

      const submitData = {
        defect_id: form.defect_id,
        pitfall_type: form.pitfall_type,
        name: form.name,
        description: form.description,
        trigger_condition: form.trigger_condition,
        error_content: form.error_content,
        confidence_level: form.confidence_level,
        recognition_rules: form.recognition_rules,
        pitfall_rules: form.pitfall_rules,
        correct_approach: form.correct_approach,
        severity: form.severity,
        status: form.status,
        sort_order: form.sort_order,
      };

      if (editId) {
        await updatePitfallConfig(editId, submitData);
      } else {
        await createPitfallConfig(submitData);
      }
      onSuccess();
    } catch (err: unknown) {
      console.error("保存失败:", err);
      const msg = err && typeof err === "object" && "message" in err ? String(err.message) : "保存失败";
      setError(msg);
    } finally {
      setSaving(false);
    }
  };

  const tabs = [
    { key: "basic", label: "基础信息", icon: "📋" },
    { key: "trigger", label: "触发条件", icon: "⚡" },
    { key: "content", label: "错误建议", icon: "💬" },
    { key: "rules", label: "判定规则", icon: "🎯" },
  ] as const;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
      <div className="bg-white rounded-2xl w-full max-w-3xl max-h-[90vh] flex flex-col shadow-2xl">
        {/* 头部 */}
        <div className="px-6 py-4 border-b border-ink-100 flex items-center justify-between flex-shrink-0">
          <div>
            <h2 className="text-base font-bold text-ink-900">
              {editId ? "编辑坑点配置" : "新建坑点配置"}
            </h2>
            <p className="text-xs text-ink-500 mt-0.5">
              {editId ? "修改坑点配置参数，保存后立即生效" : "创建新的 AI 坑点配置"}
            </p>
          </div>
          <button
            onClick={onClose}
            className="w-8 h-8 rounded-lg flex items-center justify-center text-ink-400 hover:bg-ink-100 hover:text-ink-600 transition-colors"
          >
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
              <path d="M18 6 6 18M6 6l12 12" />
            </svg>
          </button>
        </div>

        {/* Tab 导航 */}
        <div className="px-6 border-b border-ink-100 flex gap-1 flex-shrink-0">
          {tabs.map((tab) => (
            <button
              key={tab.key}
              onClick={() => setActiveTab(tab.key)}
              className={`px-4 py-3 text-sm font-medium border-b-2 transition-all -mb-px ${
                activeTab === tab.key
                  ? "border-brand-500 text-brand-600"
                  : "border-transparent text-ink-500 hover:text-ink-700"
              }`}
            >
              <span className="mr-1.5">{tab.icon}</span>
              {tab.label}
            </button>
          ))}
        </div>

        {/* 表单内容 */}
        <div className="flex-1 overflow-y-auto p-6">
          {loading ? (
            <div className="text-center py-12 text-ink-400">加载中...</div>
          ) : error && activeTab === "basic" ? (
            <div className="text-center py-12 text-danger-500">{error}</div>
          ) : (
            <div className="space-y-5">
              {activeTab === "basic" && (
                <>
                  <FormRow label="缺陷 ID" required>
                    <input
                      type="number"
                      value={form.defect_id || ""}
                      onChange={(e) => updateField("defect_id", parseInt(e.target.value) || 0)}
                      placeholder="关联的缺陷 ID"
                      className={inputCls}
                    />
                    <p className="text-xs text-ink-400 mt-1">该坑点将注入到指定缺陷的训练中</p>
                  </FormRow>

                  <FormRow label="坑点名称" required>
                    <input
                      type="text"
                      value={form.name}
                      onChange={(e) => updateField("name", e.target.value)}
                      placeholder="如：CPU 占用率误判"
                      className={inputCls}
                    />
                  </FormRow>

                  <FormRow label="坑点类型" required>
                    <div className="flex gap-2">
                      {(Object.keys(PITFALL_TYPE_LABELS) as PitfallType[]).map((type) => (
                        <button
                          key={type}
                          type="button"
                          onClick={() => updateField("pitfall_type", type)}
                          className={`flex-1 h-10 rounded-lg border text-sm font-medium transition-all ${
                            form.pitfall_type === type
                              ? "border-brand-500 bg-brand-50 text-brand-700"
                              : "border-ink-200 text-ink-600 hover:border-ink-300"
                          }`}
                        >
                          {PITFALL_TYPE_LABELS[type]}
                        </button>
                      ))}
                    </div>
                  </FormRow>

                  <FormRow label="描述">
                    <textarea
                      value={form.description || ""}
                      onChange={(e) => updateField("description", e.target.value)}
                      placeholder="简要描述这个坑点的特点和迷惑性"
                      rows={3}
                      className={`${inputCls} resize-none`}
                    />
                  </FormRow>

                  <div className="grid grid-cols-2 gap-4">
                    <FormRow label="严重程度">
                      <select
                        value={form.severity}
                        onChange={(e) => updateField("severity", e.target.value as SeverityLevel)}
                        className={inputCls}
                      >
                        <option value="low">低</option>
                        <option value="medium">中</option>
                        <option value="high">高</option>
                      </select>
                    </FormRow>
                    <FormRow label="排序">
                      <input
                        type="number"
                        value={form.sort_order}
                        onChange={(e) => updateField("sort_order", parseInt(e.target.value) || 0)}
                        className={inputCls}
                      />
                      <p className="text-xs text-ink-400 mt-1">数字越小越靠前</p>
                    </FormRow>
                  </div>

                  <FormRow label="状态">
                    <select
                      value={form.status}
                      onChange={(e) => updateField("status", e.target.value as PitfallConfigStatus)}
                      className={inputCls}
                    >
                      <option value="draft">草稿</option>
                      <option value="active">已启用</option>
                      <option value="inactive">已禁用</option>
                    </select>
                    <p className="text-xs text-ink-400 mt-1">
                      启用前系统会自动校验配置完整性
                    </p>
                  </FormRow>
                </>
              )}

              {activeTab === "trigger" && (
                <>
                  <FormRow label="触发时机" required>
                    <div className="grid grid-cols-2 gap-2">
                      {(["always", "question_match", "stage_enter", "delay"] as TriggerWhen[]).map((when) => (
                        <button
                          key={when}
                          type="button"
                          onClick={() => updateTrigger("when", when)}
                          className={`h-10 rounded-lg border text-sm font-medium transition-all ${
                            form.trigger_condition.when === when
                              ? "border-brand-500 bg-brand-50 text-brand-700"
                              : "border-ink-200 text-ink-600 hover:border-ink-300"
                          }`}
                        >
                          {when === "always" && "始终触发"}
                          {when === "question_match" && "提问匹配"}
                          {when === "stage_enter" && "阶段进入"}
                          {when === "delay" && "延迟触发"}
                        </button>
                      ))}
                    </div>
                  </FormRow>

                  <div className="grid grid-cols-2 gap-4">
                    <FormRow label="触发概率">
                      <input
                        type="number"
                        step="0.1"
                        min="0"
                        max="1"
                        value={form.trigger_condition.probability}
                        onChange={(e) => updateTrigger("probability", parseFloat(e.target.value) || 0)}
                        className={inputCls}
                      />
                      <p className="text-xs text-ink-400 mt-1">0-1 之间，1 表示 100% 触发</p>
                    </FormRow>
                    <FormRow label="最大注入次数">
                      <input
                        type="number"
                        min="1"
                        value={form.trigger_condition.max_times || 1}
                        onChange={(e) => updateTrigger("max_times", parseInt(e.target.value) || 1)}
                        className={inputCls}
                      />
                      <p className="text-xs text-ink-400 mt-1">同一次训练内最多注入几次</p>
                    </FormRow>
                  </div>

                  {form.trigger_condition.when === "question_match" && (
                    <>
                      <FormRow label="触发关键词">
                        <input
                          type="text"
                          value={arrayToStr(form.trigger_condition.question_keywords)}
                          onChange={(e) =>
                            updateTrigger("question_keywords", strToArray(e.target.value))
                          }
                          placeholder="多个关键词用逗号分隔"
                          className={inputCls}
                        />
                        <p className="text-xs text-ink-400 mt-1">用户提问命中任一关键词即触发</p>
                      </FormRow>
                    </>
                  )}

                  {form.trigger_condition.when === "stage_enter" && (
                    <FormRow label="触发阶段">
                      <input
                        type="text"
                        value={arrayToStr(form.trigger_condition.stages)}
                        onChange={(e) => updateTrigger("stages", strToArray(e.target.value))}
                        placeholder="如 detect, locate, fix，逗号分隔"
                        className={inputCls}
                      />
                    </FormRow>
                  )}

                  {form.trigger_condition.when === "delay" && (
                    <FormRow label="延迟秒数">
                      <input
                        type="number"
                        min="0"
                        value={form.trigger_condition.delay_seconds || 0}
                        onChange={(e) =>
                          updateTrigger("delay_seconds", parseInt(e.target.value) || 0)
                        }
                        className={inputCls}
                      />
                      <p className="text-xs text-ink-400 mt-1">训练开始后多少秒触发</p>
                    </FormRow>
                  )}
                </>
              )}

              {activeTab === "content" && (
                <>
                  <FormRow label="AI 错误建议内容" required>
                    <textarea
                      value={form.error_content}
                      onChange={(e) => updateField("error_content", e.target.value)}
                      placeholder="AI 作为错误建议输出的内容..."
                      rows={6}
                      className={`${inputCls} resize-none font-mono text-xs`}
                    />
                    <p className="text-xs text-ink-400 mt-1">
                      这是 AI 会注入到对话中的错误建议原文
                    </p>
                  </FormRow>

                  <FormRow label="置信度表现">
                    <select
                      value={form.confidence_level}
                      onChange={(e) =>
                        updateField("confidence_level", e.target.value as ConfidenceLevel)
                      }
                      className={inputCls}
                    >
                      <option value="low">低</option>
                      <option value="medium">中</option>
                      <option value="high">高</option>
                      <option value="random">随机</option>
                    </select>
                    <p className="text-xs text-ink-400 mt-1">
                      AI 表达建议时的自信程度；随机可避免用户"越自信越假"的规律
                    </p>
                  </FormRow>

                  <FormRow label="正确做法">
                    <textarea
                      value={form.correct_approach || ""}
                      onChange={(e) => updateField("correct_approach", e.target.value)}
                      placeholder="正确的排查思路或解决方法"
                      rows={4}
                      className={`${inputCls} resize-none`}
                    />
                    <p className="text-xs text-ink-400 mt-1">
                      用于复盘时展示正确做法，帮助用户学习
                    </p>
                  </FormRow>
                </>
              )}

              {activeTab === "rules" && (
                <>
                  <div className="mb-4">
                    <h4 className="text-sm font-semibold text-ink-800 mb-3">🔍 识别规则</h4>
                    <p className="text-xs text-ink-500 mb-4">
                      用户出现以下任一行为即视为"识别了坑点"
                    </p>
                  </div>

                  <FormRow label="反驳关键词">
                    <input
                      type="text"
                      value={arrayToStr(form.recognition_rules.dispute_keywords)}
                      onChange={(e) =>
                        updateRecognition("dispute_keywords", strToArray(e.target.value))
                      }
                      placeholder="如：不对, 为什么, 证据呢，逗号分隔"
                      className={inputCls}
                    />
                    <p className="text-xs text-ink-400 mt-1">
                      用户回复中包含这些词就算提出质疑
                    </p>
                  </FormRow>

                  <FormRow label="独立验证操作">
                    <input
                      type="text"
                      value={arrayToStr(form.recognition_rules.independent_verify_actions)}
                      onChange={(e) =>
                        updateRecognition("independent_verify_actions", strToArray(e.target.value))
                      }
                      placeholder="如：check_metrics, check_logs，逗号分隔"
                      className={inputCls}
                    />
                    <p className="text-xs text-ink-400 mt-1">
                      用户主动去验证其他证据方向
                    </p>
                  </FormRow>

                  <FormRow label="忽略并继续">
                    <label className="flex items-center gap-2 cursor-pointer">
                      <input
                        type="checkbox"
                        checked={!!form.recognition_rules.ignore_and_proceed}
                        onChange={(e) =>
                          updateRecognition("ignore_and_proceed", e.target.checked)
                        }
                        className="w-4 h-4 rounded border-ink-300 text-brand-500 focus:ring-brand-500/20"
                      />
                      <span className="text-sm text-ink-600">
                        用户不采纳建议、继续沿其他方向排查也算识别
                      </span>
                    </label>
                  </FormRow>

                  <div className="border-t border-ink-100 my-5" />

                  <div className="mb-4">
                    <h4 className="text-sm font-semibold text-ink-800 mb-3">⚠️ 踩坑规则</h4>
                    <p className="text-xs text-ink-500 mb-4">
                      用户出现以下任一行为即视为"踩了坑"
                    </p>
                  </div>

                  <FormRow label="采纳操作">
                    <input
                      type="text"
                      value={arrayToStr(form.pitfall_rules.adopt_actions)}
                      onChange={(e) =>
                        updatePitfallRules("adopt_actions", strToArray(e.target.value))
                      }
                      placeholder="如：exec_cmd, modify_config，逗号分隔"
                      className={inputCls}
                    />
                    <p className="text-xs text-ink-400 mt-1">
                      用户执行了这些操作就算采纳错误建议
                    </p>
                  </FormRow>

                  <FormRow label="采纳关键词">
                    <input
                      type="text"
                      value={arrayToStr(form.pitfall_rules.adopt_keywords)}
                      onChange={(e) =>
                        updatePitfallRules("adopt_keywords", strToArray(e.target.value))
                      }
                      placeholder="如：好的, 就按你说的, 我执行了，逗号分隔"
                      className={inputCls}
                    />
                  </FormRow>

                  <FormRow label="采纳后故障恶化">
                    <label className="flex items-center gap-2 cursor-pointer">
                      <input
                        type="checkbox"
                        checked={!!form.pitfall_rules.worsen_fault}
                        onChange={(e) => updatePitfallRules("worsen_fault", e.target.checked)}
                        className="w-4 h-4 rounded border-ink-300 text-brand-500 focus:ring-brand-500/20"
                      />
                      <span className="text-sm text-ink-600">
                        踩坑后故障会进一步恶化（T3-12 后果传导）
                      </span>
                    </label>
                  </FormRow>
                </>
              )}
            </div>
          )}
        </div>

        {/* 底部操作栏 */}
        <div className="px-6 py-4 border-t border-ink-100 flex items-center justify-between flex-shrink-0">
          <div className="text-xs text-danger-500">{error && activeTab !== "basic" ? error : ""}</div>
          <div className="flex gap-2">
            <button
              onClick={onClose}
              className="h-9 px-5 rounded-lg border border-ink-200 text-ink-600 text-sm font-medium hover:bg-ink-50 transition-colors"
            >
              取消
            </button>
            <button
              onClick={handleSubmit}
              disabled={saving || loading}
              className="h-9 px-6 rounded-lg bg-brand-500 hover:bg-brand-600 text-white text-sm font-medium transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {saving ? "保存中..." : editId ? "保存修改" : "创建配置"}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

// ---------- 辅助组件 ----------

const inputCls =
  "w-full h-9 px-3 rounded-lg border border-ink-200 text-sm focus:outline-none focus:ring-2 focus:ring-brand-500/20 focus:border-brand-500";

interface FormRowProps {
  label: string;
  required?: boolean;
  children: React.ReactNode;
}

function FormRow({ label, required, children }: FormRowProps) {
  return (
    <div>
      <label className="block text-sm font-medium text-ink-700 mb-1.5">
        {label}
        {required && <span className="text-danger-500 ml-0.5">*</span>}
      </label>
      {children}
    </div>
  );
}

// ---------- 默认配置 ----------

function defaultConfig(): PitfallConfig {
  return {
    id: 0,
    defect_id: 0,
    pitfall_type: "causal_misjudgment",
    name: "",
    description: "",
    trigger_condition: {
      when: "always",
      probability: 1,
      max_times: 1,
    },
    error_content: "",
    confidence_level: "random",
    recognition_rules: {
      dispute_keywords: [],
    },
    pitfall_rules: {
      adopt_keywords: [],
    },
    correct_approach: "",
    severity: "medium",
    status: "draft",
    sort_order: 1,
  };
}
