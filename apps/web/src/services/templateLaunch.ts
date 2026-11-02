interface LaunchPorts {
 read():number|undefined; remember(id:number):void; clear():void;
 detail(id:number):Promise<{status:string}>;progress(id:number):Promise<{status:string}>;
 create():Promise<{id:number}>;sandbox(id:number):Promise<unknown>;wait(id:number):Promise<unknown>;
}
function notFound(error:unknown){return (error as {response?:{status?:number}})?.response?.status===404}
export async function launchTemplate(p:LaunchPorts):Promise<number>{
 let id=p.read();
 if(id){
  try{const detail=await p.detail(id);if(['completed','aborted','failed'].includes(detail.status)){p.clear();throw new Error('本次实训已结束，请重新开始')}}catch(e){if(notFound(e))p.clear();throw e}
  try{const progress=await p.progress(id);if(['released','releasing','failed'].includes(progress.status)){p.clear();throw new Error('本次环境已结束，请重新开始')}}catch(e){if(!notFound(e))throw e}
 }
 if(!id){id=(await p.create()).id;p.remember(id)}
 // Template creation is idempotent for the same session, including uncertain POST responses.
 await p.sandbox(id);await p.wait(id);p.clear();return id;
}
