import { useEffect, useState } from 'react';
import { Alert, Button, Flex, InputNumber, Select, Typography } from 'antd';
import type { RawDevelop } from '../projectContracts';
import { useProjectWorkspace } from '../projectWorkspace';
import { errorText } from './ui';

const defaults: RawDevelop = {exposure:0,white_balance:'camera',contrast:1,saturation:1};
export function RawDevelopControls() {
 const project=useProjectWorkspace(),resource=project.resource;
 const [draft,setDraft]=useState<RawDevelop>(resource?.raw_develop??defaults);
 const [busy,setBusy]=useState(false),[error,setError]=useState('');
 const saved=JSON.stringify(resource?.raw_develop??defaults),dirty=JSON.stringify(draft)!==saved;
 useEffect(()=>{setDraft(resource?.raw_develop??defaults);setError('');},[resource?.id,saved]);
 const apply=async()=>{if(!resource||!dirty)return;setBusy(true);setError('');try{await project.patchResource(resource.id,{raw_develop:draft});}catch(e){setError(errorText(e));throw e;}finally{setBusy(false);}};
 useEffect(()=>{if(!resource?.raw_develop)return;return project.registerEditor('raw:'+resource.id,{dirty,flush:apply});},[resource?.id,dirty,draft,saved]);
 if(!resource?.raw_develop)return null;
 const patch=(value:Partial<RawDevelop>)=>setDraft(old=>({...old,...value}));
 return <Flex vertical gap={8} className="raw-develop-controls" aria-label="RAW 调整">
  <Typography.Text strong>RAW 调整</Typography.Text>
  <Flex gap={12} wrap align="center">
   <label>曝光 EV <InputNumber aria-label="RAW 曝光 EV" min={-2} max={3} step={.1} value={draft.exposure} disabled={busy} onChange={v=>{if(v!==null)patch({exposure:v});}}/></label>
   <label>白平衡 <Select aria-label="RAW 白平衡" value={draft.white_balance} disabled={busy} onChange={white_balance=>patch({white_balance})} options={[{value:'camera',label:'相机原设'},{value:'auto',label:'自动'},{value:'daylight',label:'日光'}]} style={{width:120}}/></label>
   <label>对比度 <InputNumber aria-label="RAW 对比度" min={.5} max={2} step={.05} value={draft.contrast} disabled={busy} onChange={v=>{if(v!==null)patch({contrast:v});}}/></label>
   <label>饱和度 <InputNumber aria-label="RAW 饱和度" min={0} max={2} step={.05} value={draft.saturation} disabled={busy} onChange={v=>{if(v!==null)patch({saturation:v});}}/></label>
   <Button type="primary" loading={busy} disabled={!dirty||resource.missing||resource.changed} onClick={()=>void apply().catch(()=>{})}>应用 RAW 调整</Button>
   <Button disabled={busy} onClick={()=>setDraft({...defaults})}>重置参数</Button>
  </Flex>
  <Typography.Text type="secondary">应用后在本机重新显影，预览、绘制和时间线使用同一结果；原文件保持不变。参数随工程保存。</Typography.Text>
  {error&&<Alert type="error" message={error}/>}
 </Flex>;
}
