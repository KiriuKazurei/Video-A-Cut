import {useState} from 'react';
import {useMutation,useQuery,useQueryClient} from '@tanstack/react-query';
import {api} from '../api';
import type {ProcessingProfile} from '../preparation';
import {ProviderEditor} from './ProviderEditor';
import {Alert,Button,Card,Col,Flex,Form,Input,InputNumber,List,Row,Space,Tag,Typography,theme} from 'antd';
import {CheckCircleOutlined,ExclamationCircleOutlined,LockOutlined,QuestionCircleOutlined} from '@ant-design/icons';
import type {WorkflowRun} from '../types';
import {ValueSelect} from './ui';

const initial:ProcessingProfile={schema_version:1,profile_id:'local_regression',revision:0,name:'本地工程回归',content_mode:'builtin',vision:{adapter:'builtin'},narration:{adapter:'builtin'},sampling:{max_frames:12,max_bytes:12582912,timeout_seconds:60},tts_voice:'Microsoft Huihui Desktop',export_target:'premiere'};
const externalEndpoint=(endpoint?:string)=>{if(!endpoint)return false;try{return !['localhost','127.0.0.1','[::1]'].includes(new URL(endpoint).hostname)}catch{return false}};
const checkLabel={passed:'已满足',not_checked:'未检查',blocked:'受阻'} as const;

