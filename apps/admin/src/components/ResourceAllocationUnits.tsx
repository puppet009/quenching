import * as api from '../services/resources';
import {formatIsoDate} from '@quenching/shared';
import {ElasticResources} from './ElasticResources';

interface Props {
 snapshot:api.Snapshot;
 busy:boolean;
 mutate:(fn:()=>Promise<unknown>,success:string)=>Promise<void>;
 edit:(workload:api.Workload,container:api.Container)=>void;
}
const titles={platform:'平台整体',sandbox:'用户沙箱',infrastructure:'集群基础设施'};
const statuses:Record<string,string>={queued:'等待资源',creating:'创建中',deploying:'部署中',ready:'已就绪',running:'运行中',releasing:'释放中',failed:'失败',released:'已释放'};
const button='px-3 py-2 border border-ink-200 rounded-lg hover:bg-ink-50 disabled:opacity-50 text-sm';
const amounts=(value:api.Amounts)=>`${api.formatCPU(value.cpu_milli)} / ${api.formatMemory(value.memory_bytes)}`;

export function ResourceAllocationUnits({snapshot,busy,mutate,edit}:Props){
 const units=snapshot.allocation_units;
 return <section className="space-y-4 min-w-0" aria-labelledby="allocation-units-title">
  <div><h2 id="allocation-units-title" className="font-semibold text-ink-800">资源分配单元</h2><p className="text-xs text-ink-500 mt-2">平台整体和每个用户沙箱分别汇总一次资源占用，服务配置与弹性策略可展开查看。</p></div>
  {!units?<p className="bg-amber-50 text-amber-800 rounded-lg p-3 text-sm">分配单元数据未提供，请升级资源服务后刷新。</p>:units.length===0?<p className="text-sm text-ink-500">暂无资源分配。</p>:units.map(unit=>{
   const belongs=(namespace:string,group?:string)=>unit.kind==='sandbox'?namespace===unit.namespace:unit.kind==='platform'?namespace===unit.namespace||group==='platform':group?group==='infrastructure':!units.some(u=>u.namespace===namespace)&&!namespace.startsWith('sandbox-');
   const workloads=snapshot.workloads.filter(w=>belongs(w.namespace,w.group));
   const pvcs=snapshot.pvcs.filter(p=>belongs(p.namespace));
   const state=snapshot.elastic?{
    policies:snapshot.elastic.policies.filter(p=>belongs(p.namespace)&&(!p.session_id||!unit.session_id||p.session_id===unit.session_id)),
    holds:snapshot.elastic.holds.filter(h=>belongs(h.namespace)),
    audit:snapshot.elastic.audit.filter(a=>belongs(a.namespace)),
   }:undefined;
   const frozen=state?.policies.filter(p=>p.frozen||state.holds.some(h=>h.namespace===p.namespace)).length??0;
   const enabled=state?.policies.filter(p=>p.enabled).length??0;
   const pending=state?.policies.filter(p=>p.pending).length??0;
   const occupied={cpu_milli:unit.requested.cpu_milli+unit.reserved.cpu_milli,memory_bytes:unit.requested.memory_bytes+unit.reserved.memory_bytes};
   return <article key={unit.id} data-allocation-unit={unit.id} className="bg-white border border-ink-200 rounded-xl p-4 space-y-4 min-w-0">
    <div className="flex flex-wrap justify-between gap-2"><div className="space-y-1 min-w-0">
     <h3 className="font-semibold text-ink-800 break-words">{titles[unit.kind]}{unit.kind==='sandbox'?(unit.session_id?` · 班次 #${unit.session_id}`:' · 未关联活跃班次'):''}</h3>
     <p className="text-xs text-ink-500 break-all">{unit.kind==='sandbox'?<>{unit.user_id?`用户 #${unit.user_id} · `:''}{unit.project_id?`项目 #${unit.project_id} · `:''}{unit.template_slug?`${unit.template_slug} · `:''}</>:null}{unit.namespace||'跨基础设施命名空间'} · {unit.pod_count} 个 Pod</p>
    </div>{unit.status?<p className="text-sm text-ink-600">{statuses[unit.status]||unit.status}</p>:null}</div>
    {(state?.holds.length||frozen>0)?<div className="bg-amber-50 text-amber-800 rounded-lg p-3 text-sm space-y-1">
     <p className="font-medium">{state?.holds.length&&unit.kind==='sandbox'?'班次故障冻结':frozen>0&&frozen<(state?.policies.length??0)?'部分服务故障冻结':'故障冻结'}{frozen>0?`（${frozen}/${state?.policies.length} 项策略）`:''}</p>
     {state?.holds.map(h=><p key={`${h.namespace}/${h.id}`} className="break-words">{h.reason}</p>)}
     {state?.policies.filter(p=>p.frozen).map(p=><p key={`${p.namespace}/${p.name}`} className="break-words">{p.name}：{p.reason||'故障冻结中'}</p>)}
    </div>:null}
    <dl className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3 text-sm">
     <div data-unit-occupied><dt className="text-ink-500">申请占用（含预留）</dt><dd className="font-medium mt-1">{amounts(occupied)}</dd></div>
     <div><dt className="text-ink-500">Pod 已申请</dt><dd className="mt-1">{amounts(unit.requested)}</dd></div>
     <div><dt className="text-ink-500">尚未落地的预留</dt><dd className="mt-1">{amounts(unit.reserved)}</dd></div>
     <div data-unit-usage><dt className="text-ink-500">实际使用</dt><dd className="mt-1">{unit.usage?amounts(unit.usage.resources):unit.pod_count>0?`${unit.usage_sampled_pods>0?'未完整采集':'未采集'}（${unit.usage_sampled_pods}/${unit.pod_count} Pod）`:'未采集（无 Pod）'}</dd>{unit.usage?<dd className="text-xs text-ink-400 mt-1">采样于 {formatIsoDate(unit.usage.timestamp)}</dd>:null}</div>
     <div><dt className="text-ink-500">已配置上限合计</dt><dd className="mt-1">{amounts(unit.limits)}</dd></div>
     <div><dt className="text-ink-500">PVC 存储申请</dt><dd className="mt-1">{api.formatMemory(unit.storage_bytes)}</dd></div>
    </dl>
    {unit.resource_plan?<div className="border-t border-ink-100 pt-3 text-xs text-ink-500 space-y-1"><p>{unit.resources_admitted?'班次预算已准入':'尚未准入，计划不计入占用'} · 计划申请 {amounts(unit.resource_plan.requests)}</p><p>班次预算上限 {amounts(unit.resource_plan.limits)} · 存储预算 {api.formatMemory(unit.resource_plan.storage_bytes)}</p></div>:null}
    {unit.allocation_reason?<p className="text-sm text-amber-700 break-words">{unit.allocation_reason}</p>:null}
    <p className="text-xs text-ink-500">{state?state.policies.length?`自动伸缩：已启用 ${enabled} / 已关闭 ${state.policies.length-enabled} 项策略${pending?` · ${pending} 项调整待确认`:''}`:'暂无已验收的服务伸缩策略':'弹性控制器状态未提供'}</p>
    <details className="border-t border-ink-100 pt-3">
     <summary className="cursor-pointer text-sm text-ink-600">服务明细与弹性策略（{workloads.length} 个工作负载）</summary>
     <div className="space-y-4 mt-4">
      <div className="space-y-3"><h4 className="font-medium text-sm">服务资源配置</h4>{unit.kind==='platform'?<p className="text-xs text-ink-500">调整会更新容器资源并可能触发滚动重启；后续部署需同步到部署配置，避免覆盖。</p>:null}
       {workloads.length===0?<p className="text-sm text-ink-500">暂无可见工作负载。</p>:<div className="grid gap-3 lg:grid-cols-2">{workloads.flatMap(w=>w.containers.map(c=><article key={`${w.namespace}/${w.kind}/${w.name}/${c.name}`} className="border border-ink-100 rounded-lg p-3 space-y-2 min-w-0"><h5 className="font-medium break-all">{w.name} / {c.name}</h5><p className="text-xs text-ink-500 break-all">{w.namespace} · {w.kind} · {w.replicas} 个副本</p><p className="text-xs">每副本申请 {amounts(c.requests)}<br/>每副本上限 {amounts(c.limits)}</p>{unit.kind==='platform'?<button className={button} disabled={busy} aria-label={`调整资源 ${w.name} / ${c.name}`} onClick={()=>edit(w,c)}>调整资源</button>:null}</article>))}</div>}
      </div>
      <ElasticResources state={state} busy={busy} mutate={mutate}/>
      {pvcs.length>0?<div className="space-y-2"><h4 className="font-medium text-sm">存储明细</h4>{pvcs.map(p=><p key={`${p.namespace}/${p.name}`} className="text-xs text-ink-500 break-all">{p.namespace} / {p.name} · {p.phase} · {api.formatMemory(p.bytes)} · {p.storage_class||'默认存储类'}</p>)}</div>:null}
     </div>
    </details>
   </article>;
  })}
 </section>;
}
