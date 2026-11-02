'use strict';
async function businessJSON(response) {
 if (!response.ok()) throw new Error('business HTTP failure');
 const body=await response.json();if (body.code!==200) throw new Error('business operation failed');return body.data;
}
function redactResult(value) {
 if(Array.isArray(value))return value.map(redactResult);
 if(value && typeof value==='object')return Object.fromEntries(Object.entries(value).filter(([k])=>!/(password|token|authorization|secret|api_key)/i.test(k)).map(([k,v])=>[k,redactResult(v)]));
 return value;
}
function validateClaim(cmd,sid) {if(!cmd || cmd.session_id!==sid || !/^[A-Za-z0-9_.:-]{1,128}$/.test(cmd.command_id))throw new Error('foreign command');return cmd;}
async function main(chromium) {
 const fs=require('node:fs');const base=process.env.BEHAVIOR_COMMAND_URL;
 const sid=Number(process.env.SESSION_ID);const token=process.env.BEHAVIOR_RUNNER_TOKEN;
 const browser=await chromium.launch({headless:true,args:['--no-sandbox','--disable-dev-shm-usage']});
 const context=await browser.newContext();const statePath=process.env.BEHAVIOR_STATE_PATH;
 if(!statePath)throw new Error('durable runner state not configured');
 const state=loadState(statePath,sid);
 const call=async(path,body)=>{
  const response=await fetch(base+path,{method:'POST',headers:{'Content-Type':'application/json','X-Runner-Token':token},body:JSON.stringify(body||{})});
  if(!response.ok)throw new Error('command channel unavailable');const result=await response.json();if(result.code!==0)throw new Error('command channel rejected');return result.data;
 };
 process.on('SIGTERM',()=>void browser.close().then(()=>process.exit(0)));
 for(;;){
  let command;
  try {command=await call('/claim');}catch{await new Promise(r=>setTimeout(r,3000));continue;}
  if(!command){await new Promise(r=>setTimeout(r,1000));continue;}
  command=validateClaim(command,sid);
  let result;
  try {
   const flow=JSON.parse(fs.readFileSync(process.env.FLOW_CONFIG_PATH,'utf8'));
   if(!flow.protocol)throw new Error('capability_unavailable');
   const {executeProtocol}=require('./protocol_runner.js');
   const details=await executeProtocol(command,flow.protocol,context,state);
   saveState(statePath,sid,state);
   result={command_id:command.command_id,status:'succeeded',evidence_refs:[],result_json:JSON.stringify(redactResult(details))};
  }catch(error){
   const ambiguous=['register','order','pay'].includes(command.operation);
   result={command_id:command.command_id,status:ambiguous&&!error.confirmedFailure?'unknown':'failed',evidence_refs:[],result_json:JSON.stringify({reason:error.message==='capability_unavailable'?'capability_unavailable':'business execution failed; inspect sandbox evidence'})};
  }
  // A lost result response only retries this report, never the business operation.
  for(;;){try {await call('/results',result);break;}catch{await new Promise(r=>setTimeout(r,2000));}}
 }
}
function loadState(file,sid){
 const fs=require('node:fs');let raw;try{raw=fs.readFileSync(file,'utf8')}catch(e){if(e.code==='ENOENT')return {};throw e;}
 const record=JSON.parse(raw);if(record.session_id!==sid || !record.state || typeof record.state!=='object' || Array.isArray(record.state))throw new Error('invalid session state');return record.state;
}
function saveState(file,sid,state){
 const fs=require('node:fs');const tmp=file+'.tmp';const fd=fs.openSync(tmp,'w',0o600);try{fs.writeFileSync(fd,JSON.stringify({session_id:sid,state}));fs.fsyncSync(fd)}finally{fs.closeSync(fd)}fs.renameSync(tmp,file);
}
module.exports={businessJSON,redactResult,validateClaim,main,loadState,saveState};
