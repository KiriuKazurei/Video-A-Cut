// Existing preview process ownership/pipe protocol, scoped by Go to one sequence.
import {createInterface} from 'node:readline';
import {createSequenceFrameSession} from './sequence-frame-session.mjs';
const input=createInterface({input:process.stdin,crlfDelay:Infinity});
let session,timer;
const idle=()=>{clearTimeout(timer);timer=setTimeout(()=>input.close(),60000);};
input.on('line',()=>session?.cancelPrefetch());idle();
try{
 for await(const line of input){
  clearTimeout(timer);let request;
  try{
   if(Buffer.byteLength(line)>8*1024*1024)throw Error('Preview request too large');
   request=JSON.parse(line);session??=await createSequenceFrameSession({output_dir:process.argv[2]});
   const result=request.operation==='segment'?await session.renderSegment(request):await session.render(request);
   process.stdout.write(JSON.stringify({...result,request_id:request.request_id,worker_pid:process.pid,ok:true})+'\n');
   if(request.operation!=='segment')void session.prefetch(request).catch(()=>{});
  }catch(error){process.stdout.write(JSON.stringify({ok:false,request_id:request?.request_id,message:error.message})+'\n');}
  finally{idle();}
 }
}finally{clearTimeout(timer);await session?.close();}
