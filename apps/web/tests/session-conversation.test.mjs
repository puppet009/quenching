import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import test from 'node:test';
import ts from 'typescript';
const source=await readFile(new URL('../src/services/sessionConversation.ts',import.meta.url),'utf8');
const out=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText;
const {SessionConversationCoordinator,mergeSessionMessages}=await import('data:text/javascript;base64,'+Buffer.from(out).toString('base64'));
const response=(sid=7,status='running')=>({session_id:sid,conversation_id:sid+100,session_status:status,messages:[{id:2,role:'user',content:'recent'}],next_before_id:2,has_more:true});
const deferred=()=>{let resolve;const promise=new Promise(r=>resolve=r);return {promise,resolve}};
test('restoresBeforeSending',async()=>{const wait=deferred();const c=new SessionConversationCoordinator({restore:()=>wait.promise});const pending=c.restoreForSession(7);assert.equal(c.canSend(7),false);assert.throws(()=>c.requireConversation(7));wait.resolve(response());await pending;assert.equal(c.requireConversation(7),107)});
test('restoreFailureCannotCreateFallbackConversation',async()=>{const c=new SessionConversationCoordinator({restore:async()=>{throw Error('offline')}});await c.restoreForSession(7);assert.equal(c.canSend(7),false);assert.match(c.snapshot().error,/offline/);assert.throws(()=>c.requireConversation(7))});
test('ignoresLateResponseAfterSessionChange',async()=>{const old=deferred();const c=new SessionConversationCoordinator({restore:(sid)=>sid===7?old.promise:Promise.resolve(response(sid))});const pending=c.restoreForSession(7);await c.restoreForSession(8);old.resolve(response(7));await pending;assert.equal(c.snapshot().sessionId,8);assert.equal(c.requireConversation(8),108);assert.equal(c.canSend(7),false)});
test('mergesHistoryWithoutDuplicates',async()=>{const c=new SessionConversationCoordinator({restore:async()=>response(),history:async()=>({messages:[{id:1,role:'user',content:'old'},{id:2,role:'user',content:'recent'}],next_before_id:1,has_more:false})});await c.restoreForSession(7);await c.loadOlder();assert.deepEqual(c.snapshot().messages.map(m=>m.id),[1,2]);assert.equal(c.snapshot().hasMore,false)});
test('closedSessionDisablesSending',async()=>{const c=new SessionConversationCoordinator({restore:async()=>response(7,'completed')});await c.restoreForSession(7);assert.equal(c.canSend(7),false);assert.throws(()=>c.requireConversation(7));assert.equal(c.snapshot().messages.length,1)});

test('historyMergePreservesLiveQuestionAnswerOrder',()=>{const current=[{id:2,role:'user',content:'old'},{id:'new-question',role:'user',content:'new'},{id:4,role:'assistant',content:'answer'}];assert.deepEqual(mergeSessionMessages([{id:1,role:'user',content:'earlier'},{id:2,role:'user',content:'old'}],current).map(m=>m.id),[1,2,'new-question',4])});
test('refreshesSessionStatusWithoutResettingHistory',async()=>{let status='deploying';const c=new SessionConversationCoordinator({restore:async()=>response(7,status)});await c.restoreForSession(7);const oldMessages=c.snapshot().messages;status='running';await c.refreshStatus();assert.equal(c.canSend(7),true);assert.equal(c.snapshot().messages,oldMessages);status='completed';await c.refreshStatus();assert.equal(c.canSend(7),false)});
