import {http} from '@quenching/shared';
export interface Amounts {cpu_milli:number;memory_bytes:number}
export interface Usage {resources:Amounts;timestamp:string}
export interface PoolNode {name:string;uid:string;resource_version:string;ready:boolean;unschedulable:boolean;eligible:boolean;reason?:string;capacity:Amounts;allocatable:Amounts;requested:Amounts;limits:Amounts;usage:Usage|null;pod_count:number}
export interface Container {name:string;requests:Amounts;limits:Amounts}
export interface Workload {namespace:string;name:string;kind:string;resource_version:string;replicas:number;group:string;containers:Container[]}
export interface Group {requested:Amounts;limits:Amounts;pod_count:number;storage_bytes:number}
export interface ResourcePlan {requests:Amounts;limits:Amounts;storage_bytes:number;components?:{name:string;requests:Amounts}[]}
export interface Allocation {session_id:number;user_id:number;project_id:number;namespace:string;template_slug:string;status:string;resources_admitted:boolean;allocation_reason?:string;resource_plan:ResourcePlan}
export interface AllocationUnit {id:string;kind:'platform'|'sandbox'|'infrastructure';namespace?:string;session_id?:number;user_id?:number;project_id?:number;template_slug?:string;status?:string;resources_admitted?:boolean;allocation_reason?:string;created_at?:string;requested:Amounts;limits:Amounts;reserved:Amounts;pod_count:number;storage_bytes:number;resource_plan?:ResourcePlan;usage:Usage|null;usage_sampled_pods:number}
export interface ElasticPolicy {namespace:string;name:string;uid:string;version:number;session_id?:number;component?:string;enabled:boolean;frozen?:boolean;min:Amounts;max:Amounts;max_replicas:number;current:{replicas:number;requests:Amounts};pending?:{id:string;target:{replicas:number;requests:Amounts}};reason:string}
export interface ElasticState {policies:ElasticPolicy[];holds:{namespace:string;id:string;reason:string;created_at:string}[];audit:{namespace:string;name:string;action:string;detail:string;created_at:string}[]}
export interface Snapshot {allocation_units?:AllocationUnit[];elastic?:ElasticState;observed_at:string;nodes:PoolNode[];workloads:Workload[];groups:Record<string,Group>;available:Amounts;pending:Amounts;reserved?:Amounts;warnings:string[];metrics_available:boolean;allocations:Allocation[];pvcs:{namespace:string;name:string;storage_class:string;phase:string;bytes:number}[]}
const base='/api/admin/v1/resources';
export const snapshot=(signal?:AbortSignal)=>http.get<unknown,Snapshot>(base,{baseURL:'',signal});
export const setScheduling=(n:PoolNode,enabled:boolean)=>http.patch(`${base}/nodes/${encodeURIComponent(n.name)}/scheduling`,{resource_version:n.resource_version,uid:n.uid,enabled},{baseURL:''});
export const removeNode=(n:PoolNode)=>http.delete(`${base}/nodes/${encodeURIComponent(n.name)}`,{baseURL:'',data:{resource_version:n.resource_version,uid:n.uid}});
export const updateWorkload=(w:Workload,c:Container,requests:Amounts,limits:Amounts)=>http.patch(`${base}/workloads/${encodeURIComponent(w.namespace)}/${w.kind}/${encodeURIComponent(w.name)}`,{container:c.name,resource_version:w.resource_version,requests,limits},{baseURL:''});
export const formatCPU=(m:number|undefined)=>m==null?'未知':`${(m/1000).toLocaleString('zh-CN',{maximumFractionDigits:3})} 核`;
export const formatMemory=(b:number|undefined)=>b==null?'未知':`${(b/1073741824).toLocaleString('zh-CN',{maximumFractionDigits:2})} GiB`;
export const setElasticEnabled=(p:ElasticPolicy,enabled:boolean)=>http.patch(`${base}/elastic/${encodeURIComponent(p.namespace)}/${encodeURIComponent(p.name)}`,{version:p.version,enabled},{baseURL:''});
