import {GAP_LABELS} from '../../services/operationState';
export function AuditCoverageNotice({overview}:{overview:{audit_complete?:boolean;audit_coverage?:unknown;audit_gaps?:string[];cleanup_status?:string}}){
 const collection=overview.audit_coverage as {complete?:boolean}|undefined;
 const gaps=overview.audit_gaps?.length?overview.audit_gaps:overview.audit_coverage?[]:['legacy_coverage_unknown'];
 return <div role="status" className="rounded-lg bg-amber-50 text-amber-800 p-3 text-sm space-y-1"><p>沙箱清理：{overview.cleanup_status==='succeeded'?'已完成':overview.cleanup_status==='failed'?'失败':'未知'}；记录采集：{collection?.complete===true&&gaps.length===0?'已确认完整':'存在缺口或覆盖未知'}。</p>{gaps.map(g=><p key={g}>{GAP_LABELS[g]||g}</p>)}<p>修复结果和能力判断以对应证据为准。</p></div>;
}
