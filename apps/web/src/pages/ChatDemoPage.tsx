import { Link } from "react-router-dom";
import { Logo } from "@quenching/shared";
import { ChatWindow } from "../components/ChatWindow";
import type { ChatMessage } from "../services/chat";

/**
 * 对话组件 Demo 页（T2-28 验收用）
 * 展示 ChatWindow 组件的各种状态和样式。
 * 访问路径：/chat-demo
 */
export function ChatDemoPage() {
  const demoMessages: ChatMessage[] = [
    {
      id: 1,
      role: "user",
      content: "我看到订单服务的 CPU 一直 100%，这是什么原因？",
    },
    {
      id: 2,
      role: "assistant",
      content:
        "根据目前的信息，我的判断是：\n\n订单服务 CPU 飙升通常有以下几个可能的原因：\n\n1. **死循环或计算密集型操作** — 检查是否有无限循环或复杂计算\n2. **大量请求涌入** — 查看 QPS 是否异常升高\n3. **频繁 GC** — 如果是 Java 服务，可能是内存问题导致\n\n建议你先看一下监控面板里的 QPS 和错误率趋势，再决定下一步排查方向。",
    },
    {
      id: 3,
      role: "user",
      content: "QPS 正常，错误率也不高，那可能是什么？",
    },
    {
      id: 4,
      role: "assistant",
      content:
        "我比较确定，基本可以按这个方向处理：\n\n根因应该是订单服务的某个计算逻辑有问题，比如订单优惠计算的复杂度太高导致 CPU 飙升。建议你直接优化订单服务的代码，或者增加更多实例来分摊压力。\n\n这种情况很常见，加机器通常是最快的解决方案。",
      isPitfall: true,
      pitfallType: "causality_misjudgment",
      confidenceLevel: "high",
    },
    {
      id: 5,
      role: "assistant",
      content: "抱歉，我暂时无法回答这个问题（模拟错误状态）。",
      isError: true,
      errorMsg: "AI 服务暂时不可用，请稍后重试",
    },
  ];

  return (
    <div className="min-h-screen bg-ink-50">
      {/* 顶部导航 */}
      <nav className="sticky top-0 z-50 bg-white/80 backdrop-blur-md border-b border-ink-100">
        <div className="max-w-4xl mx-auto px-6 h-16 flex items-center justify-between">
          <Link to="/" className="flex items-center">
            <Logo size="md" />
          </Link>
          <div className="flex items-center gap-3 text-[12px] text-ink-400">
            <span className="px-2 py-1 rounded-md bg-brand-50 text-brand-600 font-medium border border-brand-100">
              组件演示
            </span>
            <span>ChatWindow · T2-28</span>
          </div>
        </div>
      </nav>

      <main className="max-w-4xl mx-auto px-6 py-10">
        <div className="mb-6">
          <h1 className="text-2xl font-bold text-ink-900 mb-2">对话窗口组件演示</h1>
          <p className="text-ink-500 text-[14px]">
            AI 对话窗口组件，支持 SSE 流式输出、坑点标识、打字机效果、自动滚动
          </p>
        </div>

        {/* 图例 */}
        <div className="flex flex-wrap items-center gap-4 mb-4 text-[13px] text-ink-500">
          <span className="flex items-center gap-1.5">
            <span className="w-2 h-2 rounded-full bg-brand-500" />
            用户消息
          </span>
          <span className="flex items-center gap-1.5">
            <span className="w-2 h-2 rounded-full bg-ai" />
            AI 普通消息
          </span>
          <span className="flex items-center gap-1.5">
            <span className="w-2 h-2 rounded-full bg-danger" />
            AI 坑点消息
          </span>
          <span className="flex items-center gap-1.5">
            <span className="w-2 h-2 rounded-full bg-danger/50" />
            错误状态
          </span>
        </div>

        {/* 静态消息展示区 */}
        <div className="bg-white rounded-2xl border border-ink-100 p-6 mb-8">
          <h3 className="text-sm font-semibold text-ink-700 mb-4">消息气泡样式展示</h3>
          <div className="space-y-5">
            {demoMessages.map((msg) => (
              <DemoMessageBubble key={msg.id} message={msg} />
            ))}
          </div>
        </div>

        {/* 完整对话窗口（真实可用，需后端） */}
        <div className="mb-4">
          <h3 className="text-sm font-semibold text-ink-700 mb-3">完整对话窗口（需后端支持）</h3>
          <p className="text-[12px] text-ink-400 mb-3">
            后端 training-service 启动后可在此直接对话。当前后端未启动时发送会显示错误。
          </p>
        </div>
        <ChatWindow
          conversationType="knowledge_qa"
          initialMessages={demoMessages.slice(0, 2)}
          height="500px"
        />

        {/* 功能特性 */}
        <div className="mt-8 grid md:grid-cols-2 gap-4">
          <FeatureCard
            title="SSE 流式接收"
            desc="使用 fetch + ReadableStream 逐帧解析 SSE 数据，实现打字机效果"
          />
          <FeatureCard
            title="坑点标识"
            desc="AI 坑点消息显示特殊徽章，包含坑点类型和置信度等级"
          />
          <FeatureCard
            title="自动滚动"
            desc="新消息自动滚动到底部，流式输出时保持跟随"
          />
          <FeatureCard
            title="完整状态"
            desc="空状态、加载中、错误态、流式中，四种状态全覆盖"
          />
        </div>
      </main>

      <footer className="bg-white border-t border-ink-100 py-8 mt-16">
        <div className="max-w-4xl mx-auto px-6 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <span className="w-7 h-7 bg-brand-500 text-white font-bold flex items-center justify-center rounded-lg text-sm">淬</span>
            <span className="text-ink-500 font-medium text-[14px]">淬火 Quenching</span>
          </div>
          <p className="text-[12px] text-ink-400">T2-28 对话窗口组件</p>
        </div>
      </footer>
    </div>
  );
}

