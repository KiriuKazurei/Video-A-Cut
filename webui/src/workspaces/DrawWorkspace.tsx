import {useEffect,useRef,useState,type DragEvent} from 'react';
import {useQuery,useQueryClient} from '@tanstack/react-query';
import {Alert,Button,Empty,Flex,Input,InputNumber,Radio,Slider,Space,Spin,Tabs,Tag,Typography,Select,Tooltip,Dropdown,Collapse} from 'antd';
import {useProjectWorkspace} from '../projectWorkspace';
import {useSequenceEditor} from '../sequenceEditor';
import {useSkillWorkspace} from '../skillWorkspace';
import {projectApi} from '../projectApi';
import {skillApi} from '../skillApi';
import {timelineApi} from '../timelineApi';
import {desktopCommand,isDesktop} from '../desktop';
import {errorText} from '../components/ui';
import type {RouteState} from '../navigation';
import {DRAW_ZONES,drawAgentContext,drawDraftChanged,newDrawComposition,newDrawImage,drawImageReference,zoneForFocus,type DrawComposition,type DrawEngine,type DrawRender,type DrawImageLayer} from '../drawContracts';
import {drawApi} from '../drawApi';
import {PlusOutlined,SaveOutlined,CheckCircleOutlined,PlayCircleOutlined,MoreOutlined,FullscreenOutlined,FullscreenExitOutlined,StepBackwardOutlined,StepForwardOutlined,PictureOutlined,CodeOutlined,ImportOutlined} from '@ant-design/icons';

