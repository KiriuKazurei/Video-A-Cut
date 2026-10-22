import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Button, Card, Checkbox, Descriptions, Divider, Empty, Flex, Input, InputNumber, Select, Space, Tag, Typography } from 'antd';
import { useProjectWorkspace } from '../projectWorkspace';
import { useSequenceEditor } from '../sequenceEditor';
import { frameLabel, fpsValue, timeSeconds, type TimelineClip, type TimelineMarker, type ClipEffects } from '../timelineContracts';
import { constantGainEffects } from '../gainEnvelope';
import { ResourceInspector } from './ResourceInspector';
import { api } from '../api';

function MarkerRow({marker,resourceId}:{marker:TimelineMarker;resourceId?:string}) {
 const e=useSequenceEditor(),[notes,setNotes]=useState(marker.notes),[frame,setFrame]=useState(marker.frame);
 useEffect(()=>{setNotes(marker.notes);setFrame(marker.frame);},[marker.id,marker.notes,marker.frame]);
 const update=(patch:Partial<TimelineMarker>)=>void e.command([{type:'marker.update',marker_id:marker.id,resource_id:resourceId,marker:{...marker,...patch}}],'修改标记及绑定引用').catch(()=>{});
 return <div className="marker-row"><Input aria-label={`标记备注 ${marker.id}`} value={notes} onChange={event=>setNotes(event.target.value)} onBlur={()=>{if(notes!==marker.notes)update({notes});}} onPressEnter={()=>{if(notes!==marker.notes)update({notes});}}/>
  <Flex gap={6} align="center"><Tag>{resourceId?'素材标记':marker.kind==='point'?'标记点':marker.kind==='in'?'入点':'出点'}</Tag>
   {resourceId?<Typography.Text type="secondary">{marker.source_time?timeSeconds(marker.source_time).toFixed(3):'--'} 秒</Typography.Text>:<InputNumber aria-label={`标记帧 ${marker.id}`} min={0} value={frame} onChange={value=>setFrame(value ?? 0)} onBlur={()=>{if(frame!==marker.frame)update({frame});}}/>}
   <Button size="small" type="text" aria-label={`移除标记 ${marker.notes}`} onClick={()=>void e.command([{type:'marker.remove',marker_id:marker.id,resource_id:resourceId}],'移除标记').catch(()=>{})}>移除</Button>
  </Flex>{!resourceId&&<Checkbox checked={marker.locked} onChange={event=>update({locked:event.target.checked})}>固定绝对时码</Checkbox>}
 </div>;
}
function ClipProperties({clip,trackId}:{clip:TimelineClip;trackId:string}) {
 const e=useSequenceEditor(),s=e.view!.sequence,[effects,setEffects]=useState<ClipEffects>(clip.effects),[speed,setSpeed]=useState(clip.speed),[at,setAt]=useState(clip.at),[duration,setDuration]=useState(clip.duration),[target,setTarget]=useState(trackId),[name,setName]=useState(clip.name),[text,setText]=useState(clip.text ?? '');
 useEffect(()=>{setEffects(clip.effects);setSpeed(clip.speed);setAt(clip.at);setDuration(clip.duration);setTarget(trackId);setName(clip.name);setText(clip.text ?? '');},[clip.id,s.revision]);
 const rate=fpsValue(s.fps),track=s.tracks.find(t=>t.id===trackId)!,locked=track.locked;
 const [font,setFont]=useState(clip.font),[fontSize,setFontSize]=useState(clip.font_size||36);
 useEffect(()=>{setFont(clip.font);setFontSize(clip.font_size||36);},[clip.id,s.revision]);
 const fonts=useQuery({queryKey:['script-fonts'],queryFn:api.scriptFonts,enabled:track.kind==='subtitle',staleTime:60_000});
 const fx=(key:keyof ClipEffects,value:number|undefined)=>setEffects(old=>({...old,[key]:value}));
 const run=(...args:Parameters<typeof e.command>)=>void e.command(...args).catch(()=>{});
 const ids=e.selected.length?e.selected:[clip.id];
 return <>
  <Descriptions size="small" column={1} items={[{key:'source',label:'来源',children:clip.origin.actor.startsWith('agent')?<Tag color="purple">Agent · {clip.origin.run_id ?? '结构化编辑'}</Tag>:'人工'},{key:'stream',label:'源流索引',children:clip.stream_index},{key:'source-range',label:'源范围',children:`${timeSeconds(clip.source_in).toFixed(6)}–${timeSeconds(clip.source_out).toFixed(6)} 秒`},{key:'time',label:'序列位置',children:frameLabel(clip.at,s.fps)}]}/>
  <div className="timeline-property-grid">
   <label>片段名称<Input aria-label="片段名称" value={name} onChange={event=>setName(event.target.value)} onBlur={()=>{if(name!==clip.name)run([{type:'clip.update',clip_ids:[clip.id],name,unlinked:true}],'片段改名');}}/></label>
   <label>目标轨道<Select aria-label="片段目标轨道" value={target} onChange={setTarget} options={s.tracks.filter(t=>t.kind===track.kind).map((t,i)=>({value:t.id,label:`${i+1} · ${t.name}`,disabled:t.locked}))}/></label>
   <label>起始帧<InputNumber aria-label="片段起始帧" min={0} precision={0} value={at} onChange={value=>setAt(value ?? 0)}/></label>
   <label>时长（帧）<InputNumber aria-label="片段时长帧" min={1} precision={0} value={duration} onChange={value=>setDuration(value ?? 1)}/></label>
  </div>
  <Flex gap={6} wrap><Button size="small" disabled={locked||e.pending} onClick={()=>run([{type:'clip.move',clip_ids:ids,at,target_track_id:target}],'移动片段')}>应用位置</Button><Button size="small" disabled={locked||e.pending} onClick={()=>run([{type:'clip.update',clip_ids:ids,duration}],'调整时长')}>应用时长</Button><Button size="small" disabled={locked||e.pending} onClick={()=>run([{type:clip.link_id?'clip.unlink':'clip.link',clip_ids:ids}],'音画链接')}>{clip.link_id?'分离音画':'链接所选'}</Button></Flex>
  <Divider plain>速度与效果</Divider>
  <div className="timeline-property-grid">
   <label>速度（保持音调）<InputNumber aria-label="片段速度" min={.05} max={100} step={.1} value={speed} onChange={value=>setSpeed(value ?? 1)}/></label>
   <label>音量 dB<InputNumber aria-label="片段音量" min={-96} max={24} value={effects.gain_db} onChange={value=>setEffects(old=>constantGainEffects({...old,gain_db:value ?? 0}, value ?? 0))}/></label>
   {!!effects.gain_keyframes?.length&&<Typography.Text type="secondary" style={{gridColumn:'1 / -1'}}>时间线增益线有 {effects.gain_keyframes.length} 个关键帧；在此改音量会替换为恒定增益。</Typography.Text>}
   <label>淡入（秒）<InputNumber aria-label="片段淡入" min={0} max={clip.duration/rate} step={.1} value={effects.fade_in/rate} onChange={value=>fx('fade_in',Math.round((value ?? 0)*rate))}/></label>
   <label>淡出（秒）<InputNumber aria-label="片段淡出" min={0} max={clip.duration/rate} step={.1} value={effects.fade_out/rate} onChange={value=>fx('fade_out',Math.round((value ?? 0)*rate))}/></label>
   <label>淡变颜色<Select aria-label="淡变颜色" value={effects.fade_color || 'black'} onChange={value=>setEffects(fx=>({...fx,fade_color:value}))} options={[{value:'black',label:'黑场'},{value:'white',label:'白场'}]}/></label>
   <label>交叉溶解（秒）<InputNumber aria-label="交叉溶解时长" min={0} max={clip.duration/rate} step={.1} value={effects.transition/rate} onChange={value=>fx('transition',Math.round((value ?? 0)*rate))}/></label>
   {track.kind==='video'&&<><label>缩放<InputNumber aria-label="视频缩放" min={.01} max={8} step={.1} value={effects.scale} onChange={value=>fx('scale',value ?? 1)}/></label>
   {(['x','y'] as const).map(axis=><label key={axis}>位置 {axis.toUpperCase()}<InputNumber aria-label={`视频位置 ${axis}`} value={effects[axis]} onChange={value=>fx(axis,value ?? 0)}/></label>)}
   {([['crop_left','左裁切'],['crop_right','右裁切'],['crop_top','上裁切'],['crop_bottom','下裁切']] as const).map(([key,label])=><label key={key}>{label} %<InputNumber aria-label={label} min={0} max={99} value={effects[key]*100} onChange={value=>fx(key,(value ?? 0)/100)}/></label>)}</>}
   <label>效果起始帧<InputNumber aria-label="效果起始帧" min={0} max={clip.duration-1} value={effects.from_frame ?? 0} onChange={value=>fx('from_frame',value ?? undefined)}/></label><label>效果终止帧<InputNumber aria-label="效果终止帧" min={1} max={clip.duration} value={effects.to_frame ?? clip.duration} onChange={value=>fx('to_frame',value ?? undefined)}/></label>
  </div>
  {track.kind==='subtitle'&&<><div className="timeline-property-grid"><label>字幕字体<Select aria-label="字幕叠加字体" placeholder="自动选择本机字体" value={font} showSearch onChange={setFont} options={fonts.data?.map(value=>({value,label:value}))}/></label><label>字号（像素）<InputNumber aria-label="字幕叠加字号" min={1} max={512} value={fontSize} onChange={value=>setFontSize(value??36)}/></label></div><Input.TextArea aria-label="字幕叠加正文" rows={3} value={text} onChange={event=>setText(event.target.value)}/>{!text.trim()&&<Typography.Text type="warning">请输入字幕，或移除此片段。</Typography.Text>}</>}
  <Button block type="primary" disabled={locked||e.pending||(track.kind==='subtitle'&&!text.trim())} onClick={()=>run([{type:'clip.update',clip_ids:ids,speed,effects,...(track.kind==='subtitle'?{text,font,font_size:fontSize}:{})}],'应用速度与效果')}>应用到所选片段</Button>
  {locked&&<Typography.Text type="warning">此轨道已锁定。</Typography.Text>}
 </>;
}
export function TimelineInspector() {
 const e=useSequenceEditor(),p=useProjectWorkspace(),s=e.view?.sequence;
 const track=s?.tracks.find(t=>t.clips.some(c=>e.selected.includes(c.id))),clip=track?.clips.find(c=>e.selected.includes(c.id));
 const resource=p.session?.document?.resources.find(r=>r.id===clip?.resource_id) ?? p.resource;
 return <div className="timeline-inspector" tabIndex={-1}>
  <Card size="small" variant="borderless" title="片段与效果" styles={{body:{padding:12}}}>{clip&&track?<ClipProperties clip={clip} trackId={track.id}/>:<Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="选择时间线片段以编辑属性"/>}</Card>
  <Card size="small" variant="borderless" title="标记" extra={<Space size={4}><Button size="small" onClick={()=>e.marker()}>时间线</Button><Button size="small" disabled={!resource||!['video','audio','image'].includes(resource.kind)} onClick={()=>e.marker('point',true)}>素材</Button></Space>}>
   <Flex vertical gap={8}>{s?.markers.map(m=><MarkerRow key={m.id} marker={m}/>)}{resource?.markers?.map(m=><MarkerRow key={m.id} marker={m} resourceId={resource.id}/>)}{!s?.markers.length&&!resource?.markers?.length&&<Typography.Text type="secondary">M 添加标记；I / O 添加入出点。备注可直接在脚本中引用。</Typography.Text>}</Flex>
  </Card>
  <ResourceInspector/>
 </div>;
}
