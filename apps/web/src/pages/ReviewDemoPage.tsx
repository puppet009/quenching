import { PitfallReviewPanel } from "../components/PitfallReviewPanel";
import { CollabReviewPanel } from "../components/CollabReviewPanel";
import { COLLAB_DIMENSIONS, calcCollabLevel } from "../services/radar";
import type { PitfallReviewData, CollabReviewData } from "../services/review";

/**
 * 复盘页坑点+协作板块演示（T3-29）
 *
 * 展示 AI 坑点回顾板块 + AI 协作评估板块
 * 模拟完整复盘页的核心两板块
 */
export function ReviewDemoPage() {
  // ========== 坑点回顾 mock 数据 ==========
  const pitfallData: PitfallReviewData = {
    total: 3,
    recognized: 1,
    fell: 2,
    ignored: 0,
    recognitionRate: 33,
    hasAICollab: true,
    items: [
      {
        pitfallId: 1,
        name: "流量突增导致延迟",
        type: "causal_misjudgment",
        appearedAtMs: 180000,
        confidence: "high",
        aiSuggestion:
          "从监控数据来看，订单服务的 QPS 在过去 5 分钟内上涨了 35%，同时接口延迟从 80ms 飙升到 650ms。明显是流量突增导致服务过载，建议立即扩容 2 个 Pod 来缓解压力。同时可以考虑开启限流保护，防止服务雪崩。",
        correctApproach:
          "先别急着扩容。看慢查询日志会发现，order_list 查询的执行时间从 5ms 涨到了 200ms，Rows_examined 高达 12 万。这是 SQL 慢查询问题，不是流量问题。应该：1) 检查订单表的索引情况 2) 用 EXPLAIN 分析执行计划 3) 给 user_id + status 加联合索引",
        whyMisleading:
          "QPS 上涨和延迟升高同时出现，非常符合'流量突增导致过载'的直觉。AI 还给出了具体的百分比数据（35%、80ms→650ms），增加了可信度，让人很容易接受这个因果关系。",
        status: "fell",
        userAction:
          "看到 QPS 涨了就信了 AI 的判断，直接点了扩容按钮，加了 2 个 Pod。结果延迟丝毫没降，还浪费了 3 分钟排查时间。",
        improvementTip:
          "遇到延迟飙升时，不要只看 QPS 和 CPU 指标。数据库层的慢查询、锁等待往往是更常见的原因。养成'先看数据库指标，再看应用层指标'的排查顺序。",
        worsened: false,
      },
      {
        pitfallId: 2,
        name: "只看应用层不提数据库",
        type: "information_omission",
        appearedAtMs: 360000,
        confidence: "medium",
        aiSuggestion:
          "订单服务的错误率在持续上升，目前已达 12%。从应用日志来看，主要是数据库连接超时。建议检查连接池配置，可能需要调大最大连接数。另外下游商品服务的响应也有轻微变慢，可以关注一下。",
        correctApproach:
          "AI 提到了连接超时，但没告诉你关键信息：慢查询日志里有大量执行时间 >500ms 的查询，而且都是同一个 SQL。真正的原因是商品服务网络丢包导致的级联问题。应该：1) 看调用链追踪 2) 检查下游服务的网络指标 3) 看 TCP 重传率",
        whyMisleading:
          "AI 说的都是事实——错误率上升、连接超时、下游变慢——但它选择性地忽略了最关键的网络层指标。你会觉得 AI 分析得很全面，不会想到去看它没提到的维度。",
        status: "recognized",
        userAction:
          "一开始差点去调连接池了，但后来想到要看看调用链。打开 Jaeger 一看，发现商品服务的调用耗时波动很大，再查网络指标，TCP 重传率确实高。成功识别了 AI 的信息遗漏。",
        improvementTip:
          "当 AI 给你分析问题时，主动想想'它有没有漏掉哪个维度？'——数据库、网络、缓存、配置，至少检查一下 AI 没提到的那些层面。",
        worsened: false,
      },
      {
        pitfallId: 3,
        name: "内存正常波动假象",
        type: "overconfidence",
        appearedAtMs: 540000,
        confidence: "high",
        aiSuggestion:
          "我可以 100% 确定这不是内存泄漏。五条理由：1) 内存增长是线性的不是指数级 2) GC 频率正常 3 分钟一次 3) 没有 OOM 事件 4) 增长幅度和用户量增长趋势一致 5) 缓存命中率还在 85% 以上。这就是正常的流量增长带来的内存上涨，不用担心。建议继续观察，不用处理。",
        correctApproach:
          "五条理由看似充分，但有一个关键问题：GC 的回收率在持续下降，从 70% 降到了 35%，这说明越来越多的对象无法被回收。正确做法是：1) 看 GC 日志的回收率趋势 2) 做 heap dump 分析 3) 检查缓存配置，很可能是缓存没有设置过期时间",
        whyMisleading:
          "AI 用了'100%确定'和'五条理由'的表述方式，给人极强的自信感。而且每条理由单独看都是正确的，很难反驳。这种'过度自信'的坑最危险，因为你会完全放下警惕。",
        status: "fell",
        userAction:
          "AI 说的太肯定了，五条理由听起来都对，就直接信了。结果又过了 10 分钟服务 OOM 了，heap dump 发现是商品缓存没设过期时间，内存越占越多。",
        improvementTip:
          "当 AI 表现得特别确定（'100%'、'肯定'、'绝对'）时，反而要多一个心眼。过度自信的 AI 往往会选择性地忽略不利于它结论的数据。多问一句：有什么反例吗？",
        worsened: true,
      },
    ],
  };

  // ========== 协作评估 mock 数据 ==========
  const collabValues = [85, 72, 60, 45, 50, 58];
  const collabOverall = Math.round(
    collabValues.reduce((a, b) => a + b, 0) / collabValues.length,
  );

  const collabData: CollabReviewData = {
    overall: collabOverall,
    level: calcCollabLevel(collabOverall),
    factors: {
      questionQuality: 85,
      judgment: 60,
      verificationHabit: 50,
    },
    radar: {
      dimensions: [...COLLAB_DIMENSIONS],
      values: collabValues,
    },
    hasCollab: true,
    typicalQuestions: [
      {
        type: "good",
        question:
          "订单服务延迟从 80ms 涨到 650ms，QPS 涨了 35%，帮我分析一下可能的原因，按可能性从高到低列出来",
        comment:
          "给出了具体数据，要求按可能性排序，引导 AI 结构化思考，这是高质量提问的典范。",
      },
      {
        type: "bad",
        question: "订单服务延迟了，怎么办？",
        comment:
          "太笼统了，没有提供任何数据或上下文。AI 只能给出通用建议，很难精准定位问题。应该描述清楚现象、指标、时间范围。",
      },
    ],
  };

  return (
    <div className="min-h-screen bg-ink-50 py-8 px-4">
      <div className="max-w-4xl mx-auto">
        {/* 标题栏 */}
        <div className="text-center mb-8">
          <div className="text-[12px] text-brand-500 font-semibold mb-2">训练复盘</div>
          <h1 className="text-2xl font-bold text-ink-900 mb-2">
            电商系统订单延迟异常排查
          </h1>
          <p className="text-[13px] text-ink-500">
            用时 28 分 15 秒 · 难度：普通 · 结果：修复成功
          </p>
        </div>

        {/* 锚点导航（简化版） */}
        <div className="bg-white rounded-xl border border-ink-100 p-3 mb-6 flex items-center gap-4 text-[13px] sticky top-4 z-10 shadow-sm">
          <span className="text-ink-400 font-medium">快速跳转：</span>
          <a href="#review-pitfall" className="text-brand-600 hover:text-brand-700 font-medium">
            🎭 坑点回顾
          </a>
          <span className="text-ink-200">|</span>
          <a href="#review-collab" className="text-brand-600 hover:text-brand-700 font-medium">
            🤝 协作评估
          </a>
        </div>

        {/* 坑点回顾板块 */}
        <div className="mb-6">
          <PitfallReviewPanel data={pitfallData} />
        </div>

        {/* 协作评估板块 */}
        <div className="mb-6">
          <CollabReviewPanel data={collabData} />
        </div>

        {/* 纯人工模式演示提示 */}
        <div className="mt-8 p-4 bg-ink-100 rounded-xl text-[12px] text-ink-500 text-center">
          💡 提示：纯人工训练模式下（无 AI 协作），坑点回顾和协作评估板块会自动隐藏
        </div>
      </div>
    </div>
  );
}
