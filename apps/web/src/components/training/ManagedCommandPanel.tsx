import {useEffect,useRef,useState} from 'react';
import {cancelManagedCommand,getManagedCommand,startManagedCommand,type CommandRequest,type OperationEvidence} from '../../services/operations';
import {CommandSubmission,OperationScope,operationStatus} from '../../services/operationState';
import type {ServerTarget} from '../../services/sandbox';
import {OperationTimeline} from './OperationTimeline';

export function ManagedCommandPanel({sessionId,targets,enabled}:{sessionId:number;targets:ServerTarget[];enabled:boolean}) {
 const [pod,setPod]=useState('');const [container,setContainer]=useState('');const [cwd,setCwd]=useState('/');const [command,setCommand]=useState('');const [intent,setIntent]=useState('diagnose');const [timeout,setTimeout]=useState(120);
 const [result,setResult]=useState<OperationEvidence>();const [error,setError]=useState('');const [busy,setBusy]=useState(false);const [uncertain,setUncertain]=useState(false);
 const submit=useRef(new CommandSubmission());const request=useRef<CommandRequest | undefined>(undefined);const scope=useRef(new OperationScope());const ticket=useRef<ReturnType<OperationScope['begin']>|null>(null);const locked=useRef(false);
 useEffect(()=>{const t=scope.current.begin(sessionId);ticket.current=t;submit.current.newExecution();request.current=undefined;setResult(undefined);setError('');setBusy(false);setUncertain(false);locked.current=false;setCommand('');setPod('');setContainer('');return()=>scope.current.cancel()},[sessionId]);
 useEffect(()=>{if(!result||!['pending','running'].includes(result.status))return;const t=ticket.current;if(!t)return;const timer=setInterval(()=>{void getManagedCommand(sessionId,result.operation_id,t.signal).then(e=>{if(scope.current.current(t)){setResult(e);setError('')}}).catch(e=>{if(scope.current.current(t))setError(`状态查询未确认：${String(e)}`)})},1000);return()=>clearInterval(timer)},[sessionId,result?.operation_id,result?.status]);
 const selected=targets.find(t=>t.name===pod);const effectiveContainer=container||selected?.containers[0]?.name||'';
 const run=async()=>{const t=ticket.current;if(!t||locked.current||!enabled)return;locked.current=true;setBusy(true);setError('');const r=request.current??{key:submit.current.begin(),pod,container:effectiveContainer,cwd,command,intent,timeout_seconds:timeout};request.current=r;
  try{const e=await startManagedCommand(sessionId,r,t.signal);if(scope.current.current(t)){setResult(e);setUncertain(false)}}catch(e){if(scope.current.current(t)){setUncertain(true);setError(`提交结果未确认：${String(e)}。可以重试同一次提交，平台不会重复执行。`)}}finally{if(scope.current.current(t)){locked.current=false;setBusy(false)}}};
 const cancel=async()=>{const t=ticket.current;if(!t||!result)return;setBusy(true);try{const e=await cancelManagedCommand(sessionId,result.operation_id,t.signal);if(scope.current.current(t)){setResult(e);setError('已请求取消；远端是否已产生修改，以最终记录为准。')}}catch(e){if(scope.current.current(t))setError(String(e))}finally{if(scope.current.current(t))setBusy(false)}};
 const active=!!result&&['pending','running'].includes(result.status);const readOnly=busy||active||uncertain||!!result||!enabled;
 const newExecution=()=>{submit.current.newExecution();request.current=undefined;setResult(undefined);setUncertain(false);setError('')};
 return <div className="p-4 overflow-y-auto h-full space-y-4 min-w-0"><p className="text-sm text-ink-600">通过这里提交的命令会记录目标、退出码及输出。复合命令按一次提交记录，脚本内部语句不会逐条展开。</p>
 <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 text-sm">
 <label>服务器<select aria-label="服务器" disabled={readOnly} value={pod} onChange={e=>{setPod(e.target.value);setContainer('')}} className="block border rounded p-2 w-full"><option value="">选择服务器</option>{targets.filter(t=>t.ready).map(t=><option key={t.name} value={t.name}>{t.displayName||t.name}</option>)}</select></label>
 <label>容器<select aria-label="容器" disabled={readOnly} value={effectiveContainer} onChange={e=>setContainer(e.target.value)} className="block border rounded p-2 w-full">{selected?.containers.map(c=><option key={c.name} value={c.name}>{c.name}</option>)}</select></label>
 <label>工作目录<input aria-label="工作目录" disabled={readOnly} value={cwd} onChange={e=>setCwd(e.target.value)} className="block border rounded p-2 w-full"/></label>
 <label>超时（秒，1–300）<input aria-label="超时" type="number" min={1} max={300} disabled={readOnly} value={timeout} onChange={e=>setTimeout(Number(e.target.value))} className="block border rounded p-2 w-full"/></label>
 <label>本次意图<select aria-label="本次意图" disabled={readOnly} value={intent} onChange={e=>setIntent(e.target.value)} className="block border rounded p-2 w-full"><option value="diagnose">诊断</option><option value="repair">修复（仍需业务验证）</option></select></label>
 </div><label className="block text-sm">命令<textarea aria-label="命令" rows={4} maxLength={16384} disabled={readOnly} value={command} onChange={e=>setCommand(e.target.value)} className="mt-1 block border rounded p-2 w-full font-mono resize-y"/></label>
 <div className="flex flex-wrap gap-3"><button type="button" disabled={busy||active||!!result||!enabled||!pod||!command.trim()} onClick={()=>void run()} className="rounded bg-brand-500 text-white px-4 py-2 disabled:opacity-50">{uncertain?'重试同一次提交':'执行命令'}</button>{active&&<button type="button" disabled={busy||!enabled} onClick={()=>void cancel()} className="border rounded px-4 py-2">请求取消</button>}{(result&&!active||uncertain)&&<button type="button" disabled={busy||!enabled} onClick={newExecution} className="border rounded px-4 py-2">准备新的执行</button>}</div>
 {!enabled&&<p role="status">班次已结束，不能开始新的执行。</p>}{error&&<p role="alert" className="text-amber-800 break-words">{error}</p>}{result&&<p role="status">{operationStatus(result)} · {result.operation_id}</p>}
 <OperationTimeline key={sessionId} sessionId={sessionId}/></div>;
}
