import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import test from 'node:test';
import ts from 'typescript';
const source=await readFile(new URL('../src/services/growthReview.ts',import.meta.url),'utf8');
const out=ts.transpileModule(source.replace("import { http } from '@quenching/shared';", "const http = {};"),{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText;
const {formatAbility,compareAbility,resolvePracticeLink,SourceRequestGate}=await import('data:text/javascript;base64,'+Buffer.from(out).toString('base64'));
test('unknownDisplaysUnobservedInsteadOfZero',()=>assert.equal(formatAbility({level:'unknown',confidence:0}), '未观察'));
test('missingBaselineDoesNotInventImprovement',()=>assert.equal(compareAbility(undefined,{overall_level:'unknown',dimensions:{}}).hasBaseline,false));
test('practiceOnlyLinksKnownAvailableTargets',()=>{
 assert.equal(resolvePracticeLink({project_id:0,target_available:false}),undefined);
 assert.equal(resolvePracticeLink({project_id:123,target_available:false}),undefined);
 assert.equal(resolvePracticeLink({project_id:123,target_available:true}),'/projects/123');
});
test('staleSourceResponseCannotEnterNextReview',()=>{
 const gate=new SourceRequestGate();const old=gate.begin(1,'e:1');const latest=gate.begin(2,'e:1');
 assert.equal(old.signal.aborted,true);assert.equal(gate.isCurrent(old),false);assert.equal(gate.isCurrent(latest),true);gate.cancel();assert.equal(gate.isCurrent(latest),false);
});
test('legacySixChaptersRemainReadable',async()=>{
 const page=await readFile(new URL('../src/pages/ReviewPage.tsx',import.meta.url),'utf8');
 assert.match(page,/detail\.growth_report\s*&&/);assert.match(page,/sortedSections\.map/);
});
