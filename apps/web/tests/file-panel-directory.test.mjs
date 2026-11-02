import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import test from 'node:test';
import ts from 'typescript';

const source=await readFile(new URL('../src/components/training/files/FilePanel.tsx',import.meta.url),'utf8');
const compiled=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022,jsx:ts.JsxEmit.ReactJSX}}).outputText;
const same=(a,b)=>a&&b&&a.length===b.length&&a.every((x,i)=>Object.is(x,b[i]));
const tick=()=>new Promise(resolve=>setImmediate(resolve));

function mount(directory='/opt/mall') {
  const slots=[],pending=[],requests=[];let cursor=0,props={sessionId:7,pod:'mall-0',container:'app',directory},tree;
  const react={
    useState(initial){const i=cursor++;if(!(i in slots))slots[i]=typeof initial==='function'?initial():initial;return [slots[i],value=>{slots[i]=typeof value==='function'?value(slots[i]):value;}];},
    useRef(initial){const i=cursor++;return slots[i]??=( {current:initial} );},
    useCallback(fn,deps){const i=cursor++;if(!same(slots[i]?.deps,deps))slots[i]={deps,fn};return slots[i].fn;},
    useEffect(fn,deps){const i=cursor++;if(!same(slots[i]?.deps,deps)){const old=slots[i];slots[i]={deps};pending.push(()=>{old?.cleanup?.();slots[i].cleanup=fn();});}},
  };
  const jsx=(type,props,key)=>({type,props,key});
  const files={listFiles(...args){return new Promise((resolve,reject)=>requests.push({args,resolve,reject}));}};
  const exports={};
  new Function('require','exports','window',compiled)(name=>{
    if(name==='react')return react;
    if(name==='react/jsx-runtime')return {jsx,jsxs:jsx};
    if(name.endsWith('/services/files'))return files;
    if(name==='./FileRow')return {FileRow:'FileRow'};
    if(name==='./FileEditor')return {FileEditor:'FileEditor'};
    throw new Error(name);
  },exports,{addEventListener(){},removeEventListener(){}});
  const render=next=>{props={...props,...next};cursor=0;tree=exports.FilePanel(props);pending.splice(0).forEach(fn=>fn());return tree;};
  const nodes=()=>{const result=[];const visit=node=>{if(Array.isArray(node)){node.forEach(visit);return;}if(node&&typeof node==='object'){result.push(node);visit(node.props?.children);}};visit(tree);return result;};
  render();
  return {requests,render,nodes,unmount(){slots.forEach(slot=>slot?.cleanup?.());}};
}

const entry=(name,path='/opt/mall/'+name)=>({name,path,is_dir:false,mode:'0644',permissions:'-rw-r--r--',owner:'root',group:'root',size:1,mod_time:0});

test('opening Files starts in the terminal directory and follows subsequent cd',async()=>{
  const view=mount();assert.equal(view.requests[0].args[2],'/opt/mall');
  view.requests[0].resolve([entry('app.jar')]);await tick();view.render({directory:'/tmp'});
  assert.equal(view.requests[1].args[2],'/tmp');
  view.requests[1].resolve([entry('current','/tmp/current')]);await tick();view.render();
  assert.deepEqual(view.nodes().filter(n=>n.type==='FileRow').map(n=>n.props.entry.path),['/tmp/current']);
});

test('late directory results and errors cannot replace a newer listing',async()=>{
  for(const outcome of ['success','failure']) {
    const view=mount();view.render({directory:'/tmp'});assert.equal(view.requests.length,2);
    view.requests[1].resolve([entry('current','/tmp/current')]);await tick();view.render();
    if(outcome==='success')view.requests[0].resolve([entry('stale')]);
    else view.requests[0].reject(new Error('old lookup failed'));
    await tick();view.render();
    assert.equal(view.requests.length,2);
    assert.deepEqual(view.nodes().filter(n=>n.type==='FileRow').map(n=>n.props.entry.path),['/tmp/current']);
    assert.ok(!view.nodes().some(n=>n.props?.children==='old lookup failed'));
  }
});

test('lookup failure stays visible at the requested cwd without silently listing root',async()=>{
  const view=mount('/restricted');view.requests[0].reject(new Error('permission denied'));await tick();view.render();
  assert.equal(view.requests.length,1);assert.ok(view.nodes().some(n=>n.props?.children==='permission denied'));
  assert.ok(view.nodes().some(n=>n.type==='button'&&n.props.children==='/restricted'));
});

test('following terminal cd preserves the open editor and its file identity',async()=>{
  const view=mount();view.requests[0].resolve([entry('README.md')]);await tick();view.render();
  const row=view.nodes().find(n=>n.type==='FileRow');row.props.onOpen(row.props.entry);view.render();
  assert.equal(view.nodes()[0].type,'FileEditor');assert.equal(view.nodes()[0].props.path,'/opt/mall/README.md');
  view.render({directory:'/tmp'});assert.equal(view.requests.length,2);
  assert.equal(view.nodes()[0].type,'FileEditor');assert.equal(view.nodes()[0].props.path,'/opt/mall/README.md');
});

test('project, deployment and configuration shortcuts browse their explicit paths',()=>{
  const view=mount('/workspace/mall');view.render({workingDir:'/workspace/mall',deploymentDir:'/app',configDir:'/app/config'});
  for(const [label,path] of [['项目目录','/workspace/mall'],['部署目录','/app'],['配置目录','/app/config']]) {
    const shortcut=view.nodes().find(node=>node.type==='button'&&node.props.children===label);
    assert.ok(shortcut,`missing ${label}`);shortcut.props.onClick();
    assert.equal(view.requests.at(-1).args[2],path);
  }
});
