import * as api from '../services/resources';
import {formatIsoDate} from '@quenching/shared';

interface Props {state?:api.ElasticState;busy:boolean;mutate:(fn:()=>Promise<unknown>,success:string)=>Promise<void>}
const actions:Record<string,string>={freeze:'故障冻结',unfreeze:'故障清理',resize:'提交伸缩',converged:'调整完成',policy:'修改策略'};
export function ElasticResources({state,busy,mutate}:Props){
 return <section className="bg-white border border-ink-200 rounded-xl p-4 space-y-4 min-w-0">
  <h4 className="font-semibold">服务弹性策略</h4>
  <p className="text-xs text-ink-500">只使用资源池内的服务器。CPU、内存申请在策略上下界内调整，资源变更可能触发滚动重启；容量或有效指标不足时保持现状。资源类故障会冻结整个班次的自动调整，全部确认清理后恢复。</p>
  {!state?<p className="text-sm text-amber-700">弹性控制器状态未提供，请检查服务连接。</p>:<>
   {state.holds.length>0&&<div className="bg-amber-50 rounded-lg p-3 space-y-2"><h3 className="text-sm font-semibold text-amber-800">故障冻结中</h3>{state.holds.map(h=><p className="text-sm text-amber-800 break-all" key={`${h.namespace}/${h.id}`}>{h.namespace} · {h.reason}</p>)}<p className="text-xs text-amber-700">叠加故障分别持有冻结；修改策略开关不会解除冻结。</p></div>}
   {state.policies.length===0?<p className="text-sm text-ink-500">暂无已验收的伸缩策略。中间件不会自动增加副本。</p>:<div className="grid gap-3 lg:grid-cols-2">{state.policies.map(p=>{
    const frozen=p.frozen||state.holds.some(h=>h.namespace===p.namespace);
    return <article key={`${p.namespace}/${p.name}`} className="border border-ink-100 rounded-lg p-3 space-y-2 min-w-0"><h5 className="font-medium break-all">{p.namespace} / {p.name}</h5>
     <p className="text-sm">{frozen?'故障冻结':p.pending?'调整待确认':p.enabled?'自动伸缩已启用':'自动伸缩已关闭'} · {p.current.replicas} 个副本</p>
     <p className="text-xs">每副本申请 {api.formatCPU(p.current.requests.cpu_milli)} / {api.formatMemory(p.current.requests.memory_bytes)}</p>
     <p className="text-xs text-ink-500">申请下界 {api.formatCPU(p.min.cpu_milli)} / {api.formatMemory(p.min.memory_bytes)}<br/>申请上界 {api.formatCPU(p.max.cpu_milli)} / {api.formatMemory(p.max.memory_bytes)} · 副本 1–{p.max_replicas}</p>
     <p className="text-xs text-ink-500 break-words">{p.reason}</p>
     <button className="px-3 py-2 border border-ink-200 rounded-lg text-sm disabled:opacity-50 hover:bg-ink-50" disabled={busy||!!p.pending} aria-label={`${p.enabled?'关闭':'启用'}自动伸缩 ${p.name}`} onClick={()=>void mutate(()=>api.setElasticEnabled(p,!p.enabled),'弹性策略已更新')}>{p.enabled?'关闭自动伸缩':'启用自动伸缩'}</button>
    </article>
   })}</div>}
   {state.audit.length>0&&<details><summary className="text-sm cursor-pointer">最近伸缩与冻结记录</summary><div className="space-y-2 mt-3">{state.audit.slice(0,20).map((a,i)=><p key={`${a.created_at}/${i}`} className="text-xs text-ink-500 break-all">{formatIsoDate(a.created_at)} · {a.namespace}/{a.name} · {actions[a.action]||a.action} · {a.detail}</p>)}</div></details>}
  </>}
 </section>
}
