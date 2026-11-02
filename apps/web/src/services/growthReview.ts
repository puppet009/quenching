import { http } from '@quenching/shared';

export interface DimensionAssessment { level: string; confidence: number; evidence_refs?: string[] }
export interface Assessment {
  overall_level: string; sufficiency?: string; dimensions: Record<string, DimensionAssessment>;
  source_refs?: string[]; baseline?: Assessment;
}
export interface GrowthClaim {
  section: string; category: string; text: string; source_refs: string[];
  challenge_id?: string; comparison_challenge_ids?: string[]; source_urls?: string[];
}
export interface ArchitectureOption {
  problem: string; option: string; preconditions: string; benefits: string; costs: string;
  verification: string; source_refs: string[];
}
export interface Practice {
  dimension: string; goal: string; actions: string[]; success_criteria: string;
  source_refs: string[]; project_id?: number; defect_ids?: number[]; target_available?: boolean;
}
export interface GrowthReport {
  schema_version: number;
  overview: { session_id?: number; source_count?: number; audit_complete?: boolean; audit_coverage?:unknown; audit_gaps?:string[]; cleanup_status?: string };
  claims: GrowthClaim[]; ability: Assessment; architecture: ArchitectureOption[]; practices: Practice[];
}
export interface ReviewSourceFragment {
  source: { id: string; text: string; trust: string; byte_length: number; incomplete?: boolean };
  offset_bytes: number; next_offset_bytes: number; complete: boolean;
}
export interface GrowthProgress {
  phase: string; processed_sources: number; total_sources: number; audit_complete: boolean; error?: string;
}
export const DIMENSION_LABELS: Record<string, string> = {
  localization: '问题定位', evidence: '证据判断', mechanism: '原理理解', repair: '修复能力',
  regression: '回归验证', architecture: '架构判断', hint_dependence: '独立排查',
};
const LEVEL_LABELS: Record<string, string> = { junior: '初级', intermediate: '中级', senior: '高级', beginner: '入门' };
export function formatAbility(d: DimensionAssessment): string {
  return !d.level || d.level === 'unknown' ? '未观察' : LEVEL_LABELS[d.level] || d.level;
}
export function compareAbility(baseline: Assessment | undefined, _latest: Assessment): { hasBaseline: boolean } {
  return { hasBaseline: !!baseline };
}
export function resolvePracticeLink(p: Practice): string | undefined {
  return p.target_available && Number.isSafeInteger(p.project_id) && p.project_id! > 0 ? `/projects/${p.project_id}` : undefined;
}
export interface SourceTicket { reviewId: number; reference: string; generation: number; signal: AbortSignal }
export class SourceRequestGate {
  private generation = 0;
  private controller?: AbortController;
  begin(reviewId: number, reference: string): SourceTicket {
    this.cancel(); this.controller = new AbortController();
    return { reviewId, reference, generation: this.generation, signal: this.controller.signal };
  }
  isCurrent(ticket: SourceTicket): boolean { return ticket.generation === this.generation && !ticket.signal.aborted; }
  cancel(): void { this.controller?.abort(); this.generation += 1; }
}
export async function getReviewSource(reviewId: number, reference: string, offsetBytes = 0, signal?: AbortSignal): Promise<ReviewSourceFragment> {
  return http.get(`/reviews/${reviewId}/source`, { params: { reference, offset_bytes: offsetBytes, limit_bytes: 8192 }, signal });
}
export async function getGrowthProgress(reviewId: number, signal?: AbortSignal): Promise<GrowthProgress> {
  return http.get(`/reviews/${reviewId}/progress`, { signal });
}
