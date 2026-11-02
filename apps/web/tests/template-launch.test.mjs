import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import test from 'node:test';
import ts from 'typescript';
const source=await readFile(new URL('../src/services/templateLaunch.ts',import.meta.url),'utf8');
const out=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText;
const {launchTemplate}=await import('data:text/javascript;base64,'+Buffer.from(out).toString('base64'));
test('retry waiting uses the retained session and never creates another',async()=>{
 let retained;let created=0;let waits=0;const ids=[];
 const ports={read:()=>retained,remember:id=>retained=id,clear:()=>retained=undefined,detail:async()=>({status:'pending'}),progress:async()=>({status:'queued'}),create:async()=>{created++;return {id:9}},sandbox:async id=>ids.push(id),wait:async()=>{if(waits++===0)throw Error('waiting timeout')}};
 await assert.rejects(launchTemplate(ports));assert.equal(retained,9);
 assert.equal(await launchTemplate(ports),9);assert.equal(created,1);assert.deepEqual(ids,[9,9]);assert.equal(retained,undefined);
});
test('ended environment clears the stale application, network errors retain it',async()=>{
 let retained=9;let created=0;let mode='released';const ports={read:()=>retained,remember:()=>{},clear:()=>retained=undefined,detail:async()=>({status:'pending'}),progress:async()=>{if(mode==='offline')throw Error('offline');return {status:mode}},create:async()=>{created++;return {id:10}},sandbox:async()=>{},wait:async()=>{}};
 await assert.rejects(launchTemplate(ports),/已结束/);assert.equal(retained,undefined);assert.equal(created,0);
 retained=9;mode='offline';await assert.rejects(launchTemplate(ports),/offline/);assert.equal(retained,9);
});
