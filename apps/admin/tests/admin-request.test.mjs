import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {createServer} from 'node:http';
import {createRequire} from 'node:module';
import {pathToFileURL} from 'node:url';
import test from 'node:test';
import ts from 'typescript';

const require=createRequire(import.meta.url);
const moduleURL=source=>'data:text/javascript;base64,'+Buffer.from(ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText).toString('base64');
globalThis.localStorage={getItem:()=> 'test-jwt',removeItem:()=>{}};
const tokenURL=moduleURL(await readFile(new URL('../../../packages/shared/src/utils/token.ts',import.meta.url),'utf8'));
const timeURL=moduleURL(await readFile(new URL('../../../packages/shared/src/utils/time.ts',import.meta.url),'utf8'));
const telemetryURL=moduleURL((await readFile(new URL('../../../packages/shared/src/services/telemetry.ts',import.meta.url),'utf8')).replace('"../utils/time"',JSON.stringify(timeURL)));
const httpSource=(await readFile(new URL('../../../packages/shared/src/services/http.ts',import.meta.url),'utf8')).replace('"axios"',JSON.stringify(pathToFileURL(require.resolve('axios')).href)).replace('"../utils/token"',JSON.stringify(tokenURL)).replace('"./telemetry"',JSON.stringify(telemetryURL));
const httpURL=moduleURL(httpSource);
const projectURL=moduleURL((await readFile(new URL('../../../packages/shared/src/services/project.ts',import.meta.url),'utf8')).replace('"./http"',JSON.stringify(httpURL)));
const sharedURL=moduleURL(`export {http} from ${JSON.stringify(httpURL)}; export {listProjects} from ${JSON.stringify(projectURL)};`);
const api=await import(moduleURL((await readFile(new URL('../src/services/data.ts',import.meta.url),'utf8')).replace('"@quenching/shared"',JSON.stringify(sharedURL))));
const feedback=await import(moduleURL((await readFile(new URL('../../../packages/shared/src/services/feedback.ts',import.meta.url),'utf8')).replace('"./http"',JSON.stringify(httpURL))));
const {http}=await import(httpURL);

test('admin requests preserve real routes, JWT, filters and session IDs',async()=>{
 const received=[];
 const server=createServer(async(req,res)=>{let body='';for await(const chunk of req)body+=chunk;received.push({url:req.url,method:req.method,auth:req.headers.authorization,body});res.setHeader('Content-Type','application/json');res.end(JSON.stringify({code:0,data:{ok:true}}));});
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 const base=`http://127.0.0.1:${server.address().port}`;
 const interceptor=http.interceptors.request.use(cfg=>{cfg.baseURL=base+(cfg.baseURL||'');return cfg;});
 try{
  assert.deepEqual(await api.defects({template_slug:'mall',page:2}),{ok:true});
  await api.setDefectActive(8,false);await api.releaseSandbox(42);
  await feedback.listNpsFeedback({page:3,score_min:9,has_comment:true});
  await api.acceptBinding(8,'mall',{observed:'server trace'});
  await api.dashboard();await api.templates();
  assert.equal(received[0].url,'/api/admin/v1/defects?template_slug=mall&page=2');
  assert.equal(received[1].method,'PATCH');assert.deepEqual(JSON.parse(received[1].body),{is_active:false});
  assert.match(received[2].url,/^\/api\/admin\/v1\/sandboxes\/42\?/);assert.equal(received[2].method,'DELETE');
  assert.equal(received[3].url,'/api/v1/admin/feedback/nps?page=3&score_min=9&has_comment=true');
  assert.equal(received[4].url,'/api/admin/v1/defects/8/bindings/mall/accept');
  assert.equal(received[5].url,'/api/v1/admin/dashboard');assert.equal(received[6].url,'/api/v1/templates');
  assert.ok(received.every(r=>r.auth==='Bearer test-jwt'));
  assert.equal(api.errorMessage({response:{data:{msg:'尚未通过验收'}}}),'尚未通过验收');
 }finally{http.interceptors.request.eject(interceptor);server.closeAllConnections();await new Promise(resolve=>server.close(resolve));}
});

test('fault catalog preserves the available source when the other source fails',async()=>{
 for(const failedSource of ['projects','templates']){
  const template={slug:'mall',name:'mall 电商（接入验证）',services:[]};
  const project={id:41,name:'登记项目'};
  const server=createServer((req,res)=>{
   const source=req.url.startsWith('/api/v1/templates')?'templates':'projects';
   res.setHeader('Content-Type','application/json');
   res.end(JSON.stringify(source===failedSource?{code:20003,msg:'服务器内部错误'}:{code:0,data:source==='templates'?[template]:{list:[project],total:1}}));
  });
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const base=`http://127.0.0.1:${server.address().port}`;
  const interceptor=http.interceptors.request.use(cfg=>{cfg.baseURL=base+(cfg.baseURL||'');return cfg;});
  try{
   const result=await api.loadDefectCatalog();
   assert.deepEqual(result.templates,failedSource==='templates'?[]:[template]);
   assert.deepEqual(result.projects,failedSource==='projects'?[]:[project]);
   assert.equal(result.errors.length,1);
   assert.match(result.errors[0],/服务器内部错误/);
  }finally{http.interceptors.request.eject(interceptor);server.closeAllConnections();await new Promise(resolve=>server.close(resolve));}
 }
});
