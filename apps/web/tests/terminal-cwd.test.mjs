import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import test from 'node:test';
import ts from 'typescript';

const source=await readFile(new URL('../src/components/XtermTerminal.tsx',import.meta.url),'utf8');
const compiled=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022,jsx:ts.JsxEmit.ReactJSX}}).outputText;
const exports={};
new Function('require','exports',compiled)(()=>({}),exports);

test('terminal cwd decodes the absolute path from shell OSC 7 file URIs',()=>{
  assert.equal(typeof exports.parseTerminalCwd,'function');
  for(const [uri,path] of [
    ['file://training-pod/opt/mall','/opt/mall'],
    ['file:///','/'],
    ['file://training-pod/opt/my%20project/%E4%BB%A3%E7%A0%81%23%3F%25','/opt/my project/代码#?%'],
    ['file://training-pod/opt/%2520','/opt/%20'],
  ]) assert.equal(exports.parseTerminalCwd(uri),path);
});

test('malformed or non-file cwd notifications cannot navigate the file panel',()=>{
  assert.equal(typeof exports.parseTerminalCwd,'function');
  for(const uri of ['','/tmp','https://host/tmp','file://host/tmp%ZZ','file://host/tmp%00x','file://host/tmp%0Ax','file://host/tmp?query','file://host/tmp#fragment']) {
    assert.equal(exports.parseTerminalCwd(uri),undefined,uri);
  }
});