interface Props{route:RouteState;navigate:(state:Partial<RouteState>)=>void}
const labels={hyperframes:'HyperFrames',remotion:'Remotion'};
export function DrawWorkspace({route,navigate}:Props){
 const skillWorkspace=useSkillWorkspace(),project=useProjectWorkspace(),editor=useSequenceEditor(),client=useQueryClient(),session=project.session;
 const id=session?.document?.project.id??'',active=route.workspace==='draw',engine=route.drawEngine??'hyperframes';
 const query=useQuery({queryKey:['draw-compositions',session?.id,id],queryFn:({signal})=>drawApi.list(id,signal),enabled:active&&!!id});
 const list=query.data??[],selected=list.find(c=>c.id===route.drawSlot)??list.find(c=>c.engine===engine);
 const [draft,setDraft]=useState<DrawComposition>(),[busy,setBusy]=useState(''),[failure,setFailure]=useState(''),[notice,setNotice]=useState(''),[report,setReport]=useState<DrawRender>(),[mediaMissing,setMediaMissing]=useState(false),[head,setHead]=useState(0),[goal,setGoal]=useState('');
 const [previewOnly,setPreviewOnly]=useState(false),[dropActive,setDropActive]=useState(false);
 const [imageID,setImageID]=useState(''),[sourceTab,setSourceTab]=useState('compositions'),[inspectorTab,setInspectorTab]=useState('inspector');
 const player=useRef<HTMLVideoElement>(null),busyRef=useRef(false),base=useRef('');
 const dirty=drawDraftChanged(draft,selected),scope=zoneForFocus(route.drawFocus),blocked=!!route.drawProject&&route.drawProject!==id;
 useEffect(()=>{if(!selected){setDraft(undefined);base.current='';return;}if(base.current!==selected.id){setDraft(structuredClone(selected));base.current=selected.id;setReport(undefined);setMediaMissing(false);setHead(0);}else if(!dirty)setDraft(structuredClone(selected));},[selected?.id,selected?.revision]);
 useEffect(()=>{if(route.drawFocus==='source'){setSourceTab('source');setPreviewOnly(false);}if(route.drawFocus==='lint'){setInspectorTab('lint');setPreviewOnly(false);}if(route.drawFocus==='render-queue'){setInspectorTab('queue');setPreviewOnly(false);}document.querySelector<HTMLElement>('[data-draw-zone="'+scope+'"]')?.focus();},[route.drawFocus,scope]);
 useEffect(()=>{if(!active)player.current?.pause();},[active]);
 const execute=async(label:string,fn:()=>Promise<void>)=>{
  if(busyRef.current)return;busyRef.current=true;setBusy(label);setFailure('');
  try{await fn();}catch(error){setFailure(errorText(error));throw error;}finally{busyRef.current=false;setBusy('');}
 };
 const persist=async()=>{
  if(!draft||!session||blocked)throw Error('请先打开当前工程并选择构图');
  if(!dirty&&draft.id)return draft;
  const current=await projectApi.workspace();if(current.id!==session.id)throw Error('工程已切换');
  const result=await drawApi.save(id,{session:session.id,expected_revision:current.revision,idempotency_key:crypto.randomUUID(),composition:draft});
  const saved=result.document?.draw_compositions?.find(c=>c.id===draft.id)??result.document?.draw_compositions?.at(-1);
  if(!saved)throw Error('构图保存响应无效');
  setDraft(structuredClone(saved));await project.refresh();await query.refetch();return saved;
 };
 useEffect(()=>project.registerEditor('draw-source',{dirty:active&&dirty,flush:async()=>{await persist();}}),[active,dirty,draft,session?.id]);
 const choose=async(c:DrawComposition)=>{await project.flushEditors();player.current?.pause();navigate({workspace:'draw',drawProject:id,drawSlot:c.id,drawEngine:c.engine,drawFocus:'canvas'});};
 const create=async()=>{
  await project.flushEditors();const current=await projectApi.workspace();if(current.id!==session?.id)throw Error('工程已切换');
  const result=await drawApi.save(id,{session:current.id,expected_revision:current.revision,idempotency_key:crypto.randomUUID(),composition:newDrawComposition(engine)});
  const c=result.document!.draw_compositions!.at(-1)!;await project.refresh();await query.refetch();await choose(c);setSourceTab('source');
 };
 const check=async()=>{const c=await persist(),result=await drawApi.check(id,c.id,{session:session!.id,content_version:c.content_version});setInspectorTab('lint');setNotice('检查通过 · '+labels[c.engine]+' '+result.runtime_version+' · '+result.checks.join(' / '));};
 const render=async()=>{const c=await persist();await drawApi.check(id,c.id,{session:session!.id,content_version:c.content_version});const next=await drawApi.render(id,c.id,{session:session!.id,content_version:c.content_version});setReport(next);setMediaMissing(false);setInspectorTab('queue');setNotice('真实渲染完成，参数和文件指纹校验通过。');};
 const importVideo=async()=>{
  const c=await persist(),sequence=editor.sequenceId;if(!sequence)throw Error('当前工程没有可用序列');
  const view=await timelineApi.get(id,sequence);
  await drawApi.import(id,c.id,{session:session!.id,content_version:c.content_version,sequence_id:sequence,expected_revision:view.sequence.revision,idempotency_key:crypto.randomUUID()});
  await project.refresh();await client.invalidateQueries({queryKey:['timeline']});navigate({workspace:'editing',editingSection:'import'});
 };
 const createRun=async()=>{
  const c=await persist(),visible=await skillApi.visibleSet(id);
  const run=await skillApi.createRun({project_id:id,sequence_id:editor.sequenceId,flow:'custom',skill_id:'vac-draw-'+c.engine,strategy:'draw',execution_mode:'external_mcp',visible_set_sha:visible.sha256,goal:(goal.trim()||'根据当前构图源码制作并检查视频，可继续在编辑时间线剪辑。')+'\n构图 ID：'+c.id,idempotency_key:crypto.randomUUID()});
  skillWorkspace.setRunId(run.id);navigate({workspace:'editing',editingSection:'import'});
 };
 const copyContext=async()=>{const text=drawAgentContext({project_id:id,slot_id:selected?.id,skill_run_id:route.drawRun,sequence_id:editor.sequenceId,engine,focus:route.drawFocus});if(isDesktop())await desktopCommand('clipboard_write_text',{text});else await navigator.clipboard.writeText(text);setNotice('已复制当前工程的绘制上下文。');};
 const run=(label:string,fn:()=>Promise<void>)=>void execute(label,fn).catch(()=>{});
 const patch=(value:Partial<DrawComposition>)=>setDraft(old=>old?{...old,...value}:old);
 const fps=draft?draft.fps.num/draft.fps.den:30,canUse=!!id&&!blocked&&!busy,version=selected?.content_version;
 const images=draft?.images??[],image=images.find(layer=>layer.id===imageID),resources=session?.document?.resources??[],availableImages=resources.filter(resource=>resource.kind==='image'&&!resource.removed);
 const imageName=(layer:DrawImageLayer)=>resources.find(resource=>resource.id===layer.resource_id)?.name??'图片资源不可用';
 const addImage=(resourceID:string)=>{
  if(!draft||!canUse)throw Error('请先新建或选择当前工程构图');
  const resource=availableImages.find(item=>item.id===resourceID);
  if(!resource||resource.missing||resource.changed)throw Error('图片不可用，请在资源管理中重新定位或更新原件');
  if(images.length>=64)throw Error('每个构图最多 64 个图片实例');
  const layer=newDrawImage(resource,draft);patch({images:[...images,layer]});setImageID(layer.id);setSourceTab('images');setInspectorTab('inspector');
 };
 const patchImage=(value:Partial<DrawImageLayer>)=>{if(image)patch({images:images.map(item=>item.id===image.id?{...item,...value}:item)});};
 const copyImage=async()=>{if(!image)return;const text=drawImageReference(draft!.engine,image);if(isDesktop())await desktopCommand('clipboard_write_text',{text});else await navigator.clipboard.writeText(text);setNotice('已复制图片源码引用。源码模式需在构图中使用该引用。');};
 const dropImage=(event:DragEvent<HTMLElement>)=>{
  setDropActive(false);
  if(!event.dataTransfer.types.includes('application/x-vac-resource'))return;
  event.preventDefault();
  run('加入图片',async()=>{const data=JSON.parse(event.dataTransfer.getData('application/x-vac-resource'));if(data.project!==id||data.session!==session?.id)throw Error('图片来自其他工程或已失效会话');addImage(data.resource);});
 };
 const seek=(frame:number)=>{setHead(frame);if(player.current&&player.current.readyState>=1)player.current.currentTime=frame/fps;};
 return <div className={'draw-studio'+(previewOnly?' is-preview-only':'')} data-testid="draw-workspace" data-draw-engine={engine} data-draw-focus={scope} aria-busy={!!busy}>
  <div className="draw-toolbar">
   <div className="draw-toolbar-heading"><Typography.Text strong>绘制</Typography.Text><Radio.Group size="small" optionType="button" buttonStyle="solid" aria-label="绘制引擎" value={engine} disabled={!!busy} onChange={event=>run('切换引擎',async()=>{await project.flushEditors();navigate({workspace:'draw',drawProject:id,drawEngine:event.target.value as DrawEngine,drawSlot:'',drawFocus:'canvas'});})} options={(['hyperframes','remotion'] as const).map(value=>({value,label:labels[value]}))}/></div>
   <Space size={6} wrap className="draw-toolbar-actions">
    <Button size="small" icon={<PlusOutlined aria-hidden="true"/>} disabled={!canUse} onClick={()=>run('新建构图',create)}>新建构图</Button>
    <Button size="small" icon={<SaveOutlined aria-hidden="true"/>} disabled={!canUse||!draft||!dirty} onClick={()=>run('保存源码',async()=>{await persist();})}>保存源码</Button>
    <Button size="small" icon={<CheckCircleOutlined aria-hidden="true"/>} disabled={!canUse||!draft} onClick={()=>run('检查源码',check)}>Lint / Check</Button>
    <Button size="small" type="primary" icon={<PlayCircleOutlined aria-hidden="true"/>} loading={busy==='正在本地渲染'} disabled={!canUse||!draft} onClick={()=>run('正在本地渲染',render)}>渲染视频</Button>
    <Dropdown trigger={['click']} menu={{items:[{key:'context',label:'复制上下文给 Agent',disabled:!id||!!busy},{key:'reset',label:'重置布局'}],onClick:({key})=>{if(key==='context')run('复制上下文',copyContext);else{setPreviewOnly(false);setSourceTab('compositions');setInspectorTab('inspector');navigate({workspace:'draw',drawFocus:'canvas'});}}}}><Button size="small" aria-label="更多绘制操作" icon={<MoreOutlined aria-hidden="true"/>}>更多</Button></Dropdown>
   </Space>
   <div className="draw-operation-status" role="status" aria-live="polite">{busy?<><Spin size="small"/><span>{busy}</span></>:dirty?<Tag>源码有未保存更改</Tag>:<Typography.Text type="secondary">{selected?'构图已保存':'新建构图开始'}</Typography.Text>}</div>
   {(failure||query.error)&&<Alert className="draw-toolbar-alert" showIcon type="error" message={failure||errorText(query.error)}/>}
   {blocked&&<Alert className="draw-toolbar-alert" type="warning" message="深链工程与当前窗口不一致，未执行跨工程操作。"/>}
  </div>
  <section className={'draw-zone draw-zone-source'+(scope==='source'?' is-focused':'')} data-draw-zone="source" hidden={previewOnly} tabIndex={-1} aria-label={DRAW_ZONES[0].label}>
   <div className="draw-panel-title"><CodeOutlined aria-hidden="true"/><Typography.Text strong>构图与素材</Typography.Text></div>
   <Tabs size="small" animated={false} activeKey={sourceTab} onChange={setSourceTab} items={[
    {key:'compositions',label:'构图',children:<Flex vertical gap={6}>{list.filter(c=>c.engine===engine).map(c=><Button key={c.id} block aria-pressed={c.id===selected?.id} type={c.id===selected?.id?'primary':'default'} disabled={!!busy} onClick={()=>run('选择构图',()=>choose(c))}>{c.name}</Button>)}{!list.some(c=>c.engine===engine)&&<Empty description="新建构图，或让 Agent 保存源码"/>}</Flex>},
    {key:'source',label:engine==='remotion'?'组件源码':'HTML 源码',children:draft?<Input.TextArea className="draw-source-editor" rows={18} wrap="off" aria-label="构图源码" value={draft.source} spellCheck={false} disabled={!!busy} onChange={event=>patch({source:event.target.value})}/>:<Empty description="选择或新建构图"/>},
    {key:'images',label:'图片',children:<Flex vertical gap={8}>
     <Typography.Text strong>工程图片</Typography.Text>
     {!availableImages.length&&<Empty description="先从文件菜单导入图片"/>}
     {availableImages.map(resource=><Button key={resource.id} className="draw-image-resource" disabled={!canUse||!draft||resource.missing||resource.changed} draggable={canUse&&!!draft&&!resource.missing&&!resource.changed} onDragStart={event=>{event.dataTransfer.setData('application/x-vac-resource',JSON.stringify({project:id,session:session?.id,resource:resource.id}));event.dataTransfer.effectAllowed='copy';}} onClick={()=>run('加入图片',async()=>addImage(resource.id))}>
      <img src={timelineApi.thumbnailURL(id,resource.id,0)+(resource.raw_develop?"&recipe="+encodeURIComponent(JSON.stringify(resource.raw_develop)):"")} alt=""/><span>{resource.name}{resource.missing?'（缺失）':resource.changed?'（已变化）':''}</span>
     </Button>)}
     <Typography.Text type="secondary">点击加入，或拖到预览区。后加入的图片位于上方；动图使用静态首帧。</Typography.Text>
     <Typography.Text strong>图片图层 · {images.length}/64</Typography.Text>
     {images.map((layer,index)=><Button key={layer.id} className="draw-layer-item" block aria-pressed={layer.id===imageID} type="default" disabled={!!busy} onClick={()=>{setImageID(layer.id);setInspectorTab('inspector');}}>{index+1+'. '+imageName(layer)}</Button>)}
    </Flex>}

   ]}/>
  </section>
  <section className={'draw-zone draw-zone-canvas'+(scope==='canvas'?' is-focused':'')+(dropActive?' is-drop-target':'')} data-draw-zone="canvas" onDragOver={event=>{if(canUse&&draft&&event.dataTransfer.types.includes("application/x-vac-resource")){event.preventDefault();event.dataTransfer.dropEffect="copy";setDropActive(true);}}} onDragLeave={event=>{if(!event.currentTarget.contains(event.relatedTarget as Node|null))setDropActive(false);}} onDrop={dropImage} tabIndex={-1} aria-label={DRAW_ZONES[1].label}>
   <div className="draw-panel-title"><Typography.Text strong>构图预览</Typography.Text><Typography.Text type="secondary" className="draw-preview-metadata">{draft?draft.width+' × '+draft.height+' · '+fps.toFixed(2)+' fps':''}</Typography.Text><Tooltip title={previewOnly?'返回编辑布局':'放大预览，保留时间控制'}><Button size="small" type="text" icon={previewOnly?<FullscreenExitOutlined/>:<FullscreenOutlined/>} aria-label={previewOnly?'退出专注预览':'专注预览'} aria-pressed={previewOnly} onClick={()=>setPreviewOnly(value=>!value)}/></Tooltip></div>
   <div className="draw-canvas-frame">
    {!active||!selected||!version?<Empty description={id?'新建构图开始绘制':'请先新建或打开工程'}/>:mediaMissing?<Empty description="当前版本尚未渲染，请点击“渲染视频”"/>:<video key={selected.id+version+(report?.sha256??'')} ref={player} controls preload="metadata" aria-label="绘制成片预览" src={drawApi.renderURL(id,selected.id,version)} onError={()=>setMediaMissing(true)} onLoadedMetadata={()=>{if(player.current)player.current.currentTime=head/fps;}} onTimeUpdate={()=>{if(player.current&&!player.current.seeking&&player.current.readyState>=1)setHead(Math.round(player.current.currentTime*fps));}}/>}
    <div className="draw-drop-feedback" aria-hidden={!dropActive}><PictureOutlined aria-hidden="true"/><strong>松开以加入工程图片</strong><span>位置、尺寸与位移可在右侧调整</span></div>
   </div>
   <div className="draw-preview-status"><Typography.Text ellipsis type="secondary">{dirty?'这是上次保存源码的预览；请保存并重新渲染。':selected?selected.name+' · '+labels[selected.engine]+' · '+selected.content_version.slice(0,8):'源码由本地 Worker 渲染后在此预览'}</Typography.Text>{busy==='正在本地渲染'&&<span className="draw-rendering-label">正在生成新预览…</span>}</div>
  </section>
  <section className="draw-zone draw-zone-timeline" data-draw-zone="timeline" tabIndex={-1} aria-label={DRAW_ZONES[2].label}>
   <div className="draw-timeline-heading"><Typography.Text strong>构图时间轴</Typography.Text><Typography.Text type="secondary">{head+' / '+(draft?.duration_frames??0)+' 帧 · '+fps.toFixed(3)+' fps'}</Typography.Text><Typography.Text type="secondary" className="draw-timeline-hint">导入后可继续在编辑时间线剪辑</Typography.Text></div>
   <div className="draw-transport"><Button size="small" icon={<StepBackwardOutlined aria-hidden="true"/>} disabled={!selected||mediaMissing} onClick={()=>seek(Math.max(0,head-1))}>上一帧</Button><Slider aria-label="构图播放帧" min={0} max={Math.max(0,(draft?.duration_frames??1)-1)} step={1} value={head} disabled={!selected||mediaMissing||!!busy} onChange={seek}/><Button size="small" icon={<StepForwardOutlined aria-hidden="true"/>} disabled={!selected||mediaMissing} onClick={()=>seek(Math.min((draft?.duration_frames??1)-1,head+1))}>下一帧</Button></div>
  </section>
  <section className="draw-zone draw-zone-inspector" data-draw-zone="inspector" hidden={previewOnly} tabIndex={-1} aria-label={DRAW_ZONES[3].label}>
   <div className="draw-panel-title"><Typography.Text strong>属性与输出</Typography.Text></div>
   <Tabs size="small" animated={false} activeKey={inspectorTab} onChange={setInspectorTab} items={[
    {key:'inspector',label:'属性',children:draft?<Flex vertical gap={8}>
     {!image&&<div className="draw-composition-fields">
     <label className="draw-field-wide">名称<Input aria-label="构图名称" value={draft.name} disabled={!!busy} onChange={event=>patch({name:event.target.value})}/></label>
     <label>宽度<InputNumber aria-label="构图宽度" min={16} max={3840} step={2} value={draft.width} disabled={!!busy} onChange={value=>value!==null&&patch({width:value})}/></label>
     <label>高度<InputNumber aria-label="构图高度" min={16} max={2160} step={2} value={draft.height} disabled={!!busy} onChange={value=>value!==null&&patch({height:value})}/></label>
     <label>帧率<InputNumber aria-label="构图帧率" min={1} max={120} value={fps} disabled={!!busy} onChange={value=>value!==null&&patch({fps:{num:Math.round(value*1000),den:1000}})}/></label>
     <label>总帧数<InputNumber aria-label="构图总帧数" min={1} max={18000} value={draft.duration_frames} disabled={!!busy} onChange={value=>value!==null&&patch({duration_frames:value})}/></label>
     </div>}
     {image&&<>
      <Button size="small" onClick={()=>setImageID('')}>构图属性</Button>
      <Typography.Text strong>{imageName(image)}</Typography.Text>
      <Select aria-label="图片绘制方式" disabled={!!busy} value={image.mode??'layer'} onChange={value=>patchImage({mode:value})} options={[{value:'layer',label:'图层自动绘制'},{value:'source',label:'由源码引用'}]}/>
      <div className="draw-image-fields">
      {(['x','y','width','height','dx','dy'] as const).map(key=><label key={key}>{({x:'位置 X',y:'位置 Y',width:'宽度',height:'高度',dx:'位移 X',dy:'位移 Y'})[key]}<InputNumber aria-label={'图片'+({x:'位置 X',y:'位置 Y',width:'宽度',height:'高度',dx:'位移 X',dy:'位移 Y'})[key]} min={(key==='width'||key==='height')?0.1:-100000} max={key==='width'||key==='height'?32768:100000} value={image[key]} disabled={!!busy} onChange={value=>value!==null&&patchImage({[key]:value})}/></label>)}
      {(['from_frame','to_frame'] as const).map(key=><label key={key}>{key==='from_frame'?'动画起始帧':'动画结束帧'}<InputNumber aria-label={key==='from_frame'?'图片动画起始帧':'图片动画结束帧'} min={0} max={18000} step={1} precision={0} value={image[key]} disabled={!!busy} onChange={value=>value!==null&&patchImage({[key]:value})}/></label>)}
      </div>
      <Select aria-label="图片适配" disabled={!!busy} value={image.fit??'contain'} onChange={value=>patchImage({fit:value})} options={[{value:'contain',label:'等比包含'},{value:'cover',label:'等比填满'},{value:'fill',label:'拉伸'}]}/>
      <label>不透明度（%）<InputNumber aria-label="图片不透明度" min={0} max={100} value={(image.opacity??1)*100} disabled={!!busy} onChange={value=>value!==null&&patchImage({opacity:value/100})}/></label>
      <Space wrap><Button size="small" disabled={!!busy} onClick={()=>run('复制图片引用',copyImage)}>复制图片引用</Button><Button size="small" danger disabled={!!busy} onClick={()=>{patch({images:images.filter(item=>item.id!==image.id)});setImageID('');}}>移除图层</Button></Space>
      <Typography.Text type="secondary">位移在起止帧间线性变化。修改后保存并渲染。</Typography.Text><Collapse ghost size="small" items={[{key:'image-help',label:'源码引用说明',children:<Typography.Text type="secondary">源码模式不自动叠加图片。Remotion 组件接收 images；HyperFrames 使用 vac-image 引用。</Typography.Text>}]}/>
     </>}

    </Flex>:<Empty description="没有选中构图"/>},
    {key:'lint',label:'检查',children:<Alert type="info" message={notice||'保存源码后检查语法、模块、画面尺寸和时间基准。'}/>},
    {key:'queue',label:'渲染结果',children:report?<Typography.Paragraph>{report.frames+' 帧 · '+report.width+'×'+report.height+' · '+report.runtime_version}<br/>{report.sha256}</Typography.Paragraph>:<Typography.Text type="secondary">本地渲染完成后显示文件指纹；运行期间可查看按钮旁状态。</Typography.Text>}
   ]}/>
   <Flex vertical gap={8} className="draw-handoff">
    <Button block icon={<ImportOutlined aria-hidden="true"/>} disabled={!canUse||!selected||dirty||mediaMissing} onClick={()=>run('导入编辑时间线',importVideo)}>导入编辑时间线</Button>
    <Collapse ghost size="small" items={[{key:'agent',label:'Agent 绘制任务',children:<Flex vertical gap={8}><Input.TextArea aria-label="Agent 绘制目标" rows={2} value={goal} onChange={event=>setGoal(event.target.value)} placeholder="描述画面或需要修改的内容"/><Button block disabled={!canUse||!selected} onClick={()=>run('创建 Agent 绘制任务',createRun)}>新建 Agent 绘制任务</Button><Typography.Text type="secondary">Agent 可生成源码、检查实际帧，再导入编辑。</Typography.Text></Flex>}]}/>
    <Typography.Paragraph className="draw-feedback" role="status" aria-live="polite">{notice||'保存 → 检查 → 渲染 → 导入编辑'}</Typography.Paragraph>
   </Flex>
  </section>
 </div>;
}
