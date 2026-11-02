import { compareAbility, DIMENSION_LABELS, formatAbility, type Assessment } from '../../services/growthReview';

export function AbilityAssessmentPanel({ assessment, baseline, onSource }: {
  assessment: Assessment; baseline?: Assessment; onSource?: (reference: string) => void;
}) {
  const { hasBaseline } = compareAbility(baseline, assessment);
  return <section aria-label="七维能力评估" className="space-y-3">
    <h2 className="font-semibold text-ink-800">七维能力评估</h2>
    <p className="text-sm text-ink-500">总体等级：{formatAbility({ level: assessment.overall_level, confidence: 0 })}
      {!hasBaseline && '。缺少可比较的基线，本报告不判断提升幅度。'}</p>
    <div className="grid gap-3 sm:grid-cols-2">
      {Object.entries(DIMENSION_LABELS).map(([key, label]) => {
        const dimension = assessment.dimensions?.[key] || { level: 'unknown', confidence: 0 };
        return <div key={key} className="rounded-lg border border-ink-100 p-3 text-sm">
          <div className="flex justify-between gap-2"><span>{label}</span><strong>{formatAbility(dimension)}</strong></div>
          {dimension.level !== 'unknown' && <p className="mt-1 text-ink-500">置信度：{Number.isFinite(dimension.confidence) ? dimension.confidence.toFixed(2) : '未提供'}</p>}
          {!!onSource && (dimension.evidence_refs || []).map(ref => <button key={ref} className="mt-2 mr-2 text-brand-600 break-all text-left" onClick={() => onSource(ref)}>核对 {ref}</button>)}
        </div>;
      })}
    </div>
    {(assessment.source_refs || []).map(ref => <button key={ref} className="text-sm text-brand-600 mr-3" onClick={() => onSource?.(ref)}>查看评估依据 {ref}</button>)}
  </section>;
}
