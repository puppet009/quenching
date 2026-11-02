import {http, listProjects, type Defect, type Template as OfficialTemplate} from "@quenching/shared";
export type Template = OfficialTemplate;
export interface Binding {executor:string;template_slug:string;target_workload:string;status:"draft"|"pending_validation"|"verified";overrides:Record<string,unknown>;evidence:Record<string,unknown>}
export interface ManagedDefect extends Defect {provenance:Record<string,unknown>;template_bindings:Binding[]}
export interface Page<T> {items:T[];total:number;page:number;page_size:number}
export const templates=()=>http.get<unknown,Template[]>("/templates");
export async function loadDefectCatalog() {
  const [templateResult, projectResult] = await Promise.allSettled([
    templates(), listProjects({size:100}),
  ]);
  const errors: string[] = [];
  if (templateResult.status === "rejected") errors.push(`官方模板加载失败：${errorMessage(templateResult.reason)}`);
  if (projectResult.status === "rejected") errors.push(`登记项目加载失败：${errorMessage(projectResult.reason)}`);
  return {
    templates: templateResult.status === "fulfilled" ? templateResult.value ?? [] : [],
    projects: projectResult.status === "fulfilled" ? projectResult.value.list ?? [] : [],
    errors,
  };
}
export const defects=(params:Record<string,unknown>,signal?:AbortSignal)=>http.get<unknown,Page<ManagedDefect>>("/api/admin/v1/defects",{baseURL:"",params,signal});
export const saveDefect=(id:number|null,data:Record<string,unknown>)=>id?http.put<unknown,ManagedDefect>(`/api/admin/v1/defects/${id}`,data,{baseURL:""}):http.post<unknown,ManagedDefect>("/api/admin/v1/defects",data,{baseURL:""});
export const setDefectActive=(id:number,active:boolean)=>http.patch(`/api/admin/v1/defects/${id}/active`,{is_active:active},{baseURL:""});
export const deleteDefect=(id:number)=>http.delete(`/api/admin/v1/defects/${id}`,{baseURL:""});
export const acceptBinding=(id:number,slug:string,evidence:Record<string,unknown>)=>http.post(`/api/admin/v1/defects/${id}/bindings/${encodeURIComponent(slug)}/accept`,{evidence},{baseURL:""});
export interface Dashboard {stats:Record<string,number|null|{ts:number;value:number}[]|Record<string,unknown>>;unavailable:string[]}
export const dashboard=()=>http.get<unknown,Dashboard>("/admin/dashboard");
export interface Sandbox {id:number;session_id:number;user_id:number;project_id:number;namespace:string;status:string;created_at:string;last_activity_at?:string}
export interface Metrics {cpu_percent:number;memory_percent:number;timestamp:string;pod_count:number;pod_running:number;pod_abnormal:number}
export interface SandboxDetail extends Sandbox {runtime_seconds:number;metrics?:Metrics}
export interface Anomaly {id:number;session_id:number;message:string;level:string;detected_at:string;resolved:boolean}
export interface SandboxStats {total:number;active:number;by_status:Record<string,number>;unresolved_anomalies:number}
export const sandboxes=(params:Record<string,unknown>,signal?:AbortSignal)=>http.get<unknown,Page<Sandbox>>("/api/admin/v1/sandboxes",{baseURL:"",params,signal});
export const sandboxStats=()=>http.get<unknown,SandboxStats>("/api/admin/v1/sandboxes/stats",{baseURL:""});
export const sandboxDetail=(session:number,signal?:AbortSignal)=>http.get<unknown,SandboxDetail>(`/api/admin/v1/sandboxes/${session}`,{baseURL:"",signal});
export const anomalies=(params:Record<string,unknown>,signal?:AbortSignal)=>http.get<unknown,Page<Anomaly>>("/api/admin/v1/anomalies",{baseURL:"",params,signal});
export const releaseSandbox=(session:number)=>http.delete(`/api/admin/v1/sandboxes/${session}`,{baseURL:"",params:{reason:"管理员手动释放"}});
export function errorMessage(err:unknown){const msg=(err as {response?:{data?:{msg?:unknown}}})?.response?.data?.msg;return typeof msg==="string"?msg:err instanceof Error?err.message:"请求失败，请重试"}
