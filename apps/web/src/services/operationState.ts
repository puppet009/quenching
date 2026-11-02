export interface ScopeTicket { sessionId: number; generation: number; signal: AbortSignal }
export class OperationScope {
  private generation = 0;
  private controller?: AbortController;
  begin(sessionId: number): ScopeTicket {
    this.cancel(); this.controller = new AbortController();
    return { sessionId, generation: this.generation, signal: this.controller.signal };
  }
  current(ticket: ScopeTicket) { return ticket.generation === this.generation && !ticket.signal.aborted; }
  cancel() { this.controller?.abort(); this.generation++; }
}
export class CommandSubmission {
  private key?: string;
  constructor(private createKey = () => crypto.randomUUID()) {}
  begin() { return this.key ??= this.createKey(); }
  newExecution() { this.key = undefined; }
}
const statuses: Record<string,string> = { pending:'待执行',running:'执行中',succeeded:'执行成功',failed:'执行失败',unknown:'结果未知',cancelled:'已取消' };
export function operationStatus(e: {status:string;exit_code?:number|null}) {
  const label=statuses[e.status] || '结果未知';
  return (e.status==='succeeded'||e.status==='failed') && e.exit_code!=null ? `${label}（退出码 ${e.exit_code}）` : label;
}
export function coverageReasons(coverage?: {reason_codes?:string[]}) { return coverage ? coverage.reason_codes ?? [] : ['legacy_coverage_unknown']; }
export const GAP_LABELS: Record<string,string> = {
 legacy_coverage_unknown:'旧记录没有覆盖信息',interactive_untracked:'交互终端内部命令未完整跟踪',result_unconfirmed:'操作结果未确认',
 output_limit_exceeded:'输出超过保存额度',binary_text_omitted:'二进制内容未保存为文本',before_unavailable:'修改前内容不可用',
 permission_state_unavailable:'文件权限状态不可用',unsafe_record_boundary:'不完整尾段未保存',audit_delivery_timeout:'审计交付超时',operation_coverage_incomplete:'部分操作覆盖不完整',
};
