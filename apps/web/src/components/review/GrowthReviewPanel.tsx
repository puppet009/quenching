import {AuditCoverageNotice} from "./AuditCoverageNotice";
import { useState } from 'react';
import { Link } from 'react-router-dom';
import { resolvePracticeLink, type GrowthReport } from '../../services/growthReview';
import { AbilityAssessmentPanel } from './AbilityAssessmentPanel';
import { ReviewSourceDialog } from './ReviewSourceDialog';

const categories: Record<string, string> = { observed: '平台记录', user_statement: '学员自述', assistant_suggestion: '助手建议', hypothesis: '待验证推断', industry_advice: '行业建议' };
export function GrowthReviewPanel({ reviewId, data }: { reviewId: number; data: GrowthReport }) {
  const [reference, setReference] = useState<string>();
  const refs = (values: string[]) => <div className="mt-2 flex flex-wrap gap-3">{values.map(ref => <button key={ref} className="text-xs text-brand-600 break-all text-left" onClick={() => setReference(ref)}>核对 {ref}</button>)}</div>;
  return <section aria-label="整场成长复盘" className="bg-white rounded-2xl border border-ink-100 p-5 md:p-8 space-y-6">
    <div><h2 className="text-lg font-semibold text-ink-800">整场成长复盘</h2><p className="text-sm text-ink-500 mt-2">覆盖本场 {data.overview.source_count ?? 0} 条冻结来源，结合已采集的排查过程形成建议。</p></div>
    <AuditCoverageNotice overview={data.overview}/>
    <AbilityAssessmentPanel assessment={data.ability} baseline={data.ability.baseline} onSource={setReference} />
    <details className="border-t border-ink-100 pt-4"><summary className="cursor-pointer font-semibold">结论与证据核对</summary><div className="space-y-4 mt-4">{data.claims.map((claim, i) => <div key={i} className="text-sm break-words"><span className="text-ink-500">{categories[claim.category] || claim.category}：</span>{claim.text}{refs(claim.source_refs)}{(claim.source_urls || []).filter(url => /^https?:\/\//i.test(url)).map(url => <a key={url} href={url} target="_blank" rel="noreferrer" className="block mt-2 text-brand-600 break-all">行业来源：{url}</a>)}</div>)}</div></details>
    <section className="space-y-3"><h2 className="font-semibold">架构方案与取舍</h2>{data.architecture.length === 0 && <p className="text-sm text-ink-500">本场没有足够依据形成架构方案。</p>}{data.architecture.map((option, i) => <article key={i} className="rounded-lg border border-ink-100 p-4 space-y-2 text-sm break-words"><h3 className="font-semibold">{option.problem}</h3><p>方案：{option.option}</p><p>前提：{option.preconditions}</p><p>收益：{option.benefits}</p><p>代价：{option.costs}</p><p>验证：{option.verification}</p>{refs(option.source_refs)}</article>)}</section>
    <section className="space-y-3"><h2 className="font-semibold">接下来的练习</h2>{data.practices.length === 0 && <p className="text-sm text-ink-500">本场暂未形成具体练习建议。</p>}{data.practices.slice(0, 3).map((practice, i) => {const link = resolvePracticeLink(practice);return <article key={i} className="rounded-lg border border-ink-100 p-4 text-sm break-words"><h3 className="font-semibold">{practice.goal}</h3><ol className="list-decimal pl-5 my-3 space-y-1">{practice.actions.map((action, n) => <li key={n}>{action}</li>)}</ol><p>验收标准：{practice.success_criteria}</p>{refs(practice.source_refs)}{link ? <Link className="inline-block mt-3 text-brand-600" to={link}>查看对应项目</Link> : <p className="mt-3 text-xs text-ink-400">暂无已核验的项目入口，可按以上步骤练习。</p>}</article>;})}</section>
    {reference && <ReviewSourceDialog key={`${reviewId}:${reference}`} reviewId={reviewId} reference={reference} onClose={() => setReference(undefined)} />}
  </section>;
}
