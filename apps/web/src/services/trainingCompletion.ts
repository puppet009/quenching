export interface TrainingCompletionPorts {
  finish(): Promise<unknown>;
  flush?(): Promise<void>;
}

/** Acceptance ends the workbench flow; cleanup and review belong to backend workers. */
export async function completeTraining(sessionId: number, projectId: number, ports: TrainingCompletionPorts): Promise<void> {
  if (!Number.isSafeInteger(sessionId) || sessionId <= 0 || !Number.isSafeInteger(projectId) || projectId < 0) throw new Error("无效的训练会话或项目");
  await ports.flush?.();
  await ports.finish();
}

/** Finish and exit share one request; rejection releases the guard for retry. */
export function createTrainingCompletion() {
  let pending = false;
  let accepted = false;
  return async (sessionId: number, projectId: number, ports: TrainingCompletionPorts): Promise<boolean> => {
    if (pending || accepted) return false;
    pending = true;
    try {
      await completeTraining(sessionId, projectId, ports);
      accepted = true;
      return true;
    } finally {
      pending = false;
    }
  };
}
