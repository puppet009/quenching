import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import test from 'node:test';
import ts from 'typescript';

let history={};
try {
 const source=await readFile(new URL('../src/services/trainingHistory.ts',import.meta.url),'utf8');
 const out=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText;
 history=await import('data:text/javascript;base64,'+Buffer.from(out).toString('base64'));
} catch (error) { if (error.code!=='ENOENT') throw error; }

test('only ended sessions without reports count as pending background work',()=>{
 assert.equal(typeof history.trainingReviewState,'function');
 for(const status of ['pending','running','preparing']) assert.equal(history.trainingReviewState({status,hasReview:false}),'active');
 assert.equal(history.trainingReviewState({status:'running',hasReview:true,reviewId:99}),'active');
 for(const status of ['completed','aborted','failed']) assert.equal(history.trainingReviewState({status,hasReview:false}),'pending');
 assert.equal(history.trainingReviewState({hasReview:false}),'active');
});

test('pending session becomes a report entry when the coordinator supplies an id',()=>{
 assert.equal(typeof history.trainingReviewState,'function');
 const session={status:'completed',hasReview:false,brainPhase:'closing',cleanupStatus:'pending'};
 assert.equal(history.trainingReviewState(session),'pending');
 assert.equal(history.trainingReviewState({...session,hasReview:true,reviewId:99}),'available');
});

test('a report row provides an entry without claiming generation completed',()=>{
 assert.equal(typeof history.trainingReviewLabel,'function');
 assert.equal(history.trainingReviewLabel({status:'completed',hasReview:true,reviewId:99,brainPhase:'reviewing'}),'复盘记录已创建');
});

test('review lookup outage is visible without pretending the report is pending',()=>{
 assert.equal(typeof history.trainingReviewState,'function');
 assert.equal(history.trainingReviewState({status:'completed',hasReview:false,reviewLookupFailed:true}),'unavailable');
 assert.equal(history.trainingReviewState({status:'completed',hasReview:false,reviewLookupFailed:true,brainPhase:'closing'}),'pending');
});

test('ended training pagination sends the server filter and requested page',async()=>{
 const moduleURL=source=>'data:text/javascript;base64,'+Buffer.from(source).toString('base64');
 const shared=moduleURL('export const http={get:async url=>url}; export const formatIsoMinute=value=>value;');
 const radar=moduleURL('export const buildTechRadar=()=>({}); export const buildCollabRadar=()=>({});');
 const source=await readFile(new URL('../src/services/profile.ts',import.meta.url),'utf8');
 const output=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText.replaceAll('"@quenching/shared"',JSON.stringify(shared)).replaceAll('"./radar"',JSON.stringify(radar));
 const {fetchSessions}=await import(moduleURL(output));
 const request=await fetchSessions({page:2,pageSize:20,status:'finished'});
 const query=new URL(request,'https://fixture.invalid').searchParams;
 assert.equal(query.get('status'),'finished');
 assert.equal(query.get('page'),'2');
 assert.equal(query.get('pageSize'),'20');
});
