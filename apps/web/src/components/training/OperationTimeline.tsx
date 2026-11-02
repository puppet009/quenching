import {resolveApproval} from "../../services/chat";
import {useEffect,useRef,useState} from 'react';
import {listSessionOperations,readOperationSource,type OperationEvidence,type OperationFragment} from '../../services/operations';
import {OperationScope,operationStatus,coverageReasons,GAP_LABELS} from '../../services/operationState';

export function OperationTimeline({sessionId,actor,canApprove=false}:{sessionId:number;actor?:string;canApprove?:boolean}) {
 const scope=useRef(new OperationScope());const ticket=useRef<ReturnType<OperationScope['begin']>|null>(null);
 const [items,setItems]=useState<OperationEvidence[]>([]);const [before,setBefore]=useState(0);const [more,setMore]=useState(false);const [error,setError]=useState('');
 const [source,setSource]=useState<OperationFragment>();const [busy,setBusy]=useState(false);const reading=useRef(false);const [historyMode,setHistoryMode]=useState(false);
 useEffect(()=>{
  const t=scope.current.begin(sessionId);ticket.current=t;setItems([]);setBefore(0);setSource(undefined);setError('');reading.current=false;setHistoryMode(false);
  const load=async()=>{try{const p=await listSessionOperations(sessionId,0,t.signal);if(!scope.current.current(t))return;setItems(p.items??[]);setBefore(p.next_before);setMore(p.has_more);setError('')}catch(e){if(scope.current.current(t))setError(e instanceof Error?e.message:'记录暂不可用')}};
  void load();const interval=setInterval(()=>{if(!reading.current)void load()},3000);
  return()=>{clearInterval(interval);scope.current.cancel()};
 },[sessionId]);
 const older=async()=>{const t=ticket.current;if(!t)return;setBusy(true);reading.current=true;setHistoryMode(true);try{const p=await listSessionOperations(sessionId,before,t.signal);if(scope.current.current(t)){setItems(old=>[...old,...p.items.filter(v=>!old.some(x=>x.event_id===v.event_id))]);setBefore(p.next_before);setMore(p.has_more)}}catch(e){if(scope.current.current(t))setError(String(e))}finally{if(scope.current.current(t))setBusy(false)}};
 const resume=async()=>{const t=ticket.current;if(!t)return;setBusy(true);try{const p=await listSessionOperations(sessionId,0,t.signal);if(scope.current.current(t)){setItems(p.items??[]);setBefore(p.next_before);setMore(p.has_more);setError('');reading.current=false;setHistoryMode(false)}}catch(e){if(scope.current.current(t))setError(String(e))}finally{if(scope.current.current(t))setBusy(false)}};
 const open=async(ref:string,offset=0)=>{const t=ticket.current;if(!t)return;setBusy(true);try{const f=await readOperationSource(sessionId,ref,offset,t.signal);if(scope.current.current(t))setSource(old=>offset&&old?.ref===ref?{...f,text:old.text+f.text}:f)}catch(e){if(scope.current.current(t))setError(String(e))}finally{if(scope.current.current(t))setBusy(false)}};
 const approval=async(e:OperationEvidence,approved:boolean)=>{const t=ticket.current;if(!t||!e.approval_id||!e.conversation_id||!canApprove)return;setBusy(true);try{await resolveApproval(e.conversation_id!,e.approval_id,approved);if(scope.current.current(t))setError('审批已提交，等待真实执行结果。')}catch(err){if(scope.current.current(t))setError(`审批未确认：${String(err)}`)}finally{if(scope.current.current(t))setBusy(false)}};
 // Immutable events are paginated; display the latest revision of each operation loaded so far.
 const latest=new Map<string,OperationEvidence>();for(const e of items){if(!latest.has(e.operation_id)||latest.get(e.operation_id)!.sequence<e.sequence)latest.set(e.operation_id,e)}
 return <section aria-label="操作记录" className="space-y-3 text-sm min-w-0"><h3 className="font-semibold">{actor==='assistant'?'助手工具记录':'班次操作记录'}</h3>{error&&<p role="alert" className="text-red-600 break-words">{error}</p>}
 {[...latest.values()].filter(e=>!actor||e.actor===actor).map(e=><article key={e.operation_id} className="border border-ink-200 rounded-lg p-3 space-y-2 break-words"><div>{({student:'学员',assistant:'助手',platform:'平台'} as Record<string,string>)[e.actor]||e.actor} · {e.tool||e.kind} · {operationStatus(e)}</div>{e.reply_message_id&&<p className="text-xs text-ink-500">关联回复 #{e.reply_message_id}</p>}{e.command&&<pre className="whitespace-pre-wrap break-all">{e.command}</pre>}{e.approval&&<p>审批：{e.approval}</p>}{e.status==="pending" && e.approval==="requested" && e.approval_id && canApprove && <div className="flex gap-3"><button type="button" disabled={busy} onClick={()=>void approval(e,true)}>确认这次修改</button><button type="button" disabled={busy} onClick={()=>void approval(e,false)}>拒绝修改</button></div>}{coverageReasons(e.coverage).length>0&&<p className="text-amber-700">{coverageReasons(e.coverage).map(g=>GAP_LABELS[g]||g).join('；')}</p>}<div className="flex flex-wrap gap-2">{e.text_refs?.map(ref=><button type="button" key={ref} disabled={busy} onClick={()=>void open(ref)} className="text-brand-600 break-all text-left">读取 {ref.split(':').at(-1)}</button>)}</div></article>)}
 {items.length===0&&!error&&<p className="text-ink-500">暂无已交付记录。</p>}{more&&<button type="button" disabled={busy} onClick={()=>void older()} className="text-brand-600">读取更早操作</button>}
 {historyMode&&<button type="button" disabled={busy} onClick={()=>void resume()} className="text-brand-600">恢复实时记录</button>}
 {source&&<div className="border rounded-lg p-3 space-y-2"><button type="button" onClick={()=>setSource(undefined)}>关闭原文</button><pre className="whitespace-pre-wrap break-all max-h-64 overflow-y-auto">{source.text}</pre>{!source.complete&&<button type="button" disabled={busy} onClick={()=>void open(source.ref,source.next_offset_bytes)} className="text-brand-600">继续读取原文</button>}</div>}
 </section>;
}
