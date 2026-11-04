// Audio is declared alongside source code using current-project resources.
// Browser engines only draw pixels; the shared timeline mixer owns sound.
export function codeAudioTracks(sequence,resources,tracks){
 const rate=sequence.fps.num/sequence.fps.den,out=[];
 for(const track of tracks){if(track.kind!=='code'||track.muted)continue;
  for(const clip of track.clips){const source=resources.find(r=>r.id===clip.resource_id),doc=source?.draw_document;if(!doc)continue;
   const fps=doc.canvas.fps.num/doc.canvas.fps.den,sourceIn=clip.source_in.value/clip.source_in.scale,sourceOut=clip.source_out.value/clip.source_out.scale;
   for(const [i,binding] of (doc.audio_bindings??[]).entries()){
    const start=Math.max(sourceIn,binding.at_frame/fps),end=Math.min(sourceOut,(binding.at_frame+binding.duration_frames)/fps);
    if(end<=start)continue;
    const audio=resources.find(r=>r.id===binding.resource_id);
    if(!audio||binding.resource_sha256&&audio.sha256!==binding.resource_sha256)throw Error('Code audio resource missing or changed');
    const offset=(start-sourceIn)/(sourceOut-sourceIn),span=(end-start)/(sourceOut-sourceIn),keys=clip.effects.gain_keyframes??[];
    const at=t=>{let a=keys[0];if(!a)return clip.effects.gain_db??0;for(const b of keys.slice(1)){if(t<=b.t)return a.db+(b.db-a.db)*Math.max(0,(t-a.t)/Math.max(1e-12,b.t-a.t));a=b;}return a.db;};
    const gain_keyframes=keys.length?[{t:0,db:at(offset)},...keys.filter(k=>k.t>offset&&k.t<offset+span).map(k=>({t:(k.t-offset)/span,db:k.db})),{t:1,db:at(offset+span)}]:undefined;
    const from=(binding.source_in??0)+start-binding.at_frame/fps;
    const c={...clip,id:clip.id+':audio:'+i,resource_id:audio.id,stream_index:binding.stream_index??0,at:clip.at+Math.round((start-sourceIn)/clip.speed*rate),duration:Math.max(1,Math.round((end-start)/clip.speed*rate)),source_in:{value:Math.round(from*1e9),scale:1e9},source_out:{value:Math.round((from+end-start)*1e9),scale:1e9},effects:{...clip.effects,gain_keyframes,gain_db:gain_keyframes?.[0].db??clip.effects.gain_db,transition:0,parameter_keyframes:undefined}};
    out.push({...track,id:track.id+':'+c.id,kind:'audio',hidden:false,gain_db:(track.gain_db??0)+(binding.gain_db??0),clips:[c]});
   }
  }
 }
 return out;
}
