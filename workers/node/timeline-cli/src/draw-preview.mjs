// Private newline-JSON pipe. Go owns the process and filesystem bindings.
import {createInterface} from 'node:readline';
import {createHash} from 'node:crypto';
import {openUnifiedDraw} from './unified-draw.mjs';
import {appendFile} from 'node:fs/promises';
import {join} from 'node:path';
const input=createInterface({input:process.stdin,crlfDelay:Infinity});
let host,signature,timeout,currentRequest;
const state=phase=>process.stdout.write(JSON.stringify({event:'state',phase,request_id:currentRequest?.request_id})+'\n');
const resetIdle=()=>{clearTimeout(timeout);timeout=setTimeout(()=>input.close(),60000);};
resetIdle();
try{
  for await(const line of input){
    clearTimeout(timeout);
    let request;
    try{
      if(Buffer.byteLength(line)>8*1024*1024)throw Error('Preview request too large');
      request=JSON.parse(line);
      currentRequest=request;
      const next=createHash('sha256').update(JSON.stringify(request.assets)).digest('hex');
      if(!host||signature!==next){await host?.close();host=await openUnifiedDraw({...request,on_state:state});signature=next;}
      else await host.update(request.document);
      const frame=await host.seek(request);
      process.stdout.write(JSON.stringify({...frame,png:frame.png.toString('base64'),worker_pid:process.pid,ok:true})+'\n');
    }catch(error){
      if(request?.output_dir)await appendFile(join(request.output_dir,'worker-errors.log'),String(error.stack??error)+'\n'+JSON.stringify(host?.diagnostics())+'\n').catch(()=>{});
      process.stdout.write(JSON.stringify({ok:false,request_id:request?.request_id,message:String(error.message??error)})+'\n');
    }finally{resetIdle();}
  }
}finally{clearTimeout(timeout);await host?.close();}
