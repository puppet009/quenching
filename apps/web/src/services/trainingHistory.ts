export interface TrainingClosureStatus {
  status?: string;
  brainPhase?: string;
  cleanupStatus?: string;
  reviewLookupFailed?: boolean;
}

export interface TrainingReviewSession extends TrainingClosureStatus {
  hasReview: boolean;
  reviewId?: number | null;
}

export function trainingReviewState(session: TrainingReviewSession): "active" | "pending" | "unavailable" | "available" {
  const ended = ["completed", "aborted", "failed"].includes(session.status || "");
  if (session.status && !ended) return "active";
  if (session.hasReview && session.reviewId) return "available";
  if (!ended) return "active";
  if (session.reviewLookupFailed && session.brainPhase !== "closing") return "unavailable";
  return "pending";
}

export function trainingReviewLabel(session: TrainingReviewSession): string {
  const state = trainingReviewState(session);
  if (state === "available") return "复盘记录已创建";
  if (state === "unavailable") return "复盘状态暂不可用";
  if (session.cleanupStatus === "failed") return "环境回收待重试";
  if (session.brainPhase === "closing") return "环境回收中，复盘准备中";
  if (session.brainPhase === "reviewing") return "复盘生成中";
  return "复盘准备中";
}
