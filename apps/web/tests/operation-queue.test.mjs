import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import test from 'node:test';
import ts from 'typescript';
let createOperationFlush;
try {
 const source=await readFile(new URL('../src/services/operationQueue.ts',import.meta.url),'utf8');
 const output=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText;
 ({createOperationFlush}=await import('data:text/javascript;base64,'+Buffer.from(output).toString('base64')));
} catch(error) { if(error.code!=='ENOENT')throw error; }

test('final flush joins an in-flight upload and drains queued batches before resolving',async()=>{
 assert.equal(typeof createOperationFlush,'function');
 const queue=[1,2,3];const sent=[];let acknowledge;
 const flush=createOperationFlush(queue,async batch=>{
  sent.push([...batch]);
  if(sent.length===1) await new Promise(resolve=>{acknowledge=resolve});
  return batch.length;
 },2);
 const periodic=flush();const final=flush();
 assert.equal(periodic,final);
 queue.push(4,5);let finished=false;final.then(()=>{finished=true});
 await Promise.resolve();assert.equal(finished,false);
 acknowledge();await final;
 assert.deepEqual(sent,[[1,2],[3,4],[5]]);assert.deepEqual(queue,[]);
});

test('failed upload retains records in order for a successful retry',async()=>{
 assert.equal(typeof createOperationFlush,'function');
 const queue=[1,2,3];let fail=true;const sent=[];
 const flush=createOperationFlush(queue,async batch=>{sent.push([...batch]);if(fail)throw Error('offline');return batch.length},2);
 await assert.rejects(flush,/操作记录保存失败/);
 assert.deepEqual(queue,[1,2,3]);fail=false;await flush();
 assert.deepEqual(queue,[]);assert.deepEqual(sent,[[1,2],[1,2],[3]]);
});

test('partial acknowledgement drains only remaining records and zero acknowledgement stays retryable',async()=>{
 assert.equal(typeof createOperationFlush,'function');
 const queue=[1,2];const sent=[];let accepted=1;
 const flush=createOperationFlush(queue,async batch=>{sent.push([...batch]);return Math.min(accepted++,batch.length)},2);
 await flush();assert.deepEqual(sent,[[1,2],[2]]);assert.deepEqual(queue,[]);
 queue.push(3);accepted=0;
 await assert.rejects(flush,/操作记录尚未保存/);assert.deepEqual(queue,[3]);
 await flush();assert.deepEqual(queue,[]);
});
