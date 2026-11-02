import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import test from 'node:test';
import ts from 'typescript';
const source=await readFile(new URL('../src/services/trainingCompletion.ts',import.meta.url),'utf8');
const out=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText;
const {completeTraining,createTrainingCompletion}=await import('data:text/javascript;base64,'+Buffer.from(out).toString('base64'));
test('accepted finish returns without brain lookup, review polling, or public generation',async()=>{
 const calls=[];
 await completeTraining(7,0,{finish:async()=>calls.push('finish'),brainEnabled:async()=>{calls.push('brain');return true},reviewBySession:async()=>{calls.push('lookup');return {id:19}},generate:async()=>{calls.push('generate');return {id:19}},wait:async()=>calls.push('wait')});
 assert.deepEqual(calls,['finish']);
});

test('finish and exit share one pending request and permit retry after rejection',async()=>{
 assert.equal(typeof createTrainingCompletion,'function');
 const complete=createTrainingCompletion();let resolveFinish;let calls=0;
 const ports={finish:()=>{calls++;return new Promise(resolve=>{resolveFinish=resolve})}};
 const first=complete(7,0,ports);
 assert.equal(await complete(7,0,ports),false);
 resolveFinish();assert.equal(await first,true);assert.equal(calls,1);
 assert.equal(await complete(7,0,ports),false);
 const retryable=createTrainingCompletion();
 await assert.rejects(()=>retryable(7,0,{finish:async()=>{throw Error('offline')}}),/offline/);
 assert.equal(await retryable(7,0,{finish:async()=>{calls++}}),true);
 assert.equal(calls,2);
});

test('operation upload failure prevents finish and permits retry',async()=>{
 const calls=[];
 const complete=createTrainingCompletion();
 await assert.rejects(()=>complete(7,2,{flush:async()=>{calls.push('flush');throw Error('操作记录保存失败，请重试')},finish:async()=>calls.push('finish')}),/操作记录保存失败/);
 assert.deepEqual(calls,['flush']);
 await complete(7,2,{flush:async()=>calls.push('retry upload'),finish:async()=>calls.push('finish')});
 assert.deepEqual(calls,['flush','retry upload','finish']);
});

test('finish waits for operation acknowledgement even beyond 1.5 seconds',async context=>{
 context.mock.timers.enable({apis:['setTimeout']});
 let release;const calls=[];
 const pending=completeTraining(7,2,{flush:()=>new Promise(resolve=>{release=resolve}),finish:async()=>calls.push('finish')});
 context.mock.timers.tick(1700);
 await new Promise(setImmediate);
 assert.deepEqual(calls,[]);
 release();await pending;
 assert.deepEqual(calls,['finish']);
});
test('failed finish cannot generate a report',async()=>{
 let generated=false;await assert.rejects(()=>completeTraining(7,2,{finish:async()=>{throw Error('finish failed')},brainEnabled:async()=>false,reviewBySession:async()=>null,generate:async()=>{generated=true;return {id:1}},wait:async()=>{}}));assert.equal(generated,false);
});
