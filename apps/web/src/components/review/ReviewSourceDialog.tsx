import { useCallback, useEffect, useRef, useState } from 'react';
import { getReviewSource, SourceRequestGate, type ReviewSourceFragment } from '../../services/growthReview';

export function ReviewSourceDialog({ reviewId, reference, onClose }: { reviewId: number; reference: string; onClose: () => void }) {
  const gate = useRef(new SourceRequestGate());
  const [fragment, setFragment] = useState<ReviewSourceFragment>();
  const [text, setText] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const closeRef = useRef<HTMLButtonElement>(null);
  const read = useCallback(async (offset: number) => {
    const ticket = gate.current.begin(reviewId, reference);
    setLoading(true); setError('');
    try {
      const result = await getReviewSource(reviewId, reference, offset, ticket.signal);
      if (!gate.current.isCurrent(ticket)) return;
      setFragment(result); setText(previous => offset === 0 ? result.source.text : previous + result.source.text);
    } catch {
      if (gate.current.isCurrent(ticket)) setError('暂时无法读取此来源，请重试。');
    } finally { if (gate.current.isCurrent(ticket)) setLoading(false); }
  }, [reviewId, reference]);
  useEffect(() => {
    setFragment(undefined); setText(''); void read(0);
    const previous = document.activeElement as HTMLElement | null;
    closeRef.current?.focus();
    return () => { gate.current.cancel(); previous?.focus(); };
  }, [read]);
  useEffect(() => {
    const keydown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
      if (event.key === 'Tab') {
        const buttons = closeRef.current?.closest('[role="dialog"]')?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)');
        if (!buttons?.length) return;
        const first = buttons[0], last = buttons[buttons.length - 1];
        if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
        if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
      }
    };
    document.addEventListener('keydown', keydown); return () => document.removeEventListener('keydown', keydown);
  }, [onClose]);
  return <div className="fixed inset-0 z-50 bg-black/40 flex items-center justify-center p-3" onClick={onClose}>
    <section role="dialog" aria-modal="true" aria-labelledby="review-source-title" className="bg-white rounded-xl w-full max-w-3xl max-h-[85vh] overflow-auto p-5" onClick={event => event.stopPropagation()}>
      <div className="flex justify-between gap-4 mb-3"><h2 id="review-source-title" className="font-semibold break-all">来源原文：{reference}</h2><button ref={closeRef} onClick={onClose} className="shrink-0 text-brand-600">关闭</button></div>
      {fragment?.source.incomplete && <p className="text-sm text-amber-700 mb-3">平台记录标记为不完整，不能据此确认最终结果。</p>}
      <pre className="whitespace-pre-wrap break-all text-xs leading-6 bg-ink-50 rounded p-3">{text}</pre>
      {error && <p role="alert" className="text-sm text-red-600 mt-3">{error}</p>}
      <div className="mt-4 flex gap-3 items-center text-sm">
        {loading ? <span role="status">读取中…</span> : error ? <button className="text-brand-600" onClick={() => void read(fragment?.next_offset_bytes || 0)}>重试读取</button> : fragment && !fragment.complete ? <button className="text-brand-600" onClick={() => void read(fragment.next_offset_bytes)}>读取下一段</button> : fragment ? <span>已读取完整原文</span> : null}
        {fragment && <span className="text-ink-400">{fragment.next_offset_bytes} / {fragment.source.byte_length} 字节</span>}
      </div>
    </section>
  </div>;
}
