import type {ChatMessage,SessionHistoryPage,SessionConversationRestore} from "./chat";
export interface SessionConversationState {
 sessionId?:number; conversationId?:number; status:string; loading:boolean; error:string;
 messages:ChatMessage[]; nextBeforeId:number; hasMore:boolean; loadingHistory:boolean; historyError:string;
}
interface Ports {
 restore:(id:number,signal:AbortSignal)=>Promise<SessionConversationRestore>;
 history?:(id:number,before:number,signal:AbortSignal)=>Promise<SessionHistoryPage>;
}
export function mergeSessionMessages(older:ChatMessage[],current:ChatMessage[]):ChatMessage[]{
 // Preserve the live question/answer order even while IDs are temporary.
 const currentIds=new Set(current.map(m=>m.id));
 const prefix=new Map<number|string,ChatMessage>();for(const m of older)if(!currentIds.has(m.id))prefix.set(m.id,m);
 const added=[...prefix.values()].sort((a,b)=>typeof a.id==="number"&&typeof b.id==="number"?a.id-b.id:0);
 return [...added,...current];
}
const convert=(m:SessionHistoryPage["messages"][number]):ChatMessage=>({id:m.id,role:m.role,content:m.content,createdAt:m.created_at,isPitfall:m.is_pitfall,pitfallType:m.pitfall_type,confidenceLevel:m.confidence_level});
const initial=():SessionConversationState=>({status:"",loading:false,error:"",messages:[],nextBeforeId:0,hasMore:false,loadingHistory:false,historyError:""});
export class SessionConversationCoordinator {
 private statusController?:AbortController;
 private state=initial();private generation=0;private controller?:AbortController;private historyController?:AbortController;private listeners=new Set<()=>void>();
 constructor(private ports:Ports){}
 snapshot=()=>this.state;
 subscribe=(listener:()=>void)=>{this.listeners.add(listener);return ()=>{this.listeners.delete(listener)}};
 private update(patch:Partial<SessionConversationState>){this.state={...this.state,...patch};for(const listener of this.listeners)listener()}
 invalidate(){this.generation++;this.controller?.abort();this.historyController?.abort();this.statusController?.abort()}
 async restoreForSession(id?:number){
  this.invalidate();const generation=this.generation;this.controller=new AbortController();const signal=this.controller.signal;
  this.state={...initial(),sessionId:id,loading:!!id};this.update({});if(!id)return;
  try{const data=await this.ports.restore(id,signal);if(generation!==this.generation||signal.aborted)return;
   if(data.session_id!==id||!Number.isSafeInteger(data.conversation_id)||data.conversation_id<=0||!Array.isArray(data.messages))throw Error("班次对话恢复数据不完整，请重试");
   this.update({conversationId:data.conversation_id,status:data.session_status,messages:data.messages.map(convert),nextBeforeId:data.next_before_id,hasMore:data.has_more,loading:false});
  }catch(e){if(generation!==this.generation||signal.aborted)return;this.update({loading:false,error:(e as Error)?.message||"对话恢复失败，请重试"})}
 }
 async refreshStatus(){
  const {sessionId,conversationId}=this.state;if(!sessionId||!conversationId||this.state.loading||this.state.error)return;
  const generation=this.generation;this.statusController?.abort();this.statusController=new AbortController();const signal=this.statusController.signal;
  try{const data=await this.ports.restore(sessionId,signal);if(generation!==this.generation||signal.aborted)return;
   if(data.session_id===sessionId&&data.conversation_id===conversationId)this.update({status:data.session_status});
  }catch{if(generation===this.generation&&!signal.aborted)this.update({status:"unavailable"})}
 }
 canSend(id?:number){return !!id&&this.state.sessionId===id&&!this.state.loading&&!this.state.error&&this.state.status==="running"&&!!this.state.conversationId}
 requireConversation(id?:number){if(!this.canSend(id))throw Error(this.state.error||"班次对话尚未恢复或班次已结束");return this.state.conversationId!}
 async loadOlder(){
  const {sessionId,nextBeforeId,hasMore,loadingHistory}=this.state;if(!sessionId||!hasMore||loadingHistory||!this.ports.history)return;
  const generation=this.generation;this.historyController=new AbortController();const signal=this.historyController.signal;this.update({loadingHistory:true,historyError:""});
  try{const page=await this.ports.history(sessionId,nextBeforeId,signal);if(generation!==this.generation||signal.aborted)return;
   this.update({messages:mergeSessionMessages(page.messages.map(convert),this.state.messages),nextBeforeId:page.next_before_id,hasMore:page.has_more,loadingHistory:false});
  }catch(e){if(generation!==this.generation||signal.aborted)return;this.update({loadingHistory:false,historyError:(e as Error)?.message||"历史读取失败，请重试"})}
 }
}
