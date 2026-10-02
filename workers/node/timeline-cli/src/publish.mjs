import { access, rename } from 'node:fs/promises';
import { setTimeout as sleep } from 'node:timers/promises';

// Windows scanners can briefly retain a handle after the final file closes.
// Retry only sharing/permission failures, and never replace an existing target.
export async function publishNewDirectory(source, target, {renameImpl=rename, sleepImpl=sleep, platform=process.platform}={}) {
  for(let attempt=0;attempt<6;attempt++){
    try { await access(target);throw Object.assign(new Error('output directory already exists'),{code:'EEXIST'}); }
    catch(error){ if(error.code!=='ENOENT')throw error; }
    try { await renameImpl(source,target);return; }
    catch(error){
      if(platform!=='win32'||!['EPERM','EACCES','EBUSY'].includes(error.code)||attempt===5)throw error;
      await sleepImpl(100*(attempt+1));
    }
  }
}