/** 准备页：处理预设版本、服务商诊断、外发授权、固定版本预检与启动（API-17~21、23~26）。 */
export function PreparationPanel({assetId,onStarted}:{assetId:string;onStarted?:(run:WorkflowRun)=>void}){
  const client=useQueryClient();
  const {token}=theme.useToken();
  const [draft,setDraft]=useState<ProcessingProfile>(initial);
  const [selected,setSelected]=useState('');
  const [version,setVersion]=useState(0);
  const [requested,setRequested]=useState(false);
  const [error,setError]=useState('');
  const [notice,setNotice]=useState('');
  const profiles=useQuery({queryKey:['processing-profiles'],queryFn:api.listProfiles});
  const report=useQuery({queryKey:['prepared',assetId,draft.profile_id,draft.revision],queryFn:()=>api.preparedPreflight(assetId,draft.profile_id,draft.revision),enabled:requested&&draft.revision>0,refetchInterval:requested?15000:false});
  const refresh=async()=>{await Promise.all(['processing-profiles','prepared','workflows','assets','audit'].map(k=>client.invalidateQueries({queryKey:[k]})))};
  const action=useMutation({mutationFn:(fn:()=>Promise<unknown>)=>fn(),onSuccess:async()=>{setError('');await refresh()},onError:e=>{setError(e instanceof Error?e.message:'操作失败');void refresh()}});
  const busy=action.isPending;
  const unsaved=notice.startsWith('有未保存');
  const canConsent=draft.revision>0&&!unsaved&&(externalEndpoint(draft.vision.endpoint)||externalEndpoint(draft.narration.endpoint));
  const edit=(next:ProcessingProfile)=>{setDraft(next);setRequested(false);setNotice('有未保存修改，请先保存新版本')};
  const changeMode=(mode:'builtin'|'configured')=>edit({...draft,content_mode:mode,vision:mode==='builtin'?{adapter:'builtin'}:{adapter:'vision',api_format:'openai',endpoint:'https://api.openai.com/v1',model:'',token_env:'VAC_VISION_TOKEN'},narration:mode==='builtin'?{adapter:'builtin'}:{adapter:'narration',api_format:'openai',endpoint:'https://api.openai.com/v1',model:'',token_env:'VAC_NARRATION_TOKEN'}});
  const sampling=(patch:Partial<ProcessingProfile['sampling']>)=>edit({...draft,sampling:{...draft.sampling,...patch}});
  const save=()=>action.mutate(async()=>{const saved=await api.saveProfile({...draft,revision:draft.revision+1},draft.revision);setDraft(saved);setSelected(saved.profile_id);setRequested(false);setNotice(`已保存 v${saved.revision}`)});
  const fetchError=error||(profiles.error instanceof Error?profiles.error.message:report.error instanceof Error?report.error.message:profiles.error||report.error?'读取失败':'');
  return <Card className="preparation-panel" aria-labelledby="preparation-title" title={<span id="preparation-title">处理预设与运行准备</span>}
    extra={<Tag color={draft.revision>0?'blue':'default'}>{draft.profile_id} · {draft.revision>0?`v${draft.revision}`:'未保存'}</Tag>}>
    <Flex vertical gap={12}>
      <Typography.Text type="secondary">保存预设后检查实际 Worker 能力，再启动固定版本。工程回归仍需人工内容验收。</Typography.Text>
      <Flex gap={8} align="flex-end" wrap className="profile-picker">
        <Form.Item label="已保存预设" layout="vertical" style={{marginBottom:0,flex:'1 1 240px',minWidth:0}}>
          <ValueSelect<string> className="select-saved-profile" aria-label="已保存预设" value={selected} onChange={v=>{setSelected(v);setVersion(0)}}
            options={[{value:'',label:'选择预设'},...(profiles.data??[]).map(p=>({value:p.profile_id,label:`${p.name} · v${p.revision}`}))]}/>
        </Form.Item>
        <Form.Item label="历史版本（0 为当前）" layout="vertical" style={{marginBottom:0,width:150}}>
          <InputNumber aria-label="历史版本" style={{width:'100%'}} min={0} step={1} precision={0} value={version} onChange={v=>setVersion(Number(v??0))}/>
        </Form.Item>
        <Button disabled={busy||!selected} onClick={()=>action.mutate(async()=>{const p=await api.getProfile(selected,version);setDraft(p.profile);setRequested(false);setNotice(`已加载 ${p.profile.profile_id} / v${p.profile.revision}`)})}>加载版本</Button>
        <Button disabled={busy} onClick={()=>{setDraft({...initial,profile_id:'profile_'+Date.now()});setRequested(false);setNotice('新预设尚未保存')}}>新建预设</Button>
      </Flex>
      {fetchError&&<Alert type="error" showIcon role="alert" message={fetchError}/>}
      {busy&&<Typography.Text type="secondary" role="status">正在处理…</Typography.Text>}
      {notice&&<Alert type={unsaved?'warning':'info'} showIcon role="status" message={notice}/>}
      <Row gutter={[16,16]}>
        <Col xs={24} xl={16}>
          <Form layout="vertical" className="prep-main" onFinish={save} disabled={busy}>
            <Typography.Title level={5}>基本信息</Typography.Title>
            <Row gutter={12}>
              <Col xs={24} md={8}><Form.Item label="预设 ID"><Input required maxLength={64} pattern="[A-Za-z0-9_-]+" disabled={draft.revision>0||busy} value={draft.profile_id} onChange={e=>edit({...draft,profile_id:e.target.value})}/></Form.Item></Col>
              <Col xs={24} md={8}><Form.Item label="名称"><Input required maxLength={100} value={draft.name} onChange={e=>edit({...draft,name:e.target.value})}/></Form.Item></Col>
              <Col xs={24} md={8}><Form.Item label="内容模式"><ValueSelect<'builtin'|'configured'> className="select-content-mode" value={draft.content_mode} onChange={changeMode}
                options={[{value:'builtin',label:'builtin 工程回归'},{value:'configured',label:'已配置模型'}]}/></Form.Item></Col>
            </Row>
            {draft.content_mode==='configured'?<Flex vertical gap={12} style={{marginBottom:16}}>{(['vision','narration'] as const).map(key=><ProviderEditor key={key} role={key} provider={draft[key]} disabled={busy} onChange={provider=>edit({...draft,[key]:provider})}/>)}</Flex>
              :<Alert type="info" style={{marginBottom:16}} message="builtin 模式不调用任何服务商；切换为“已配置模型”后可设置画面识别与中文解说服务。"/>}
            <Typography.Title level={5}>取样与语音</Typography.Title>
            <Row gutter={12}>
              <Col xs={12} md={6}><Form.Item label="单任务帧数"><InputNumber required style={{width:'100%'}} min={1} max={48} value={draft.sampling.max_frames} onChange={v=>sampling({max_frames:Number(v??0)})}/></Form.Item></Col>
              <Col xs={12} md={6}><Form.Item label="取样预算（MiB）"><InputNumber required style={{width:'100%'}} min={1} max={48} value={draft.sampling.max_bytes/1048576} onChange={v=>sampling({max_bytes:Number(v??0)*1048576})}/></Form.Item></Col>
              <Col xs={12} md={6}><Form.Item label="取样总期限（秒）"><InputNumber required style={{width:'100%'}} min={1} max={120} value={draft.sampling.timeout_seconds} onChange={v=>sampling({timeout_seconds:Number(v??0)})}/></Form.Item></Col>
              <Col xs={12} md={6}><Form.Item label="SAPI 声音名称"><Input required value={draft.tts_voice} onChange={e=>edit({...draft,tts_voice:e.target.value})}/></Form.Item></Col>
            </Row>
            <Space wrap><Button type="primary" htmlType="submit">保存新版本</Button><Typography.Text type="secondary">已保存版本不可覆盖，每次保存生成 v{draft.revision+1}。</Typography.Text></Space>
          </Form>
        </Col>
        <Col xs={24} xl={8}>
          <Card size="small" className="prep-side" title="运行条件" aria-label="运行条件">
            <Flex vertical gap={10}>
              <Typography.Text type="secondary">以服务端预检的 can_start 与逐项检查为准；阻断也会以 HTTP 200 返回。</Typography.Text>
              <Button block disabled={busy||draft.revision<1||unsaved} onClick={()=>{setRequested(true);void report.refetch()}}>检查运行条件</Button>
              <Button block disabled={busy||!canConsent} onClick={()=>action.mutate(()=>api.profileConsent(draft.profile_id,draft.revision,true))}>授权此版本向外部服务发送取样</Button>
              <Button block danger disabled={busy||!canConsent} onClick={()=>action.mutate(()=>api.profileConsent(draft.profile_id,draft.revision,false))}>撤销此版本外发许可</Button>
              {report.isFetching&&<Typography.Text type="secondary" role="status">正在核对环境和资产…</Typography.Text>}
              {!requested&&<Typography.Text type="secondary">保存或加载一个预设版本后，点击“检查运行条件”。</Typography.Text>}
              {requested&&report.data&&<>
                <Alert type={report.data.can_start?'success':'warning'} showIcon message={`预设 v${report.data.profile_revision}：${report.data.can_start?'运行条件满足':'尚不可运行'}`}/>
                <List size="small" className="check-list" dataSource={report.data.checks} renderItem={c=><List.Item key={c.code} className={`check-${c.status}`}>
                  <List.Item.Meta avatar={c.status==='passed'?<CheckCircleOutlined style={{color:token.colorSuccess}}/>:c.status==='blocked'?<LockOutlined style={{color:token.colorWarning}}/>:c.status==='not_checked'?<QuestionCircleOutlined/>:<ExclamationCircleOutlined/>}
                    title={<span style={{fontWeight:400}}>{checkLabel[c.status]??c.status}：{c.message}</span>} description={<Typography.Text code style={{fontSize:12}}>{c.code}</Typography.Text>}/>
                </List.Item>}/>
                <Button type="primary" block disabled={busy||report.isFetching||!report.data.can_start} onClick={()=>action.mutate(async()=>{const run=await api.startPrepared(assetId,draft.profile_id,draft.revision,report.data!.expected_asset_version!);setRequested(false);setNotice('已启动固定预设流程，请到「编辑」页审查证据和草稿');onStarted?.(run)})}>启动此版本流程</Button>
              </>}
            </Flex>
          </Card>
        </Col>
      </Row>
    </Flex>
  </Card>;
}