/** 演示用消息气泡（直接复用 ChatMessage 类型，静态展示） */
function DemoMessageBubble({ message }: { message: ChatMessage }) {
  const isUser = message.role === "user";

  if (isUser) {
    return (
      <div className="flex justify-end">
        <div className="max-w-[80%]">
          <div className="bg-brand-500 text-white rounded-2xl rounded-tr-md px-4 py-3 text-[14px] leading-relaxed shadow-sm whitespace-pre-wrap">
            {message.content}
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="flex gap-3">
      <div className="flex-none w-9 h-9 rounded-xl bg-gradient-to-br from-ai to-ai/80 text-white flex items-center justify-center shadow-sm">
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <path d="M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3Z" />
          <path d="M19 10v2a7 7 0 0 1-14 0v-2" />
          <line x1="12" x2="12" y1="19" y2="22" />
        </svg>
      </div>
      <div className="max-w-[80%] flex-1">
        {message.isPitfall && (
          <div className="mb-2 flex items-center gap-2">
            <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full bg-danger/10 text-danger text-[11px] font-semibold border border-danger/20">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
                <line x1="12" y1="9" x2="12" y2="13" />
                <line x1="12" y1="17" x2="12.01" y2="17" />
              </svg>
              AI 坑点
            </span>
            {message.pitfallType && (
              <span className="text-[11px] text-ink-400">
                {message.pitfallType === "causality_misjudgment"
                  ? "因果误判"
                  : message.pitfallType === "info_missing"
                    ? "信息遗漏"
                    : "过度自信"}
              </span>
            )}
            {message.confidenceLevel && (
              <span className="text-[11px] text-ink-400 ml-auto">
                {message.confidenceLevel === "low"
                  ? "低置信度"
                  : message.confidenceLevel === "medium"
                    ? "中等置信度"
                    : "高置信度"}
              </span>
            )}
          </div>
        )}
        <div
          className={`bg-white border border-ink-100 rounded-2xl rounded-tl-md px-4 py-3 text-[14px] leading-relaxed text-ink-700 shadow-sm whitespace-pre-wrap ${
            message.isError ? "border-danger/30 bg-danger/5 text-danger" : ""
          }`}
        >
          {message.isError ? (
            <div className="flex items-start gap-2">
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="mt-0.5 flex-none">
                <circle cx="12" cy="12" r="10" />
                <line x1="12" y1="8" x2="12" y2="12" />
                <line x1="12" y1="16" x2="12.01" y2="16" />
              </svg>
              <span>{message.errorMsg ?? message.content}</span>
            </div>
          ) : (
            message.content
          )}
        </div>
      </div>
    </div>
  );
}

function FeatureCard({ title, desc }: { title: string; desc: string }) {
  return (
    <div className="bg-white rounded-xl border border-ink-100 p-4">
      <h4 className="font-semibold text-ink-700 text-[14px] mb-1">{title}</h4>
      <p className="text-[13px] text-ink-500">{desc}</p>
    </div>
  );
}
