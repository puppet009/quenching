/** Uploads are serialized; final completion joins any upload already in progress. */
export function createOperationFlush<T>(queue: T[], upload: (batch: T[]) => Promise<number>, batchSize = 50): () => Promise<void> {
  let pending: Promise<void> | null = null;
  return () => {
    if (pending) return pending;
    const drain = async () => {
      while (queue.length > 0) {
        const batch = queue.splice(0, batchSize);
        let accepted: number;
        try {
          accepted = await upload(batch);
        } catch (cause) {
          queue.unshift(...batch);
          throw new Error("操作记录保存失败，请重试后再结束实训", { cause });
        }
        if (!Number.isSafeInteger(accepted) || accepted < 0 || accepted > batch.length) {
          queue.unshift(...batch);
          throw new Error("操作记录保存状态不确定，请重试后再结束实训");
        }
        queue.unshift(...batch.slice(accepted));
        if (accepted === 0) throw new Error("操作记录尚未保存，请重试后再结束实训");
      }
    };
    pending = drain().finally(() => { pending = null; });
    return pending;
  };
}
