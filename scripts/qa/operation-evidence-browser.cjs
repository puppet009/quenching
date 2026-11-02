const {chromium}=require(process.env.OPERATION_QA_PLAYWRIGHT || 'playwright');
const fs=require('fs');const assert=require('assert');const path=require('path');
const artifactDir=process.env.OPERATION_QA_ARTIFACT_DIR || 'output/operation-evidence';fs.mkdirSync(artifactDir,{recursive:true});const artifact=name=>path.join(artifactDir,name);const baseURL=process.env.OPERATION_QA_BASE_URL || 'http://127.0.0.1:5175';
(async()=>{
const browser=await chromium.launch({headless:true,executablePath:process.env.OPERATION_QA_CHROMIUM});
const context=await browser.newContext({viewport:{width:1280,height:900}});await context.addInitScript(()=>localStorage.setItem('quenching_token','fixture-token'));
const page=await context.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
let startCount=0;let keys=[];let currentStatus='succeeded';let ops=[];let approvals=0;let historyEnabled=false;
const base=(id,actor='student',kind='command',status=currentStatus)=>({schema_version:1,event_id:id+':3',operation_id:id,session_id:7,sequence:3,actor,kind,status,tool:kind==='tool'?'read_file':undefined,conversation_id:107,reply_message_id:2,target:{pod:'mall-0',container:'app',cwd:'/'},command:kind==='command'?'printf hello':undefined,coverage:{execution:status==='unknown'?'unknown':'complete',command:'complete',output:'complete',reason_codes:status==='unknown'?['result_unconfirmed']:[]},started_at:new Date().toISOString(),exit_code:status==='unknown'?null:status==='failed'?7:0,text_refs:['optext:'+id+':stdout']});
await page.route('**/api/v1/**',async route=>{
const req=route.request(),u=new URL(req.url()),p=u.pathname;let data={};let status=200;
if(p.match(/sandboxes\/7\/commands$/)&&req.method()==='POST'){startCount++;const body=req.postDataJSON();keys.push(body.key);if(startCount===1){await route.abort('failed');return};const e=base('cmd-'+startCount);ops.unshift(e);data=e;}
else if(p.endsWith('/cancel')){currentStatus='unknown';data=base('cancelled');ops.unshift(data)}
else if(p.match(/commands\//)){data=base('running')}
else if(p.endsWith('/operations/source')){const off=+(u.searchParams.get('offset_bytes')||0);data={ref:u.searchParams.get('ref'),text:off?'SOURCE TAIL':'SOURCE HEAD',offset_bytes:off,next_offset_bytes:off?22:11,complete:!!off}}
else if(p.endsWith('/operations'))data=+(u.searchParams.get('before_sequence')||0)?{items:[base('old-tool','assistant','tool','succeeded')],next_before:0,has_more:false}:{items:ops,next_before:100,has_more:historyEnabled};
else if(p.endsWith('/approvals')){approvals++;ops=ops.map(e=>e.kind==='tool'?{...e,status:'cancelled',approval:'rejected'}:e);data={approved:false}}
else if(p.includes('/ai/sessions/'))data={session_id:7,conversation_id:107,session_status:'running',messages:[{id:1,role:'user',content:'检查订单'},{id:2,role:'assistant',content:'先检查记录'}],has_more:false};
else if(p.endsWith('/targets'))data=[{name:'mall-0',displayName:'电商服务',phase:'Running',ready:true,containers:[{name:'app'}]}];
else if(p.endsWith('/brain'))data={phase:'adaptive_training',cleanup_status:'pending',session_id:7,hint_level:1};
else if(p.includes('/training/sessions/'))data={id:7,status:'running',aiMode:'assistant',difficulty:'easy',projectId:0,projectName:'电商实训',createdAt:new Date().toISOString(),startTime:new Date().toISOString()};
else if(p.endsWith('/health/overview'))data={services:[],total_services:0};
else if(p.includes('/metrics/'))data={series:[]};
else if(p.includes('/alerts'))data={items:[],total:0};
else if(p.match(/reviews\/99$/))data={id:99,session_id:7,user_id:1,project_id:0,title:'班次复盘',summary:'模拟报告',overall_evaluation:'',review_level:'arch',generation_status:'completed',sections:[],growth_report:{schema_version:1,overview:{source_count:3,cleanup_status:'succeeded',audit_complete:false,audit_coverage:{complete:false},audit_gaps:['result_unconfirmed']},claims:[],ability:{overall_level:'unknown',dimensions:{}},architecture:[],practices:[]}};
else if(p.includes('/reviews/history'))data={list:[],total:0};
else if(p.includes('/change-analysis'))data={changes:[],total_count:0};
await route.fulfill({status,contentType:'application/json',body:JSON.stringify({code:0,data})});
});
try{
await page.goto(baseURL+'/training/0?session=7&sandbox=7');await page.getByTitle('命令执行',{exact:true}).click();
await page.getByLabel('服务器',{exact:true}).selectOption('mall-0');await page.getByLabel('命令',{exact:true}).fill('printf hello');await page.getByRole('button',{name:'执行命令',exact:true}).click();await page.getByRole('button',{name:'重试同一次提交'}).click();await page.getByText('执行成功（退出码 0）').first().waitFor();assert.equal(keys[0],keys[1]);
await page.getByRole('button',{name:'读取 stdout'}).first().click();await page.getByText('SOURCE HEAD',{exact:true}).waitFor();await page.getByRole('button',{name:'继续读取原文'}).click();await page.getByText('SOURCE HEADSOURCE TAIL',{exact:true}).waitFor();
await page.getByRole('button',{name:'准备新的执行'}).click();currentStatus='failed';await page.getByRole('button',{name:'执行命令',exact:true}).click();await page.getByText('执行失败（退出码 7）').first().waitFor();
await page.getByRole('button',{name:'准备新的执行'}).click();currentStatus='unknown';await page.getByRole('button',{name:'执行命令',exact:true}).click();await page.getByText(/结果未知/).first().waitFor();
await page.getByRole('button',{name:'准备新的执行'}).click();currentStatus='running';await page.getByRole('button',{name:'执行命令',exact:true}).click();await page.getByRole('button',{name:'请求取消',exact:true}).click();await page.getByText('已请求取消；远端是否已产生修改，以最终记录为准。').waitFor();
ops.unshift({...base('tool-1','assistant','tool','pending'),approval:'requested',approval_id:'approval-token',text_refs:[]});
const countBefore=startCount;await page.reload();await page.getByTitle('实训助手',{exact:true}).click();await page.getByText('助手工具记录',{exact:true}).waitFor();await page.getByRole('button',{name:'拒绝修改'}).click();assert.equal(approvals,1);assert.equal(startCount,countBefore);
historyEnabled=true;await page.reload();await page.getByTitle('实训助手',{exact:true}).click();await page.getByRole('button',{name:'读取更早操作'}).click();ops.unshift({...base('fresh-tool','assistant','tool','succeeded'),tool:'fresh_query'});await page.getByRole('button',{name:'恢复实时记录'}).click({timeout:10000});await page.getByText(/fresh_query/).waitFor();assert.equal(startCount,countBefore);
await page.screenshot({path:artifact('browser-desktop.png')});
await page.setViewportSize({width:390,height:844});await page.reload();await page.getByTitle('命令执行',{exact:true}).click();await page.getByLabel('命令',{exact:true}).waitFor();const box=await page.getByLabel('命令',{exact:true}).boundingBox();assert(box.x>=0&&box.x+box.width<=390);await page.screenshot({path:artifact('browser-mobile.png')});
await page.goto(baseURL+'/review/99');await page.getByText('操作结果未确认',{exact:true}).waitFor();await page.getByText(/沙箱清理：已完成/).waitFor();await page.screenshot({path:artifact('browser-review.png')});
assert.deepEqual(errors,[]);console.log('PASS TrainingPage managed retry/key, success/failure/unknown/cancel, source tail; ChatWindow refresh/rejection/history resume no execution; ReviewPage separate cleanup/gap; 390px; pageerrors=0');
}catch(e){fs.writeFileSync(artifact('browser-failure.txt'),await page.locator('body').innerText());throw e}finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
