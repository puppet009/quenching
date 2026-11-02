import { http } from '@quenching/shared';
export interface OperationEvidence {
 event_id:string;operation_id:string;session_id:number;sequence:number;actor:string;kind:string;status:string;
 command?:string;intent?:string;tool?:string;approval?:string;approval_id?:string;conversation_id?:number;reply_message_id?:number;
 started_at:string;finished_at?:string;exit_code?:number|null;
 target:{pod?:string;container?:string;cwd?:string};coverage:{command?:string;execution?:string;output?:string;before?:string;after?:string;reason_codes?:string[]};text_refs?:string[];
}
export interface OperationPage {items:OperationEvidence[];next_before:number;has_more:boolean}
export interface OperationFragment {ref:string;text:string;offset_bytes:number;next_offset_bytes:number;complete:boolean}
export interface CommandRequest {key:string;pod:string;container:string;cwd:string;command:string;intent:string;timeout_seconds:number}
export function startManagedCommand(sid:number,request:CommandRequest,signal?:AbortSignal):Promise<OperationEvidence> {return http.post(`/sandboxes/${sid}/commands`,request,{signal});}
export function getManagedCommand(sid:number,id:string,signal?:AbortSignal):Promise<OperationEvidence> {return http.get(`/sandboxes/${sid}/commands/${encodeURIComponent(id)}`,{signal});}
export function cancelManagedCommand(sid:number,id:string,signal?:AbortSignal):Promise<OperationEvidence> {return http.post(`/sandboxes/${sid}/commands/${encodeURIComponent(id)}/cancel`,{}, {signal});}
export function listSessionOperations(sid:number,before=0,signal?:AbortSignal):Promise<OperationPage> {return http.get(`/sessions/${sid}/operations`,{params:{before_sequence:before,limit:20},signal});}
export function readOperationSource(sid:number,ref:string,offset=0,signal?:AbortSignal):Promise<OperationFragment> {return http.get(`/sessions/${sid}/operations/source`,{params:{ref,offset_bytes:offset,limit_bytes:8192},signal});}
