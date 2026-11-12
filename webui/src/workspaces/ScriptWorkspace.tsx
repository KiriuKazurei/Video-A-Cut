import { useState } from 'react';
import { Alert, Button, Empty, Select, Space, Typography } from 'antd';
import { projectApi } from '../projectApi';
import type { RouteState } from '../navigation';
import { useProjectWorkspace } from '../projectWorkspace';
import { ScriptWorkbench } from '../components/ScriptWorkbench';

export function ScriptWorkspace({ route, navigate, assetId, active, dock = false }: { route: RouteState; navigate: (route: Partial<RouteState>) => void; assetId?: string; active: boolean; dock?: boolean }) {
  const project = useProjectWorkspace();
  const [creating,setCreating]=useState(false),[failure,setFailure]=useState('');
  const resources = project.session?.document?.resources ?? [];
  const scripts=project.session?.document?.scripts ?? [];
  const selected=route.scriptId ?? scripts.at(-1)?.id;
  const scriptAsset = resources.find(resource => resource.script_id === selected)?.asset_id
    ?? resources.find(resource => resource.asset_id)?.asset_id ?? (!project.session?.document?assetId:undefined);
  return <div className={`page-body script-workspace${dock ? " script-workspace-docked" : ""}`} data-testid="script-dock">
    <div><Space wrap className="script-workspace-heading">
      {dock ? <Typography.Text strong>脚本</Typography.Text> : <Typography.Title level={1}>脚本</Typography.Title>}
      {!dock && <Typography.Text type="secondary">保存创作要求，再交给 Agent 按脚本剪辑</Typography.Text>}
      {scripts.length>0&&<Select aria-label="已保存脚本" value={selected} style={{minWidth:180}} onChange={scriptId=>void project.flushEditors().then(()=>navigate({workspace:'editing',editingSection:'content',scriptId})).catch(e=>setFailure(String(e)))} options={scripts.map(script=>({value:script.id,label:script.title||'未命名脚本'}))}/>}
      <Button type="primary" size={dock ? 'small' : 'middle'} loading={creating} disabled={!project.session?.document} onClick={()=>{
        setCreating(true);setFailure('');
        void project.flushEditors().then(()=>projectApi.createScript(project.session!.document!.project.id)).then(async view=>{await project.refresh();navigate({workspace:'editing',editingSection:'content',scriptId:view.document!.scripts.at(-1)!.id});}).catch(e=>setFailure(String(e))).finally(()=>setCreating(false));
      }}>新建脚本</Button>
    </Space>
    {failure&&<Alert type="error" message={failure}/>}
    {scriptAsset ? <ScriptWorkbench key={scriptAsset} assetId={scriptAsset} selectedScriptId={selected} navigate={navigate} active={active} variant={dock ? "dock" : "page"} readOnly={creating} />
      : <Empty description={project.session?.document?'点击「新建脚本」开始，无需先导入素材':'请先新建或打开工程'}/>}
    </div>
  </div>;
}
