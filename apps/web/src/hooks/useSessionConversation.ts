import {useEffect,useMemo,useSyncExternalStore} from "react";
import {restoreSessionConversation,fetchSessionConversationHistory} from "../services/chat";
import {SessionConversationCoordinator} from "../services/sessionConversation";
export function useSessionConversation(sessionId:number|undefined,enabled:boolean){
 const coordinator=useMemo(()=>new SessionConversationCoordinator({restore:restoreSessionConversation,history:fetchSessionConversationHistory}),[]);
 const state=useSyncExternalStore(coordinator.subscribe,coordinator.snapshot);
 useEffect(()=>{void coordinator.restoreForSession(enabled?sessionId:undefined);return ()=>coordinator.invalidate()},[coordinator,sessionId,enabled]);
 useEffect(()=>{if(!enabled||!sessionId)return;const timer=setInterval(()=>{void coordinator.refreshStatus()},5000);return ()=>clearInterval(timer)},[coordinator,enabled,sessionId]);
 return {state,coordinator,retry:()=>coordinator.restoreForSession(enabled?sessionId:undefined),loadOlder:()=>coordinator.loadOlder()};
}
