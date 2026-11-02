const test=require('node:test');const assert=require('node:assert/strict');
const {businessJSON, redactResult, validateClaim}=require('../internal/service/behavior_assets/command_runner.js');
test('http_200_business_failure',async()=>{await assert.rejects(()=>businessJSON({ok:()=>true,json:async()=>({code:500,message:'failed'})}));});
test('claim_one_command',()=>{assert.throws(()=>validateClaim({session_id:2,command_id:'c1'},1));assert.equal(validateClaim({session_id:1,command_id:'c1'},1).command_id,'c1');});
test('redact_account_secret',()=>{const value=redactResult({token:'secret-a',account:{password:'secret-b'},order_id:1});assert.equal(JSON.stringify(value).includes('secret'),false);assert.equal(value.order_id,1);});

test('sandbox account state survives runner restart and rejects foreign session',()=>{
 const fs=require('node:fs'),os=require('node:os'),path=require('node:path');const {saveState,loadState}=require('../internal/service/behavior_assets/command_runner.js');
 const dir=fs.mkdtempSync(path.join(os.tmpdir(),'quenching-state-'));const file=path.join(dir,'state.json');
 try{saveState(file,7,{username:'account',token:'secret'});assert.deepEqual(loadState(file,7),{username:'account',token:'secret'});assert.throws(()=>loadState(file,8));}finally{if(fs.existsSync(file))fs.unlinkSync(file);fs.rmdirSync(dir);}
});
