import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import test from 'node:test';
import ts from 'typescript';

const source=await readFile(new URL('../src/hooks/useTerminalWS.ts',import.meta.url),'utf8');
const compiled=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText;

function mount() {
  let state;
  const timeouts=new Map(),intervals=new Map(),sockets=[];
  let nextTimer=1;
  const react={
    useState(initial){state=initial;return [initial,value=>{state=typeof value==='function'?value(state):value;}];},
    useRef(value){return {current:value};},
    useCallback(fn){return fn;},
    useEffect(){},
  };
  const later=callback=>{const id=nextTimer++;timeouts.set(id,callback);return id;};
  class Socket {
    static CONNECTING=0;static OPEN=1;static CLOSING=2;static CLOSED=3;
    readyState=Socket.CONNECTING;
    constructor(url){this.url=url;sockets.push(this);}
    send(){}
    close(){this.readyState=Socket.CLOSING;}
    hello(){this.readyState=Socket.OPEN;this.onopen?.({});this.message({type:'hello',data:{connection_id:'fixture'}});}
    message(data){this.onmessage?.({data:JSON.stringify(data)});}
    end(code=1006){this.readyState=Socket.CLOSED;this.onclose?.({code});}
  }
  const exports={};
  const window={setTimeout:later,setInterval:fn=>{const id=nextTimer++;intervals.set(id,fn);return id;}};
  new Function('require','exports','window','WebSocket','clearTimeout','clearInterval',compiled)(
    name=>{assert.equal(name,'react');return react;},exports,window,Socket,id=>timeouts.delete(id),id=>intervals.delete(id),
  );
  const hook=exports.useTerminalWS({url:'ws://fixture/exec',token:'fixture',autoConnect:false});
  return {hook,sockets,timeouts,intervals,get state(){return state;}};
}

test('terminal server failures remain visible and stop automatic reconnects',()=>{
  const view=mount();view.hook.connect();const socket=view.sockets[0];socket.hello();
  socket.message({type:'error',message:'exec is forbidden'});socket.end();
  assert.equal(view.state.status,'error');assert.equal(view.state.error,'exec is forbidden');
  assert.equal(view.timeouts.size,0);assert.equal(view.intervals.size,0);
  view.hook.connect();assert.equal(view.sockets.length,2);assert.equal(view.state.status,'connecting');assert.equal(view.state.error,undefined);
});

test('normal shell completion does not reopen another shell',()=>{
  const view=mount();view.hook.connect();const socket=view.sockets[0];socket.hello();
  socket.message({type:'close'});socket.end();
  assert.equal(view.state.status,'disconnected');assert.equal(view.timeouts.size,0);
});

test('late messages from an old connection cannot overwrite a manual retry',()=>{
  const view=mount();view.hook.connect();const previous=view.sockets[0];previous.hello();
  previous.message({type:'error',message:'first failure'});
  view.hook.connect();const current=view.sockets[1];assert.ok(current);current.hello();
  previous.end();previous.message({type:'error',message:'stale failure'});
  assert.equal(view.state.status,'connected');assert.equal(view.state.error,undefined);assert.equal(view.timeouts.size,0);
});

test('unexpected transport loss retains automatic retry',()=>{
  const view=mount();view.hook.connect();view.sockets[0].hello();view.sockets[0].end();
  assert.equal(view.state.status,'reconnecting');assert.equal(view.timeouts.size,1);
});

test('native socket errors cannot erase an already received server failure',()=>{
  const view=mount();view.hook.connect();const socket=view.sockets[0];socket.hello();
  socket.message({type:'error',message:'exec is forbidden'});socket.onerror({});socket.end();
  assert.equal(view.state.status,'error');assert.equal(view.state.error,'exec is forbidden');assert.equal(view.timeouts.size,0);
});

test('native socket errors after normal completion do not turn exit into failure',()=>{
  const view=mount();view.hook.connect();const socket=view.sockets[0];socket.hello();
  socket.message({type:'close'});socket.onerror({});socket.end();
  assert.equal(view.state.status,'disconnected');assert.equal(view.state.error,undefined);assert.equal(view.timeouts.size,0);
});
