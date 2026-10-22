import { Fragment, useEffect, useMemo, useRef, useState, type PointerEvent, type RefObject } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Button, Dropdown, Flex, Select, Slider, Space, Tooltip, type MenuProps } from 'antd';
import { LockOutlined, UnlockOutlined, SoundOutlined, EyeInvisibleOutlined, ScissorOutlined, UndoOutlined, RedoOutlined, PlusOutlined, PushpinOutlined, MoreOutlined, SelectOutlined, ColumnWidthOutlined, VerticalAlignMiddleOutlined, CompressOutlined, ExpandOutlined, AimOutlined } from '@ant-design/icons';
import { useSequenceEditor } from '../sequenceEditor';
import { useProjectWorkspace } from '../projectWorkspace';
import { timelineApi } from '../timelineApi';
import { buildLevels, columns, drawColumns, loudnessRefetchInterval, loudnessRetry, loudnessRetryDelay, visibleSpan, sourceWindow, loudnessDisplayGain } from '../loudness';
import { ClipGainBand } from './ClipGainBand';
import { fpsValue, frameLabel, linkedSelection, snapFrame, timeSeconds, type TimelineClip, type TimelineTrack } from '../timelineContracts';

function ClipLoudness({clip,track,px,scroller}:{clip:TimelineClip;track:TimelineTrack;px:number;scroller:RefObject<HTMLDivElement|null>}) {
 const e=useSequenceEditor(),p=useProjectWorkspace(),canvas=useRef<HTMLCanvasElement>(null);
 const query=useQuery({queryKey:['resource-loudness',p.session?.id,e.projectId,clip.resource_id,clip.stream_index],queryFn:({signal})=>timelineApi.loudness(e.projectId,clip.resource_id,clip.stream_index,signal),staleTime:Infinity,retry:loudnessRetry,retryDelay:loudnessRetryDelay,refetchInterval:q=>loudnessRefetchInterval(q.state.status)});
 // The pyramid is built once per asset; zoom/scroll only re-read it.
 const data=useMemo(()=>query.data?buildLevels(query.data):null,[query.data]);
 const left=clip.at*px,width=Math.max(3,clip.duration*px),from=timeSeconds(clip.source_in),to=timeSeconds(clip.source_out),trackGain=track.gain_db||0;
 const [span,setSpan]=useState<[number,number]>([0,0]);
 useEffect(()=>{
  const node=scroller.current;if(!node)return;let frame=0;
  // Only the visible slice (+overscan) gets a canvas, so hour-long clips at
  // high zoom never allocate a canvas wider than the viewport.
  const update=()=>{frame=0;const [x0,x1]=visibleSpan(node.scrollLeft,node.clientWidth,left,width);setSpan(prev=>prev[0]===x0&&prev[1]===x1?prev:[x0,x1]);};
  const schedule=()=>{if(!frame)frame=requestAnimationFrame(update);};
  update();node.addEventListener('scroll',schedule,{passive:true});const observer=new ResizeObserver(schedule);observer.observe(node);
  return()=>{node.removeEventListener('scroll',schedule);observer.disconnect();if(frame)cancelAnimationFrame(frame);};
 },[scroller,left,width]);
 useEffect(()=>{
  const node=canvas.current;if(!node||!data)return;const w=span[1]-span[0];if(w<=0)return;
  const dpr=Math.min(2,window.devicePixelRatio||1),h=Math.max(1,node.clientHeight),pw=Math.round(w*dpr),ph=Math.round(h*dpr);
  if(node.width!==pw)node.width=pw;if(node.height!==ph)node.height=ph;
  const ctx=node.getContext('2d');if(!ctx)return;
  const [a,b]=sourceWindow(from,to,width,span);
  // Spectrum uses constant clip+track gain only — never the keyframe rubber-band —
  // so the blue line stays a manual envelope editor, independent of loudness shape.
  const gain=loudnessDisplayGain(clip.effects,trackGain);
  drawColumns(ctx,columns(data,a,b,pw,gain),ph,data.floorDb);
 },[data,span,from,to,width,trackGain,clip.effects.gain_db]);
 const clipped=data&&query.data?.clipped_buckets?'含接近 0 dBFS 的峰值':'';
 if(query.isError)return <div className="clip-loudness is-empty" title="音量谱暂不可用" aria-label="音量谱暂不可用"/>;
 if(!data)return <div className="clip-loudness is-loading" aria-label="正在计算音量谱"/>;
 return <canvas ref={canvas} className="clip-loudness" aria-label={`音量谱${clipped?` · ${clipped}`:''}`} style={{left:span[0],width:span[1]-span[0]}}/>;
}
/** Premiere-style: one source-in poster at the clip start, aspect ratio preserved. */
function ClipThumbnails({clip}:{clip:TimelineClip}) {
 const e=useSequenceEditor(),from=timeSeconds(clip.source_in);
 return <div className="clip-thumbnails" aria-hidden="true"><img draggable={false} loading="lazy" alt="" src={timelineApi.thumbnailURL(e.projectId,clip.resource_id,from)}/></div>;
}
type Drag={id:string;ids:string[];kind:'move'|'left'|'right';x:number;y:number;scroll:number;at:number;end:number;delta:number;target:string;started:boolean;snapped:boolean};
function effectLabel(clip:TimelineClip) {
 const fx=clip.effects;
 return [clip.speed!==1?`${clip.speed}×`:'',fx.gain_keyframes?.length?`音量 ${fx.gain_keyframes.length}点`:(fx.gain_db?`${fx.gain_db}dB`:''),fx.scale!==1||fx.x||fx.y?'变换':'',fx.crop_left||fx.crop_right||fx.crop_top||fx.crop_bottom?'裁切':'',fx.fade_in||fx.fade_out?'淡变':'',fx.transition?'溶解':'',clip.text?'字幕':''].filter(Boolean).join(' · ');
}
export function SequenceTimeline() {
 const e=useSequenceEditor(),p=useProjectWorkspace(),s=e.view?.sequence;
 const scroller=useRef<HTMLDivElement>(null),drag=useRef<Drag|null>(null),boxStart=useRef<{x:number;y:number}|null>(null);
 const [ghost,setGhost]=useState<Drag|null>(null),[box,setBox]=useState<{x:number;y:number;w:number;h:number}|null>(null),[drop,setDrop]=useState<{track:string;at:number;duration:number;insert:boolean}|null>(null),[insert,setInsert]=useState(false),[notice,setNotice]=useState('');
 const cancel=()=>{drag.current=null;setGhost(null);boxStart.current=null;setBox(null);setDrop(null);};
 useEffect(()=>{const key=(event:KeyboardEvent)=>{if(event.key==='Escape'&&(drag.current||boxStart.current)){event.preventDefault();cancel();setNotice('已取消拖动，时间线未修改');}};window.addEventListener('keydown',key,true);return()=>window.removeEventListener('keydown',key,true);},[]);
 useEffect(()=>cancel(),[e.sequenceId]);
 useEffect(()=>{
  const node=scroller.current,video=s?.tracks.find(t=>t.kind==='video');if(!node||!video||!e.active)return;
  const center=()=>{const row=node.querySelector<HTMLElement>(`[data-track-id="${video.id}"]`);if(row)node.scrollTop=Math.max(0,row.offsetTop+row.offsetHeight-(node.clientHeight+52)/2);};
  const observer=new ResizeObserver(center);observer.observe(node);return()=>observer.disconnect();
 },[s?.id,e.active]);
 if(!s)return null;
 const rate=fpsValue(s.fps),px=e.zoom/rate,length=Math.max(rate*10,...s.tracks.flatMap(t=>t.clips.map(c=>c.at+c.duration)))+rate*5,width=Math.max(640,length*px);
 const fitZoom=(spanFrames:number)=>{
  const lane=Math.max(160,(scroller.current?.clientWidth??640)-148);
  const seconds=Math.max(0.5,spanFrames/rate);
  e.setZoom(Math.min(300,Math.max(10,Math.round(lane/seconds))));
 };
 const fitAll=()=>fitZoom(length);
 const fitSelection=()=>{
  const clips=s.tracks.flatMap(t=>t.clips).filter(c=>selected.has(c.id));
  if(!clips.length){fitAll();return;}
  const start=Math.min(...clips.map(c=>c.at)),end=Math.max(...clips.map(c=>c.at+c.duration));
  fitZoom(Math.max(1,end-start));
 };

 const selected=new Set(e.selected),candidates=[e.head,...s.markers.map(m=>m.frame),...s.tracks.flatMap(t=>t.clips.filter(c=>!selected.has(c.id)).flatMap(c=>[c.at,c.at+c.duration]))];
 const displayTracks=[...s.tracks.filter(t=>t.kind!=='audio').reverse(),...s.tracks.filter(t=>t.kind==='audio')];
 const snap=(frame:number,disabled=false)=>snapFrame(frame,candidates,px,e.snap&&!disabled);
 const command=(...args:Parameters<typeof e.command>)=>void e.command(...args).catch(()=>{});
 const toggle=(t:TimelineTrack,prop:'locked'|'muted'|'hidden'|'solo')=>command([{type:'track.update',track_id:t.id,[prop]:!t[prop]}],'修改轨道状态');
 const clipMenu=(clip:TimelineClip,track:TimelineTrack):MenuProps['items']=>[
  {key:'split',label:'在播放头分割  Ctrl+K',disabled:track.locked,onClick:()=>void e.split().catch(()=>{})},
  {key:'copy',label:'复制  Ctrl+C',onClick:()=>e.copy()},{key:'cut',label:'剪切  Ctrl+X',disabled:track.locked,onClick:()=>e.copy(true)},
  {key:'delete',label:'删除  Delete',disabled:track.locked,onClick:()=>void e.remove().catch(()=>{})},{key:'ripple',label:'波纹删除  Shift+Delete',disabled:track.locked,onClick:()=>void e.remove(true).catch(()=>{})},
  {type:'divider'},{key:'unlink',label:clip.link_id?'分离音画':'链接所选音画',disabled:track.locked,onClick:()=>command([{type:clip.link_id?'clip.unlink':'clip.link',clip_ids:e.selected}],'音画链接')},
  {key:'move',label:'移动到播放头',disabled:track.locked,onClick:()=>command([{type:'clip.move',clip_ids:e.selected,at:e.head}],'移动片段')},
  {key:'marker',label:'为源画面添加标记',onClick:()=>e.marker('point',true)}
 ];
 const begin=(event:PointerEvent<HTMLElement>,clip:TimelineClip,track:TimelineTrack,kind:Drag['kind'])=>{
  if(event.button!==0)return;event.stopPropagation();event.currentTarget.focus();if(track.locked||e.pending){e.select([clip.id],event.altKey);e.setSelectedTrack(track.id);return;}
  if(e.tool==='razor'&&kind==='move'){const lane=event.currentTarget.closest('[data-lane]')!.getBoundingClientRect();command([{type:'clip.split',clip_ids:[clip.id],at:Math.round((event.clientX-lane.left)/px),unlinked:event.altKey}],'剃刀分割');return;}
  if(event.ctrlKey&&selected.has(clip.id)){e.select(e.selected.filter(id=>id!==clip.id),true);return;}
  const ids=selected.has(clip.id)?e.selected:linkedSelection(s,event.ctrlKey?[...e.selected,clip.id]:[clip.id],event.altKey);
  e.select(ids,event.altKey);e.setSelectedTrack(track.id);setNotice('');
  drag.current={id:clip.id,ids,kind,x:event.clientX,y:event.clientY,scroll:scroller.current?.scrollLeft??0,at:clip.at,end:clip.at+clip.duration,delta:0,target:track.id,started:false,snapped:false};event.currentTarget.setPointerCapture(event.pointerId);
 };
 const move=(event:PointerEvent<HTMLElement>)=>{const d=drag.current;if(!d)return;if(!d.started&&Math.hypot(event.clientX-d.x,event.clientY-d.y)<4)return;d.started=true;const bounds=scroller.current!.getBoundingClientRect();if(event.clientX>bounds.right-28)scroller.current!.scrollLeft+=16;else if(event.clientX<bounds.left+176)scroller.current!.scrollLeft-=16;const base=d.kind==='right'?d.end:d.at,raw=base+(event.clientX-d.x+scroller.current!.scrollLeft-d.scroll)/px,point=snap(raw,event.altKey);d.delta=point-base;d.snapped=point!==Math.round(raw);const track=document.elementFromPoint(event.clientX,event.clientY)?.closest<HTMLElement>('[data-track-id]');if(track)d.target=track.dataset.trackId!;setGhost({...d});};
 const finish=()=>{const d=drag.current;drag.current=null;setGhost(null);if(!d||!d.started)return;const ids=d.ids;
  if(d.kind==='move'){if(d.delta||d.target!==s.tracks.find(t=>t.clips.some(c=>c.id===d.id))?.id)command([{type:'clip.move',clip_ids:ids,at:d.at+d.delta,target_track_id:d.target,mode:insert?'insert':'overwrite'}],'拖动片段');}
  else command([{type:'clip.trim',clip_ids:ids,at:d.kind==='left'?d.at+d.delta:d.at,end:d.kind==='right'?d.end+d.delta:d.end}],'修剪片段');
 };
 const addTrackItems:MenuProps['items']=['video','audio','subtitle'].map(kind=>({key:kind,label:kind==='video'?'添加视频轨':kind==='audio'?'添加音频轨':'添加字幕轨',onClick:()=>command([{type:'track.add',kind}],'添加轨道')}));
 const clipCount=(track:TimelineTrack,at:number,duration:number)=>track.clips.filter(c=>c.at<at+duration&&c.at+c.duration>at).length;
 const sourceStreams=Array.isArray(p.resource?.metadata.streams)?p.resource.metadata.streams as {codec_type:string}[]:[],audioCount=sourceStreams.filter(stream=>stream.codec_type==='audio').length;
 const affectedDrop=(track:TimelineTrack)=>{if(!drop)return false;if(drop.track===track.id)return true;const target=s.tracks.find(t=>t.id===drop.track);if(!target||!p.resource||!['audio','video'].includes(p.resource.kind)||track.kind!=='audio'||target.group_id!==track.group_id)return false;const audio=s.tracks.filter(t=>t.kind==='audio'&&t.group_id===target.group_id),start=target.kind==='audio'?audio.findIndex(t=>t.id===target.id):0;return audio.slice(start,start+audioCount).some(t=>t.id===track.id);};
 return <section className="sequence-timeline" data-nav-anchor="timeline" aria-label="多轨时间线" tabIndex={0}>
  <Flex className="timeline-toolbar" align="center" gap={4} wrap>
   <Space.Compact>
    <Tooltip title="选择工具 V"><Button size="small" aria-label="选择工具 V" aria-pressed={e.tool==='select'} type={e.tool==='select'?'primary':'default'} icon={<SelectOutlined/>} onClick={()=>e.setTool('select')}/></Tooltip>
    <Tooltip title="剃刀工具 C"><Button size="small" aria-label="剃刀工具 C" aria-pressed={e.tool==='razor'} type={e.tool==='razor'?'primary':'default'} icon={<ScissorOutlined/>} onClick={()=>e.setTool('razor')}/></Tooltip>
   </Space.Compact>
   <Tooltip title="撤销 Ctrl+Z"><Button size="small" aria-label="撤销时间线编辑" icon={<UndoOutlined/>} disabled={e.pending||!e.view?.history.some(h=>h.applied)} onClick={()=>command([{type:'history.undo'}],'撤销')}/></Tooltip>
   <Tooltip title="重做 Ctrl+Shift+Z"><Button size="small" aria-label="重做时间线编辑" icon={<RedoOutlined/>} disabled={e.pending||!e.view?.history.some(h=>!h.applied)} onClick={()=>command([{type:'history.redo'}],'重做')}/></Tooltip>
   <Tooltip title="在播放头分割 Ctrl+K"><Button size="small" aria-label="分割" icon={<ColumnWidthOutlined/>} onClick={()=>void e.split().catch(()=>{})} disabled={e.pending}/></Tooltip>
   <Tooltip title="添加标记 M"><Button size="small" aria-label="标记 M" icon={<PushpinOutlined/>} onClick={()=>e.marker()}/></Tooltip>
   <Tooltip title="吸附（Alt 临时关闭）"><Button size="small" aria-label="吸附" aria-pressed={e.snap} type={e.snap?'primary':'default'} icon={<AimOutlined/>} onClick={()=>e.setSnap(!e.snap)}/></Tooltip>
   <Tooltip title="插入模式"><Button size="small" aria-label="插入模式" aria-pressed={insert} type={insert?'primary':'default'} icon={<VerticalAlignMiddleOutlined/>} onClick={()=>setInsert(!insert)}/></Tooltip>
   <Dropdown menu={{items:[...(addTrackItems??[]),{type:'divider' as const},{key:'scope',label:'对照范围',type:'group' as const,children:[{key:'scope-all',label:'全部轨道',onClick:()=>command([{type:'scope.set',group_id:''}],'切换对照范围')},{key:'scope-original',label:'原剪辑',onClick:()=>command([{type:'scope.set',group_id:'original'}],'切换对照范围')},...s.groups.map(g=>({key:`scope-${g.id}`,label:g.name,onClick:()=>command([{type:'scope.set',group_id:g.id}],'切换对照范围')}))]}]}}><Button size="small" icon={<MoreOutlined/>} aria-label="更多">更多</Button></Dropdown>
   <span className="timeline-zoom">
    <Tooltip title="适配全部"><Button size="small" aria-label="适配全部" icon={<CompressOutlined/>} onClick={fitAll}/></Tooltip>
    <Tooltip title="适配选中"><Button size="small" aria-label="适配选中" icon={<ExpandOutlined/>} onClick={fitSelection} disabled={!selected.size}/></Tooltip>
    <span>缩放</span><Slider min={10} max={300} value={e.zoom} onChange={e.setZoom} aria-label="时间线缩放"/>
   </span>
  </Flex>
  <div className="timeline-scroll" ref={scroller} onPointerCancel={cancel}>
   <div className="timeline-grid" style={{width:width+148}}>
    <div className="timeline-ruler"><div className="track-heading">{frameLabel(e.head,s.fps)}</div><div className="timeline-scale" style={{width}} onPointerDown={event=>{if(event.button===0){event.currentTarget.closest<HTMLElement>('.sequence-timeline')?.focus({preventScroll:true});e.setHead(snap((event.clientX-event.currentTarget.getBoundingClientRect().left)/px,event.altKey));}}}>
     {Array.from({length:Math.ceil(length/rate/Math.max(1,Math.ceil(104/e.zoom)))}).map((_,i)=>{const second=i*Math.max(1,Math.ceil(104/e.zoom));return <span className="ruler-label" key={i} style={{left:second*e.zoom}}>{frameLabel(Math.round(second*rate),s.fps)}</span>;})}
     {s.markers.map(m=><Tooltip key={m.id} title={`${m.notes}${m.locked?' · 固定时码':''}`}><button className={`timeline-marker marker-${m.kind}`} aria-label={`标记 ${m.notes}`} style={{left:m.frame*px}} onPointerDown={event=>event.stopPropagation()} onClick={()=>e.setHead(m.frame)}>{m.kind==='point'?'◆':m.kind==='in'?'［':'］'}<span>{m.notes}</span></button></Tooltip>)}
    </div></div>
    {displayTracks.map((track,displayIndex)=>{const trackIndex=s.tracks.indexOf(track);const trackItems:MenuProps['items']=[{key:'up',label:'上移轨道',disabled:!displayIndex||(track.kind==='audio')!==(displayTracks[displayIndex-1]?.kind==='audio'),onClick:()=>command([{type:'track.update',track_id:track.id,order:s.tracks.indexOf(displayTracks[displayIndex-1])}],'排列轨道')},{key:'down',label:'下移轨道',disabled:displayIndex===displayTracks.length-1||(track.kind==='audio')!==(displayTracks[displayIndex+1]?.kind==='audio'),onClick:()=>command([{type:'track.update',track_id:track.id,order:s.tracks.indexOf(displayTracks[displayIndex+1])}],'排列轨道')},{key:'height',label:track.height<96?'增高轨道':'恢复轨高',onClick:()=>command([{type:'track.update',track_id:track.id,height:track.height<96?112:64}],'轨道高度')},{key:'remove',label:'移除此轨道',disabled:track.locked,onClick:()=>command([{type:'track.remove',track_id:track.id}],'移除轨道')}];const audioStart=track.kind==='audio'&&displayIndex>0&&displayTracks[displayIndex-1].kind!=='audio';return <Fragment key={track.id}>{audioStart&&<div className="timeline-track-divider" role="separator" aria-label="视频轨与音频轨分界"><div className="track-heading"/></div>}<div className={`timeline-row${track.locked?' is-locked':''}${e.selectedTrack===track.id?' is-target':''}${audioStart?' is-audio-start':''}`} data-track-id={track.id} style={{height:track.height}}>
     <Dropdown trigger={['contextMenu']} menu={{items:trackItems}}>
      <div className="track-heading">
       <button className="track-name" onClick={()=>e.setSelectedTrack(track.id)} aria-pressed={e.selectedTrack===track.id} title={track.name}>{track.kind==='video'?'V':track.kind==='audio'?'A':'S'}{s.tracks.slice(0,trackIndex+1).filter(t=>t.kind===track.kind).length} · {track.name}{track.group_id?' · Agent':''}</button>
       <div className="track-head-controls" role="group" aria-label={`${track.name} 轨道控制`}>
        <Tooltip title={track.locked?'解锁':'锁定'}><Button size="small" type="text" className={track.locked?'is-on':''} aria-pressed={track.locked} aria-label={`${track.locked?'解锁':'锁定'} ${track.name}`} icon={track.locked?<LockOutlined/>:<UnlockOutlined/>} onClick={event=>{event.stopPropagation();toggle(track,'locked');}}/></Tooltip>
        <Tooltip title="独奏"><Button size="small" type="text" className={track.solo?'is-on':''} aria-pressed={track.solo} aria-label={`独奏 ${track.name}`} onClick={event=>{event.stopPropagation();toggle(track,'solo');}}>S</Button></Tooltip>
        <Tooltip title={track.kind==='audio'?(track.muted?'取消静音':'静音'):(track.hidden?'显示':'隐藏')}><Button size="small" type="text" className={(track.kind==='audio'?track.muted:track.hidden)?'is-on':''} aria-pressed={track.kind==='audio'?track.muted:track.hidden} aria-label={`${track.kind==='audio'?'静音':'隐藏'} ${track.name}`} icon={track.kind==='audio'?<SoundOutlined/>:<EyeInvisibleOutlined/>} onClick={event=>{event.stopPropagation();toggle(track,track.kind==='audio'?'muted':'hidden');}}/></Tooltip>
        <Dropdown trigger={['click']} menu={{items:trackItems}}><Button size="small" type="text" icon={<MoreOutlined/>} aria-label={`${track.name} 轨道菜单`}/></Dropdown>
       </div>
      </div>
     </Dropdown>
     <div data-lane className={`timeline-lane${track.group_id?' agent-lane':''}`} style={{width}} onPointerDown={event=>{if(event.button!==0||event.target!==event.currentTarget)return;event.currentTarget.closest<HTMLElement>('.sequence-timeline')?.focus({preventScroll:true});e.setSelectedTrack(track.id);e.select([]);boxStart.current={x:event.clientX,y:event.clientY};event.currentTarget.setPointerCapture(event.pointerId);}}
      onPointerMove={event=>{if(!boxStart.current)return;const a=boxStart.current,bounds=scroller.current!.getBoundingClientRect();setBox({x:Math.min(a.x,event.clientX)-bounds.left+scroller.current!.scrollLeft,y:Math.min(a.y,event.clientY)-bounds.top+scroller.current!.scrollTop,w:Math.abs(event.clientX-a.x),h:Math.abs(event.clientY-a.y)});}}
      onPointerUp={event=>{if(!boxStart.current)return;const a=boxStart.current;boxStart.current=null;setBox(null);if(Math.abs(a.x-event.clientX)<4&&Math.abs(a.y-event.clientY)<4){e.setHead(snap((event.clientX-event.currentTarget.getBoundingClientRect().left)/px,event.altKey));return;}const ids=[...scroller.current!.querySelectorAll<HTMLElement>('[data-clip-id]')].filter(el=>{const r=el.getBoundingClientRect();return r.left<Math.max(a.x,event.clientX)&&r.right>Math.min(a.x,event.clientX)&&r.top<Math.max(a.y,event.clientY)&&r.bottom>Math.min(a.y,event.clientY);}).map(el=>el.dataset.clipId!);e.select(ids,event.altKey);}}
      onDragOver={event=>{if(!event.dataTransfer.types.includes('application/x-vac-resource'))return;event.preventDefault();event.dataTransfer.dropEffect=track.locked?'none':'copy';const at=snap((event.clientX-event.currentTarget.getBoundingClientRect().left)/px,event.altKey),r=p.resource;setDrop({track:track.id,at,duration:Math.max(1,Math.round((r?.kind==='image'?5:(r?.out ?? 5)-(r?.in ?? 0))*rate)),insert:insert||event.shiftKey});}}
      onDragLeave={()=>setDrop(null)} onDrop={event=>{event.preventDefault();setDrop(null);try{const data=JSON.parse(event.dataTransfer.getData('application/x-vac-resource'));if(data.session!==p.session?.id||data.project!==e.projectId)throw Error('拖入资源来自其他工程');const at=snap((event.clientX-event.currentTarget.getBoundingClientRect().left)/px,event.altKey);void e.addResource(data.resource,track.id,at,insert||event.shiftKey?'insert':'overwrite').catch(reason=>setNotice(String(reason)));}catch(reason){setNotice(`无法拖入：${String(reason)}`);}}}>
      <div className="timeline-playhead" style={{left:e.head*px}}/>
      {track.clips.map(clip=><Dropdown key={clip.id} trigger={['contextMenu']} menu={{items:clipMenu(clip,track)}} onOpenChange={open=>{if(open){if(!selected.has(clip.id))e.select([clip.id]);e.setSelectedTrack(track.id);}}}>
       <div className={`timeline-clip${clip.origin.actor.startsWith('agent')?' is-agent':''}${selected.has(clip.id)?' is-selected':''}${clip.link_id?' is-linked':''}${track.hidden||track.muted?' is-disabled':''}`} data-clip-id={clip.id} role="button" tabIndex={0} aria-label={`${clip.name} · ${track.kind} · ${frameLabel(clip.at,s.fps)}`} aria-pressed={selected.has(clip.id)} style={{left:clip.at*px,width:Math.max(3,clip.duration*px)}}
        onPointerDown={event=>begin(event,clip,track,'move')} onPointerMove={move} onPointerUp={finish}
        onKeyDown={event=>{if(event.key==='Enter'){event.preventDefault();event.stopPropagation();e.select([clip.id]);e.setSelectedTrack(track.id);}}}>
        {track.kind==='audio'?<><ClipLoudness clip={clip} track={track} px={px} scroller={scroller}/><ClipGainBand clip={clip} track={track} px={px} height={track.height}/></>:track.kind==='video'?<ClipThumbnails clip={clip}/>:null}
        <span className="clip-title">{clip.origin.actor.startsWith('agent')?'Agent · ':''}{clip.name}{track.kind==='audio'?` · 流 ${clip.stream_index}`:''}{clip.link_id?' ↔':''}</span>
        {clip.effects.fade_in>0&&<span className="clip-effect fade-in" style={{width:clip.effects.fade_in*px}} title="淡入"/>}{clip.effects.fade_out>0&&<span className="clip-effect fade-out" style={{width:clip.effects.fade_out*px}} title="淡出"/>}{clip.effects.transition>0&&<span className="clip-transition" style={{width:clip.effects.transition*px}}>溶解</span>}
        {effectLabel(clip)&&<><span className="clip-effects-label">{effectLabel(clip)}</span><span className="clip-effect-scope" aria-label={`效果范围 ${clip.effects.from_frame??0} 至 ${clip.effects.to_frame??clip.duration} 帧`} style={{left:(clip.effects.from_frame??0)*px,width:Math.max(1,((clip.effects.to_frame??clip.duration)-(clip.effects.from_frame??0))*px)}}/></>}
        {p.session?.document?.resources.find(r=>r.id===clip.resource_id)?.markers?.filter(m=>m.source_time && timeSeconds(m.source_time)>=timeSeconds(clip.source_in)&&timeSeconds(m.source_time)<timeSeconds(clip.source_out)).map(m=><button key={m.id} className="source-marker" aria-label={`素材标记 ${m.notes}`} title={m.notes} style={{left:(timeSeconds(m.source_time!)-timeSeconds(clip.source_in))/clip.speed*rate*px}} onPointerDown={event=>event.stopPropagation()} onClick={event=>{event.stopPropagation();e.setHead(clip.at+Math.round((timeSeconds(m.source_time!)-timeSeconds(clip.source_in))/clip.speed*rate));e.select([clip.id]);}}>◆</button>)}
        <span className="clip-trim trim-left" title="拖动修剪起点；也可在右侧设置起始帧与时长" onPointerDown={event=>begin(event,clip,track,'left')} onPointerMove={move} onPointerUp={finish}/><span className="clip-trim trim-right" title="拖动修剪终点；也可在右侧设置时长" onPointerDown={event=>begin(event,clip,track,'right')} onPointerMove={move} onPointerUp={finish}/>
       </div>
      </Dropdown>)}
      {ghost&&ghost.target===track.id&&<div className="timeline-ghost" style={{left:(ghost.kind==='left'?ghost.at+ghost.delta:ghost.at+(ghost.kind==='move'?ghost.delta:0))*px,width:Math.max(1,(ghost.end-ghost.at+(ghost.kind==='right'?ghost.delta:ghost.kind==='left'?-ghost.delta:0))*px)}}>{track.locked?'轨道已锁定':`${frameLabel(ghost.at+ghost.delta,s.fps)}${ghost.snapped?' · 已吸附':''} · Esc 取消`}</div>}
      {drop&&affectedDrop(track)&&<div className="timeline-drop" style={{left:drop.at*px,width:drop.duration*px}}>{track.locked?'涉及锁定轨道，不能放入':`${drop.insert?'插入 · 后续内容及标记后移':`覆盖 ${clipCount(track,drop.at,drop.duration)} 个片段`}${track.kind==='video'&&audioCount?` · 同时放入 ${audioCount} 条音轨，不足时扩展`:''}`}</div>}
     </div>
    </div></Fragment>;})}
   </div>{box&&<div className="timeline-selection-box" style={{left:box.x,top:box.y,width:box.w,height:box.h}}/>}
  </div>
  <div className="timeline-help" role="status">{notice||'Alt 暂停吸附 · Esc 取消拖动'}</div>
 </section>;
}
