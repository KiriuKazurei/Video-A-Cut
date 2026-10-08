import { lstat, rename } from 'node:fs/promises';
import { setTimeout as sleep } from 'node:timers/promises';

// Windows scanners can briefly retain a handle after the final file closes.
// Retry only sharing/permission failures, and never replace an existing target.
export async function publishNewDirectory(source, target, {renameImpl=rename, sleepImpl=sleep, platform=process.platform, signal}={}) {
  for(let attempt=0;attempt<6;attempt++){
    signal?.throwIfAborted();
    // lstat also rejects dangling links instead of following them as missing.
    try { await lstat(target);throw Object.assign(new Error('output directory already exists'),{code:'EEXIST'}); }
    catch(error){ if(error.code!=='ENOENT')throw error; }
    signal?.throwIfAborted();
    try { await renameImpl(source,target);return; }
    catch(error){
      signal?.throwIfAborted();
      if(platform!=='win32'||!['EPERM','EACCES','EBUSY'].includes(error.code)||attempt===5)throw error;
      try { await sleepImpl(100*(attempt+1), undefined, {signal}); }
      catch(waitError){ signal?.throwIfAborted();throw waitError; }
    }
  }
}
