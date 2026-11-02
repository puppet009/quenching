"use strict";
const {businessJSON,redactResult}=require('./command_runner.js');
const {randomBytes}=require('node:crypto');
function at(root,path){if(!path)return root;let v=root;for(const key of path.split('.')){if(['__proto__','constructor','prototype'].includes(key))throw new Error('unsafe binding');if(v==null || !Object.hasOwn(v,key))throw new Error('missing protocol binding');v=v[key];}return v;}
function resolve(value,state,response){
 if(Array.isArray(value))return value.map(v=>resolve(v,state,response));
 if(value && typeof value==='object')return Object.fromEntries(Object.entries(value).map(([k,v])=>[k,resolve(v,state,response)]));
 if(typeof value!=='string')return value;
 if(value.startsWith('$') && !value.startsWith('${')){const key=value.slice(1);return key==='response'?response:key.startsWith('response.')?at(response,key.slice(9)):at(state,key);}
 return value.replace(/\$\{([a-zA-Z_][a-zA-Z0-9_.]*)\}/g,(_,key)=>encodeURIComponent(String(at(state,key))));
}
async function executeProtocol(command,adapter,context,state={}){
 if(adapter.protocol_version!==1 || !adapter.capabilities.includes(command.operation))throw new Error('capability_unavailable');
 const steps=adapter.operations[command.operation];if(!Array.isArray(steps)||!steps.length)throw new Error('capability_unavailable');
 let asserted=false;const timeline=[];
 for(const step of steps){
  if(step.action==='generate_account'){
   const nonce=randomBytes(8).toString('hex');state.username=`q${command.session_id}_${nonce}`;state.password=randomBytes(24).toString('hex');state.telephone='138'+String(BigInt('0x'+randomBytes(4).toString('hex'))%100000000n).padStart(8,'0');
   continue;
  }
  if(step.action==='sandbox_pay') {
   const amount=String(state.order_amount);if(!/^\d+(\.\d{1,2})?$/.test(amount))throw new Error('invalid order amount');const [whole,fraction='']=amount.split('.');const minor=Number(BigInt(whole)*100n+BigInt(fraction.padEnd(2,'0')));if(!Number.isSafeInteger(minor)||minor<=0)throw new Error('invalid order amount');
   const response=await fetch(process.env.BEHAVIOR_COMMAND_URL+'/payments',{method:'POST',headers:{'Content-Type':'application/json','X-Runner-Token':process.env.BEHAVIOR_RUNNER_TOKEN},body:JSON.stringify({command_id:command.command_id,scenario:JSON.parse(command.parameters_json||'{}').profile==='steady'?'success':(JSON.parse(command.parameters_json||'{}').profile||'success'),order:{order_id:String(state.order_id),account_ref:String(state.account_id),amount_minor:minor,currency:'CNY'}})});
   const result=await response.json();if(!response.ok || result.code!==0 || result.data.status!=='succeeded'){const error=new Error('sandbox payment outcome unconfirmed');error.confirmedFailure=response.ok && result.code===0 && result.data.status==='failed';throw error;}
   timeline.push({source:'sandbox_payment',receipt_id:result.data.receipt_id,passed:true});continue;
  }
  if(step.action==='ui'){
   const page=await context.newPage();try{const response=await page.goto(new URL('/',adapter.entry_url).href,{waitUntil:'domcontentloaded',timeout:15000});if(!response || !response.ok())throw new Error('frontend unavailable');await page.locator('body').waitFor({state:'visible',timeout:15000});}finally{await page.close();}timeline.push({source:'browser',operation:'browse',passed:true});continue;
  }
  if(step.action!=='http')throw new Error('unsupported protocol action');
  const base=new URL(adapter.api_url);const path=resolve(step.path,state);
  if(!path.startsWith('/') || path.startsWith('//') || path.includes('\\'))throw new Error('external protocol target');
  const target=new URL(path,base);if(target.origin!==base.origin)throw new Error('external protocol target');
  const headers={};if(state.token)headers.Authorization=(state.token_head||'Bearer ')+state.token;
  const options={method:step.method,headers,timeout:15000,maxRetries:0,failOnStatusCode:false,maxRedirects:0};
  if(step.query)options.params=resolve(step.query,state);
  if(step.form)options.form=resolve(step.form,state);
  if(step.body!==undefined)options.data=resolve(step.body,state);
  const response=await businessJSON(await context.request.fetch(target.href,options));
  for(const [binding,rule] of Object.entries(step.assert||{})){
   const actual=resolve(binding,state,response);
   if(Object.hasOwn(rule,'equals') && actual!==resolve(rule.equals,state,response))throw new Error('business assertion failed');
   if(Object.hasOwn(rule,'gt') && !(Number(actual)>rule.gt))throw new Error('business assertion failed');
   if(rule.exists && (actual===undefined || actual===null || actual===''))throw new Error('business evidence missing');
   asserted=true;
  }
  for(const [key,binding]of Object.entries(step.save||{})){if(['__proto__','constructor','prototype'].includes(key))throw new Error('unsafe binding');state[key]=resolve(binding,state,response);}
  timeline.push({source:'business_api',path:step.path,passed:true});
 }
 return redactResult({operation:command.operation,account_id:state.account_id,order_id:state.order_id,business_verified:asserted,timeline});
}
module.exports={executeProtocol,resolve};
