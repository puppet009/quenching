import { Link } from "react-router-dom";

/**
 * 首页/落地页
 *
 * 结构：Hero → 故障场景库 → 核心能力 → 四步流程 → AI 协作差异化
 *       → 双维能力模型 → 适用人群 → 常见问题 → 轻量底部引导
 *
 * 设计取向：不写空洞口号式深色大横幅（典型 AI 模板味），
 * 用具体的故障场景、能力项和真实问答撑起信息密度。
 * 布局采用流式容器（.page-container），宽度随屏幕伸缩；Hero 为左右双栏。
 */
export function HomePage() {
  return (
    <div className="bg-ink-50 text-ink-800">
      {/* ===== Hero ===== */}
      <section className="relative overflow-hidden">
        <div className="absolute inset-0 bg-gradient-to-br from-brand-50 via-white to-ai/5 pointer-events-none" />
        <div className="absolute inset-0 bg-blueprint pointer-events-none" />
        <div className="absolute top-24 right-[6%] w-[420px] h-[420px] bg-brand-200/30 rounded-full blur-3xl pointer-events-none" />
        <div className="absolute bottom-0 left-[-4%] w-[340px] h-[340px] bg-ai/10 rounded-full blur-3xl pointer-events-none" />

        <div className="page-container relative py-16 md:py-24 lg:py-28">
          <div className="grid lg:grid-cols-[1.05fr_0.95fr] gap-12 xl:gap-16 items-center">
            {/* 左：文案 */}
            <div className="max-w-2xl">
              <div className="animate-rise inline-flex items-center gap-2 px-3.5 py-1.5 rounded-full bg-brand-50 border border-brand-100 text-brand-600 text-[12px] font-medium mb-6">
                <span className="w-1.5 h-1.5 rounded-full bg-brand-500" />
                AI 时代的工程师必修课
              </div>

              <h1
                className="animate-rise text-4xl md:text-5xl lg:text-[56px] font-bold text-ink-900 leading-[1.12] tracking-tight"
                style={{ animationDelay: "0.08s" }}
              >
                在真实故障里
                <br />
                练出
                <span className="text-brand-500">驾驭 AI</span>
                的判断力
              </h1>

              <p
                className="animate-rise mt-6 text-base md:text-lg text-ink-500 leading-relaxed"
                style={{ animationDelay: "0.16s" }}
              >
                淬火 Quenching 是面向 AI 时代的工程师生产风险实战与人机协作训练平台。
                用全真沙箱故障仿真，结合 AI 坑点训练，让你在安全的环境中掌握排障技能与 AI 协作能力。
              </p>

              <div
                className="animate-rise mt-9 flex flex-wrap items-center gap-4"
                style={{ animationDelay: "0.24s" }}
              >
                <Link
                  to="/login"
                  state={{ mode: "register" }}
                  className="h-12 px-7 rounded-xl bg-brand-500 hover:bg-brand-600 text-white font-semibold text-[15px] inline-flex items-center gap-2 transition-all shadow-lg shadow-brand-500/20 hover:shadow-xl hover:shadow-brand-500/30 hover:-translate-y-0.5"
                >
                  免费开始训练
                  <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M5 12h14" />
                    <path d="m12 5 7 7-7 7" />
                  </svg>
                </Link>
                <Link
                  to="/projects"
                  className="h-12 px-7 rounded-xl border border-ink-200 bg-white hover:border-ink-300 text-ink-700 font-semibold text-[15px] inline-flex items-center transition-colors"
                >
                  浏览训练项目
                </Link>
              </div>

              <div
                className="animate-rise mt-8 flex flex-wrap items-center gap-x-6 gap-y-2 text-[13px] text-ink-400"
                style={{ animationDelay: "0.32s" }}
              >
                <div className="flex items-center gap-1.5">
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="text-ok">
                    <path d="M20 6 9 17l-5-5" />
                  </svg>
                  <span>沙箱隔离，安全演练</span>
                </div>
                <div className="flex items-center gap-1.5">
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="text-ok">
                    <path d="M20 6 9 17l-5-5" />
                  </svg>
                  <span>AI 助教全程引导</span>
                </div>
                <div className="flex items-center gap-1.5">
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="text-ok">
                    <path d="M20 6 9 17l-5-5" />
                  </svg>
                  <span>双维能力量化</span>
                </div>
              </div>
            </div>

            {/* 右：实战任务面板 */}
            <div className="relative hidden lg:block">
              <div className="animate-rise" style={{ animationDelay: "0.2s" }}>
                <div className="animate-floaty relative bg-white/90 backdrop-blur rounded-2xl border border-ink-100 shadow-2xl shadow-ink-900/10 overflow-hidden">
                  <div className="flex items-center gap-2 px-5 h-12 border-b border-ink-100 bg-ink-50/70">
                    <span className="w-2.5 h-2.5 rounded-full bg-danger/80" />
                    <span className="w-2.5 h-2.5 rounded-full bg-warn/80" />
                    <span className="w-2.5 h-2.5 rounded-full bg-ok/80" />
                    <span className="ml-2 text-[12px] text-ink-400 font-mono">mission · ecommerce-prod</span>
                    <span className="ml-auto inline-flex items-center gap-1.5 text-[11px] font-medium text-danger">
                      <span className="w-1.5 h-1.5 rounded-full bg-danger animate-pulse-dot" />
                      故障注入中
                    </span>
                  </div>

                  <div className="p-5">
                    <div className="flex items-start justify-between gap-3">
                      <div>
                        <p className="text-[11px] font-medium tracking-wider text-ink-400 uppercase">Mission 07</p>
                        <h3 className="mt-1 text-[17px] font-bold text-ink-900">订单服务间歇性 502</h3>
                      </div>
                      <span className="shrink-0 px-2.5 py-1 rounded-lg bg-danger/10 text-danger text-[11px] font-semibold">
                        困难
                      </span>
                    </div>

                    <div className="mt-4 grid grid-cols-3 gap-3">
                      <Metric label="CPU" value="96%" tone="danger" />
                      <Metric label="P99 延迟" value="4.2s" tone="warn" />
                      <Metric label="错误率" value="18%" tone="danger" />
                    </div>

                    <div className="mt-4 rounded-xl bg-ink-900 p-3.5 font-mono text-[11.5px] leading-6">
                      <p className="text-ink-400">$ kubectl logs order-service --tail=4</p>
                      <p className="text-ok/90">[10:24:01] health check passed</p>
                      <p className="text-warn">[10:24:03] upstream timeout · db pool</p>
                      <p className="text-danger">[10:24:04] ERROR: connection pool exhausted</p>
                      <p className="text-ink-300">[10:24:06] restarting worker ...</p>
                    </div>

                    <div className="mt-4 flex items-center justify-between">
                      <div className="flex items-center gap-2 text-[12px] text-ink-500">
                        <span className="w-6 h-6 rounded-lg bg-ai/10 text-ai flex items-center justify-center">
                          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
                            <path d="M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3Z" />
                            <path d="M19 10v2a7 7 0 0 1-14 0v-2" />
                          </svg>
                        </span>
                        AI 助教已就位
                      </div>
                      <span className="text-[12px] font-semibold text-brand-600">限时 20:00</span>
                    </div>
                  </div>
                </div>

                <div
                  className="absolute -left-6 xl:-left-10 bottom-10 bg-white rounded-xl border border-ink-100 shadow-xl shadow-ink-900/10 p-3.5 w-44 animate-floaty"
                  style={{ animationDelay: "1.2s" }}
                >
                  <p className="text-[11px] text-ink-400 font-medium">本次能力评估</p>
                  <div className="mt-2 space-y-2">
                    <ScoreBar label="排障技术" value={82} color="bg-brand-500" />
                    <ScoreBar label="AI 协作" value={91} color="bg-ai" />
                  </div>
                </div>

                <div
                  className="absolute -top-4 -right-3 bg-ink-900 text-white rounded-xl px-3.5 py-2.5 shadow-xl flex items-center gap-2 animate-floaty"
                  style={{ animationDelay: "0.6s" }}
                >
                  <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="text-ok">
                    <rect width="18" height="18" x="3" y="3" rx="2" />
                    <path d="M9 9h6v6H9z" />
                  </svg>
                  <span className="text-[11.5px] font-semibold">K8s 沙箱隔离</span>
                </div>
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* ===== 真实故障场景库 ===== */}
      <section className="py-20 md:py-24 bg-white border-t border-ink-100">
        <div className="page-container">
          <div className="max-w-2xl mb-12">
            <h2 className="text-3xl md:text-4xl font-bold text-ink-900">
              你会遇到的，都是真实生产里的故障
            </h2>
            <p className="mt-4 text-[15px] text-ink-500 leading-relaxed">
              每个场景都从一线事故复盘提炼，故障会沿依赖链自然传导——你看到的 CPU 报警，
              根因可能在数据库、网络或下游服务，而不是出问题的那个进程本身。
            </p>
          </div>

          <div className="grid md:grid-cols-2 lg:grid-cols-3 gap-5">
            {FAULT_CATEGORIES.map((cat) => (
              <div
                key={cat.title}
                className="rounded-2xl border border-ink-100 bg-white p-6 hover:border-brand-200 transition-colors"
              >
                <div className="flex items-center gap-3 mb-4">
                  <span className="w-9 h-9 rounded-lg bg-brand-50 text-brand-500 flex items-center justify-center">
                    {cat.icon}
                  </span>
                  <h3 className="text-[15.5px] font-semibold text-ink-800">{cat.title}</h3>
                </div>
                <ul className="space-y-2">
                  {cat.items.map((item) => (
                    <li key={item} className="text-[13px] text-ink-500 flex items-start gap-2">
                      <span className="mt-[7px] w-1 h-1 rounded-full bg-ink-300 shrink-0" />
                      {item}
                    </li>
                  ))}
                </ul>
              </div>
            ))}
          </div>

          <div className="mt-10">
            <Link
              to="/projects"
              className="inline-flex items-center gap-1.5 text-[14px] font-semibold text-brand-600 hover:text-brand-700"
            >
              查看项目大厅全部训练任务
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                <path d="m9 18 6-6-6-6" />
              </svg>
            </Link>
          </div>
        </div>
      </section>

      {/* ===== 核心能力 ===== */}
      <section className="py-20 md:py-24 bg-ink-50">
        <div className="page-container">
          <div className="max-w-2xl mb-12">
            <h2 className="text-3xl md:text-4xl font-bold text-ink-900">平台的三项核心能力</h2>
            <p className="mt-4 text-[15px] text-ink-500">
              不只是排障训练，更是 AI 时代工程决策力的锻造场。
            </p>
          </div>

          <div className="grid md:grid-cols-3 gap-6">
            <FeatureCard
              iconBg="bg-brand-50"
              iconColor="text-brand-500"
              icon={
                <svg width="26" height="26" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <rect width="18" height="18" x="3" y="3" rx="2" />
                  <path d="M3 9h18" />
                  <path d="M9 21V9" />
                </svg>
              }
              title="全真沙箱故障注入"
              desc="基于 K8s 的真实沙箱环境，支持 CPU 飙升、网络故障、磁盘打满等多种生产级故障注入，故障沿架构自然传导。"
            />
            <FeatureCard
              iconBg="bg-ai/10"
              iconColor="text-ai"
              icon={
                <svg width="26" height="26" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3Z" />
                  <path d="M19 10v2a7 7 0 0 1-14 0v-2" />
                  <line x1="12" x2="12" y1="19" y2="22" />
                </svg>
              }
              title="AI 坑点协作训练"
              desc="AI 助教故意给出看似合理但错误的建议：因果误判、信息遗漏、过度自信，训练你的批判性验证习惯。"
            />
            <FeatureCard
              iconBg="bg-ok/10"
              iconColor="text-ok"
              icon={
                <svg width="26" height="26" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M21.21 15.89A10 10 0 1 1 8 2.83" />
                  <path d="m22 12-8.41 8.41c-.58.58-1.51.47-2.12-.24L9 17l-4 1 1-4 1.65-2.47c.71-.61.82-1.54.24-2.12L22 12Z" />
                </svg>
              }
              title="双维能力雷达"
              desc="排障技术分与 AI 协作分双维度评估，清晰量化技术能力和人机协作水平的成长轨迹。"
            />
          </div>
        </div>
      </section>

      {/* ===== 训练流程 ===== */}
      <section className="py-20 md:py-24 bg-white">
        <div className="page-container">
          <div className="max-w-2xl mb-12">
            <h2 className="text-3xl md:text-4xl font-bold text-ink-900">四步完成一次实战训练</h2>
            <p className="mt-4 text-[15px] text-ink-500">从选择项目到复盘提升，完整闭环。</p>
          </div>

          <div className="grid md:grid-cols-4 gap-6">
            {[
              { num: "01", title: "选择项目", desc: "从项目大厅选择适合你水平的训练项目和难度" },
              { num: "02", title: "启动沙箱", desc: "一键启动隔离沙箱环境，真实业务系统即刻就绪" },
              { num: "03", title: "故障实战", desc: "排查故障、修复问题，AI 助教引导但不直接给答案" },
              { num: "04", title: "复盘提升", desc: "方法级复盘报告：根因分析、处置时间线与能力雷达" },
            ].map((step, i) => (
              <div
                key={step.num}
                className="relative bg-white rounded-2xl p-6 border border-ink-100 shadow-sm"
              >
                <div className="text-4xl font-bold text-brand-100 mb-3">{step.num}</div>
                <h3 className="text-lg font-semibold text-ink-800 mb-2">{step.title}</h3>
                <p className="text-[14px] text-ink-500 leading-relaxed">{step.desc}</p>
                {i < 3 && (
                  <div className="hidden md:block absolute -right-3 top-1/2 -translate-y-1/2 text-ink-200">
                    <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <path d="m9 18 6-6-6-6" />
                    </svg>
                  </div>
                )}
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* ===== AI 协作差异化 ===== */}
      <section className="py-20 md:py-24 bg-ink-50">
        <div className="page-container">
          <div className="grid md:grid-cols-2 gap-12 lg:gap-16 items-center">
            <div>
              <div className="inline-flex items-center gap-2 px-3.5 py-1.5 rounded-full bg-ai/10 text-ai text-[12px] font-medium mb-5">
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3Z" />
                  <path d="M19 10v2a7 7 0 0 1-14 0v-2" />
                  <line x1="12" x2="12" y1="19" y2="22" />
                </svg>
                AI 协作训练
              </div>
              <h2 className="text-3xl md:text-4xl font-bold text-ink-900 leading-tight">
                AI 不只是助手，
                <br />
                更是你的
                <span className="text-ai">「对手」</span>
                与
                <span className="text-ai">「镜子」</span>
              </h2>
              <p className="mt-5 text-ink-500 text-[15px] leading-relaxed">
                在 AI 生成代码日益普及的今天，工程师的核心竞争力不再是写代码的速度，
                而是<strong className="text-ink-700">判断 AI 建议是否正确的能力</strong>。
              </p>
              <p className="mt-4 text-ink-500 text-[15px] leading-relaxed">
                淬火的 AI 坑点机制会在合适时机给出看似合理但错误的建议——
                因果误判、信息遗漏、过度自信三种模式，训练你保持独立思考与验证习惯。
              </p>

              <div className="mt-8 space-y-4">
                <DiffItem
                  color="text-ink-500"
                  title="传统训练"
                  desc="只练技术，AI 只是辅助工具"
                />
                <DiffItem
                  color="text-ai"
                  title="淬火训练"
                  desc="技术 + 判断力双修，AI 既是助手也是考官"
                  highlight
                />
              </div>
            </div>

            {/* 右侧对话卡片 */}
            <div className="relative">
              <div className="absolute -inset-4 bg-gradient-to-br from-ai/10 to-brand-100/50 rounded-3xl blur-2xl" />
              <div className="relative bg-white rounded-2xl border border-ink-100 shadow-xl p-6">
                <div className="flex items-center gap-2 mb-5 pb-4 border-b border-ink-100">
                  <div className="w-3 h-3 rounded-full bg-danger" />
                  <div className="w-3 h-3 rounded-full bg-warn" />
                  <div className="w-3 h-3 rounded-full bg-ok" />
                  <span className="ml-2 text-[12px] text-ink-400 font-mono">ai-assistant</span>
                </div>
                <div className="space-y-4">
                  <div className="flex gap-3">
                    <div className="w-7 h-7 rounded-lg bg-ai/10 flex items-center justify-center flex-none">
                      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" className="text-ai">
                        <path d="M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3Z" />
                        <path d="M19 10v2a7 7 0 0 1-14 0v-2" />
                      </svg>
                    </div>
                    <div className="flex-1 bg-ink-50 rounded-xl p-3">
                      <p className="text-[13px] text-ink-700 leading-relaxed">
                        根据 CPU 使用率持续 100% 的现象，
                        <span className="text-ai font-medium">根因应该是订单服务的计算逻辑问题</span>。
                        建议你直接优化订单服务的代码，或者增加更多实例。
                      </p>
                      <div className="mt-2 flex items-center gap-1.5 text-[11px] text-ink-400">
                        <span className="w-1.5 h-1.5 rounded-full bg-warn" />
                        置信度：高
                      </div>
                    </div>
                  </div>
                  <div className="flex gap-3 justify-end">
                    <div className="flex-1 max-w-[80%] bg-brand-50 rounded-xl p-3">
                      <p className="text-[13px] text-ink-700 leading-relaxed">
                        等等，让我先检查数据库慢查询日志和下游依赖的响应时间……
                      </p>
                      <p className="mt-2 text-[12px] text-ink-500">
                        发现了——其实是数据库连接池耗尽导致请求堆积，CPU 高只是表象。
                      </p>
                    </div>
                    <div className="w-7 h-7 rounded-lg bg-brand-500 flex items-center justify-center flex-none text-white text-[12px] font-bold">
                      你
                    </div>
                  </div>
                  <div className="flex gap-3">
                    <div className="w-7 h-7 rounded-lg bg-ok/10 flex items-center justify-center flex-none">
                      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" className="text-ok">
                        <path d="M20 6 9 17l-5-5" />
                      </svg>
                    </div>
                    <div className="flex-1 bg-ok/5 rounded-xl p-3 border border-ok/20">
                      <p className="text-[13px] text-ink-700">
                        <span className="font-semibold text-ok">识别成功：</span>
                        你没有被 AI 的错误建议误导，通过自主排查找到了真正根因，AI 协作分 +10。
                      </p>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* ===== 双维能力模型 ===== */}
      <section className="py-20 md:py-24 bg-white">
        <div className="page-container">
          <div className="max-w-2xl mb-12">
            <h2 className="text-3xl md:text-4xl font-bold text-ink-900">复盘时，你会拿到什么</h2>
            <p className="mt-4 text-[15px] text-ink-500">
              每次训练结束生成方法级复盘报告，两个维度、十项能力，分数只反映本次操作的可验证行为。
            </p>
          </div>

          <div className="grid md:grid-cols-2 gap-6">
            <AbilityGroup
              tag="技术维度"
              tagClass="bg-brand-50 text-brand-600"
              abilities={[
                { name: "现象识别", desc: "从告警与指标中读出异常模式" },
                { name: "假设构建", desc: "列出可能根因并排定验证顺序" },
                { name: "工具运用", desc: "日志、链路追踪、监控命令的选择" },
                { name: "根因定位", desc: "区分表象与根因，定位到具体环节" },
                { name: "处置恢复", desc: "先止损再根治，验证修复有效性" },
              ]}
            />
            <AbilityGroup
              tag="协作维度"
              tagClass="bg-ai/10 text-ai"
              abilities={[
                { name: "指令表达", desc: "向 AI 描述问题的完整与准确程度" },
                { name: "建议核验", desc: "是否对 AI 结论做交叉验证" },
                { name: "错误识别", desc: "识破因果误判、信息遗漏与过度自信" },
                { name: "决策主导", desc: "关键判断由人做出而非照单采纳" },
                { name: "知识沉淀", desc: "把单次处置提炼为可复用的方法" },
              ]}
            />
          </div>
        </div>
      </section>

      {/* ===== 适用人群 ===== */}
      <section className="py-20 md:py-24 bg-ink-50">
        <div className="page-container">
          <div className="max-w-2xl mb-12">
            <h2 className="text-3xl md:text-4xl font-bold text-ink-900">谁在淬火上训练</h2>
          </div>

          <div className="grid md:grid-cols-2 lg:grid-cols-4 gap-5">
            {AUDIENCES.map((a) => (
              <div key={a.title} className="rounded-2xl bg-white border border-ink-100 p-6">
                <h3 className="text-[15px] font-semibold text-ink-800">{a.title}</h3>
                <p className="mt-3 text-[13px] text-ink-500 leading-relaxed">{a.desc}</p>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* ===== 常见问题 ===== */}
      <section className="py-20 md:py-24 bg-white">
        <div className="page-container">
          <div className="mb-12">
            <h2 className="text-3xl md:text-4xl font-bold text-ink-900">常见问题</h2>
          </div>

          <div className="grid md:grid-cols-2 gap-5">
            {FAQS.map((f) => (
              <div
                key={f.q}
                className="rounded-2xl border border-ink-100 bg-ink-50/60 p-6 hover:border-ink-200 transition-colors"
              >
                <h3 className="text-[15.5px] font-semibold text-ink-900">{f.q}</h3>
                <p className="mt-3 text-[13.5px] text-ink-500 leading-7">{f.a}</p>
              </div>
            ))}
          </div>
        </div>
      </section>
    </div>
  );
}

/* ===== 数据 ===== */

const FAULT_CATEGORIES = [
  {
    title: "资源耗尽",
    icon: (
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <path d="M12 2v4" /><path d="M12 18v4" /><path d="m4.93 4.93 2.83 2.83" /><path d="m16.24 16.24 2.83 2.83" />
        <path d="M2 12h4" /><path d="M18 12h4" /><circle cx="12" cy="12" r="3" />
      </svg>
    ),
    items: ["连接池耗尽导致请求排队", "内存泄漏触发 OOMKill", "磁盘被日志写满", "文件句柄 / 线程数打满"],
  },
  {
    title: "数据库",
    icon: (
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <ellipse cx="12" cy="5" rx="9" ry="3" /><path d="M3 5v14a9 3 0 0 0 18 0V5" /><path d="M3 12a9 3 0 0 0 18 0" />
      </svg>
    ),
    items: ["慢查询拖垮整库", "死锁与锁等待", "主从延迟读到旧数据", "缺失索引被一次上线带出"],
  },
  {
    title: "网络与通信",
    icon: (
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <rect x="16" y="16" width="6" height="6" rx="1" /><rect x="2" y="16" width="6" height="6" rx="1" />
        <rect x="9" y="2" width="6" height="6" rx="1" /><path d="M5 16v-3a1 1 0 0 1 1-1h12a1 1 0 0 1 1 1v3" /><path d="M12 9v4" />
      </svg>
    ),
    items: ["DNS 解析间歇性失败", "跨可用区网络抖动", "连接未释放导致端口耗尽", "TCP 重传与 MTU 不匹配"],
  },
  {
    title: "缓存与消息",
    icon: (
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16Z" />
        <path d="m3.3 7 8.7 5 8.7-5" /><path d="M12 22V12" />
      </svg>
    ),
    items: ["缓存击穿 / 雪崩", "缓存与数据库不一致", "消息积压消费滞后", "消费重复与幂等失效"],
  },
  {
    title: "容器与编排",
    icon: (
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <path d="M22 12h-6l-2 3h-4l-2-3H2" /><path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11Z" />
      </svg>
    ),
    items: ["探针误判导致反复重启", "滚动更新触发容量不足", "限流配置错误丢流量", "节点驱逐与 Pod 漂移"],
  },
  {
    title: "配置与变更",
    icon: (
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76Z" />
      </svg>
    ),
    items: ["证书到期引发全站故障", "错误配置被发布到生产", "特性开关误开", "依赖版本升级不兼容"],
  },
];

const AUDIENCES = [
  { title: "1-3 年开发工程师", desc: "日常开发没问题，但没独立扛过线上事故，想在安全环境里补齐排障经验。" },
  { title: "转型 SRE / 运维的人", desc: "需要系统化的故障演练路径，而不是靠真实事故交学费。" },
  { title: "计算机相关专业学生", desc: "课程里学不到的生产实战，用 20 分钟任务理解真实系统如何出问题。" },
  { title: "重度使用 AI 的开发者", desc: "已经在依赖 AI 写代码，想练出识别 AI 错误建议、守住决策主导权的能力。" },
];

const FAQS = [
  {
    q: "沙箱是真实环境还是模拟脚本？",
    a: "是运行在 K8s 上的真实服务栈，故障由规则引擎注入到资源、网络或配置层，会沿依赖链自然传导，不是预设好的脚本演示。",
  },
  {
    q: "需要自己准备云服务器吗？",
    a: "不需要。沙箱由平台在隔离命名空间内创建并按时自动回收，你只用浏览器里的日志、终端和监控面板操作。",
  },
  {
    q: "AI 助教故意误导，不会帮倒忙吗？",
    a: "错误建议只在训练节点出现，且有明确边界：它考验的是你是否核验，而不是让你卡死；每次坑点后都会在复盘中标注它错在哪里。",
  },
  {
    q: "没有运维基础能做吗？",
    a: "可以。入门项目提供现象解读和命令提示，难度从单一服务故障起步；复盘中的方法总结会解释每一步为什么这么做。",
  },
  {
    q: "一次训练大约多久？",
    a: "入门任务 15-20 分钟，进阶的跨服务故障 30-45 分钟。中途可以保存进度，沙箱超时会自动回收。",
  },
  {
    q: "内测收费吗？数据怎么处理？",
    a: "内测期间完全免费。操作记录仅用于生成你的能力评估，不会用于训练第三方模型，也不会关联公开身份信息。",
  },
];

/* ===== 子组件 ===== */

function FeatureCard({
  iconBg,
  iconColor,
  icon,
  title,
  desc,
}: {
  iconBg: string;
  iconColor: string;
  icon: React.ReactNode;
  title: string;
  desc: string;
}) {
  return (
    <div className="bg-white rounded-2xl p-7 border border-ink-100 hover:border-brand-200 hover:shadow-lg hover:shadow-brand-500/5 transition-all group h-full">
      <div className={`w-12 h-12 rounded-xl ${iconBg} ${iconColor} flex items-center justify-center mb-5 group-hover:scale-110 transition-transform`}>
        {icon}
      </div>
      <h3 className="text-lg font-semibold text-ink-800 mb-2">{title}</h3>
      <p className="text-[14px] text-ink-500 leading-relaxed">{desc}</p>
    </div>
  );
}

function AbilityGroup({
  tag,
  tagClass,
  abilities,
}: {
  tag: string;
  tagClass: string;
  abilities: { name: string; desc: string }[];
}) {
  return (
    <div className="rounded-2xl border border-ink-100 bg-ink-50/60 p-7">
      <span className={`inline-block px-2.5 py-1 rounded-lg text-[12px] font-semibold ${tagClass}`}>
        {tag}
      </span>
      <ul className="mt-5 divide-y divide-ink-100">
        {abilities.map((ab) => (
          <li key={ab.name} className="py-3 flex items-start justify-between gap-4 first:pt-0 last:pb-0">
            <span className="text-[14px] font-medium text-ink-800 shrink-0">{ab.name}</span>
            <span className="text-[12.5px] text-ink-500 text-right leading-6">{ab.desc}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function DiffItem({
  color,
  title,
  desc,
  highlight = false,
}: {
  color: string;
  title: string;
  desc: string;
  highlight?: boolean;
}) {
  return (
    <div
      className={`flex items-center gap-4 p-4 rounded-xl ${
        highlight ? "bg-ai/5 border border-ai/20" : "bg-ink-50 border border-transparent"
      }`}
    >
      <span className={`font-bold text-[15px] ${color}`}>{title}</span>
      <span className="text-[13px] text-ink-500">{desc}</span>
    </div>
  );
}

function Metric({ label, value, tone }: { label: string; value: string; tone: "danger" | "warn" }) {
  const tones = {
    danger: "text-danger bg-danger/10",
    warn: "text-warn bg-warn/10",
  } as const;
  return (
    <div className="rounded-xl border border-ink-100 bg-ink-50/60 p-3">
      <p className="text-[11px] text-ink-400 font-medium">{label}</p>
      <p className={`mt-1 text-[18px] font-bold tabular-nums ${tones[tone]}`}>{value}</p>
    </div>
  );
}

function ScoreBar({ label, value, color }: { label: string; value: number; color: string }) {
  return (
    <div>
      <div className="flex items-center justify-between text-[11px] text-ink-500">
        <span>{label}</span>
        <span className="font-semibold tabular-nums">{value}</span>
      </div>
      <div className="mt-1 h-1.5 rounded-full bg-ink-100 overflow-hidden">
        <div className={`h-full rounded-full ${color}`} style={{ width: `${value}%` }} />
      </div>
    </div>
  );
}
