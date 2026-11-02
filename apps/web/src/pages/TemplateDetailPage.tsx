import { useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  listTemplates,
  domainLabel,
  tokenStore,
  type Template,
} from "@quenching/shared";
import { launchTemplate } from "../services/templateLaunch";
import { TemplateCover } from "../components/TemplateCover";
import { createSession, fetchSessionDetail } from "../services/session";
import { createSandbox, fetchSandboxProgress, waitUntilReady, type SandboxProgress } from "../services/sandbox";

/**
 * 模板详情页（基于 slug，数据来自 /templates 文件注册表）。
 * 展示项目介绍、业务场景、训练目标、技术栈与组件职责。
 * 开始训练：创建会话(template_slug) → 创建沙箱(template_slug) → 轮询就绪 → 跳训练页。
 */
export function TemplateDetailPage() {
  const { slug } = useParams<{ slug: string }>();
  const navigate = useNavigate();
  const [template, setTemplate] = useState<Template | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const [pending, setPending] = useState(() => Number(localStorage.getItem(`quenching_pending_template_${slug}`)) || undefined);
  const [starting, setStarting] = useState(false);
  const [startError, setStartError] = useState("");
  const [percent, setPercent] = useState(0);
  const [stage, setStage] = useState("");

  useEffect(() => {
    setLoading(true);
    listTemplates()
      .then((list) => {
        const t = (list ?? []).find((x) => x.slug === slug);
        if (!t) {
          setError("模板不存在或已停用");
          setTemplate(null);
        } else {
          setTemplate(t);
        }
      })
      .catch((e) => setError(e?.message ?? "加载失败"))
      .finally(() => setLoading(false));
  }, [slug]);

  // 开始训练：登录校验 → 创建会话(slug) → 创建沙箱(slug) → 轮询就绪 → 跳训练页。
  const handleStart = async () => {
    if (!slug || starting) return;
    if (!tokenStore.get()) {
      navigate("/login", { state: { from: `/projects/t/${slug}` } });
      return;
    }
    setStarting(true);
    setStartError("");
    setPercent(0);
    setStage("初始化中");
    try {
      const difficulty = template?.difficulty || "medium";
      const onProgress = (p: SandboxProgress) => {
        setPercent(p.percent);
        setStage(p.stage_name || p.message || "启动中");
      };
      const key = `quenching_pending_template_${slug}`;
      const sessionID = await launchTemplate({
        read: () => Number(localStorage.getItem(key)) || undefined,
        remember: id => { localStorage.setItem(key, String(id)); setPending(id); },
        clear: () => { localStorage.removeItem(key); setPending(undefined); },
        detail: fetchSessionDetail, progress: fetchSandboxProgress,
        create: () => createSession({ project_id: 0, template_slug: slug, difficulty }),
        sandbox: id => createSandbox({ session_id: id, project_id: 0, template_slug: slug }),
        wait: id => waitUntilReady(id, onProgress),
      });
      navigate(`/training/0?session=${sessionID}&sandbox=${sessionID}&template=${slug}`);
    } catch (e) {
      setStartError(e instanceof Error ? e.message : "启动失败，请稍后重试");
    } finally {
      setStarting(false);
    }
  };

  return (
    <div className="bg-ink-50 min-h-screen">
      <main className="page-container py-10">
        {/* 面包屑 */}
        <div className="mb-6 flex items-center gap-2 text-[13px] text-ink-400">
          <Link to="/projects" className="hover:text-ink-600">项目大厅</Link>
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="m9 18 6-6-6-6" />
          </svg>
          <span className="text-ink-600 font-medium truncate">{loading ? "加载中..." : template?.name ?? "模板详情"}</span>
        </div>

        {loading && (
          <div className="bg-white rounded-2xl border border-ink-100 p-8 animate-pulse">
            <div className="h-8 bg-ink-100 rounded w-1/3 mb-4" />
            <div className="h-4 bg-ink-100 rounded w-2/3" />
          </div>
        )}

        {!loading && error && (
          <div className="text-center py-24">
            <h3 className="text-lg font-bold text-ink-700 mb-2">{error}</h3>
            <Link to="/projects" className="text-brand-500 text-[14px]">返回项目大厅</Link>
          </div>
        )}

        {!loading && !error && template && (
          <div className="grid lg:grid-cols-3 gap-8">
            <div className="lg:col-span-2 space-y-6">
              <div className="bg-white rounded-2xl border border-ink-100 overflow-hidden">
                <div className="p-7 grid sm:grid-cols-[minmax(0,1fr)_190px] xl:grid-cols-[minmax(0,1fr)_210px] gap-7 items-center">
                  <div>
                    <span className="inline-flex items-center px-2.5 py-1 rounded-full bg-brand-50 text-brand-600 text-[12px] font-medium mb-4">
                      {domainLabel(template.domain)}
                    </span>
                    <h1 className="text-2xl md:text-3xl font-bold text-ink-900 mb-4 leading-snug">{template.name}</h1>
                    <p className="text-ink-500 text-[15px] leading-7">{template.description}</p>
                  </div>
                  <TemplateCover template={template} className="h-80 rounded-xl border border-ink-100" />
                </div>
                {!!template.business_scenarios?.length && (
                  <div className="mx-7 pb-7 pt-5 border-t border-ink-100">
                    <h2 className="text-sm font-semibold text-ink-700 mb-3">围绕这些业务展开</h2>
                    <div className="flex flex-wrap gap-2">
                      {template.business_scenarios.map((scenario) => (
                        <span key={scenario} className="rounded-lg bg-ink-50 px-3 py-2 text-sm text-ink-600">{scenario}</span>
                      ))}
                    </div>
                  </div>
                )}
              </div>

              {!!template.overview?.length && (
                <section className="bg-white rounded-2xl border border-ink-100 p-7" aria-labelledby="template-overview-title">
                  <h2 id="template-overview-title" className="text-lg font-bold text-ink-800 mb-4">项目介绍</h2>
                  <div className="space-y-4 text-[15px] leading-7 text-ink-600">
                    {template.overview.map((paragraph) => <p key={paragraph}>{paragraph}</p>)}
                  </div>
                </section>
              )}

              {!!template.learning_objectives?.length && (
                <section className="bg-white rounded-2xl border border-ink-100 p-7" aria-labelledby="template-learning-title">
                  <h2 id="template-learning-title" className="text-lg font-bold text-ink-800 mb-5">你能练习什么</h2>
                  <ol className="space-y-4">
                    {template.learning_objectives.map((objective, index) => (
                      <li key={objective} className="flex gap-3 text-[15px] leading-relaxed text-ink-600">
                        <span className="flex-shrink-0 w-7 h-7 rounded-lg bg-brand-50 text-brand-600 text-xs font-semibold flex items-center justify-center">{index + 1}</span>
                        <span>{objective}</span>
                      </li>
                    ))}
                  </ol>
                </section>
              )}

              {/* 技术栈 */}
              <div className="bg-white rounded-2xl border border-ink-100 p-7">
                <h2 className="text-lg font-bold text-ink-800 mb-4">技术栈</h2>
                <div className="flex flex-wrap gap-2">
                  {(template.tech_stack ?? []).map((s) => (
                    <span key={s} className="px-3 py-1 rounded-lg bg-ink-50 text-ink-600 text-[12px] font-medium border border-ink-100">{s}</span>
                  ))}
                </div>
              </div>

              {/* 服务声明 */}
              <div className="bg-white rounded-2xl border border-ink-100 p-7">
                <h2 className="text-lg font-bold text-ink-800 mb-4">
                  架构服务
                  <span className="ml-2 px-2 py-0.5 rounded-full bg-ink-100 text-ink-500 text-[11px] font-medium">
                    {template.services.length} 个
                  </span>
                </h2>
                <div className="grid sm:grid-cols-2 gap-3">
                  {template.services.map((s) => (
                    <div key={s.name} className="flex items-center gap-3 p-4 rounded-xl bg-ink-50 border border-ink-100">
                      <div className="w-10 h-10 rounded-lg bg-white border border-ink-100 text-brand-500 flex items-center justify-center">⚙️</div>
                      <div className="flex-1 min-w-0">
                        <div className="font-semibold text-[14px] text-ink-800">{s.display_name || s.name}</div>
                        {s.description && <p className="text-sm text-ink-500 leading-relaxed mt-1">{s.description}</p>}
                        <div className="text-[12px] text-ink-400 mt-1">{s.display_name ? `${s.name} · ` : ""}端口 {s.port} · {s.role === "middleware" ? "中间件" : "业务服务"}</div>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            </div>

            {/* 侧边栏：开始实训 */}
            <div className="space-y-6 lg:self-start lg:sticky lg:top-24">
              <div className="bg-white rounded-2xl border border-ink-100 p-6">
                <h3 className="font-bold text-ink-800 mb-5">开始实训</h3>
                <button
                  type="button"
                  onClick={handleStart}
                  disabled={starting}
                  className="w-full h-11 rounded-xl bg-brand-500 hover:bg-brand-600 disabled:opacity-60 text-white font-semibold text-[14px] transition-colors"
                >
                  {starting ? "等待环境就绪…" : startError ? (pending ? "重试本次申请" : "重新开始训练") : pending ? "继续等待本次申请" : "开始训练"}
                </button>
                {starting && (
                  <div className="mt-4">
                    <div className="h-2 rounded-full bg-ink-100 overflow-hidden">
                      <div className="h-full bg-brand-500 transition-all" style={{ width: `${percent}%` }} />
                    </div>
                    <p className="mt-2 text-[11px] text-ink-400">{stage} · {percent}%</p>
                  </div>
                )}
                {startError && (
                  <p className="mt-3 text-[11px] text-danger">{startError}</p>
                )}
                <p className="mt-3 text-[11px] text-ink-400 leading-relaxed">
                  每次实训使用独立沙箱。实训结束后回收环境，并结合操作记录复盘排查过程。
                </p>
              </div>
              {!!template.audience?.length && (
                <section className="bg-white rounded-2xl border border-ink-100 p-6" aria-labelledby="template-audience-title">
                  <h2 id="template-audience-title" className="font-bold text-ink-800 mb-4">适合谁练</h2>
                  <ul className="space-y-4 text-sm leading-relaxed text-ink-500">
                    {template.audience.map((item) => <li key={item} className="flex gap-2"><span className="mt-2 h-1.5 w-1.5 flex-none rounded-full bg-brand-400" aria-hidden="true" /><span>{item}</span></li>)}
                  </ul>
                </section>
              )}
            </div>
          </div>
        )}
      </main>
    </div>
  );
}
