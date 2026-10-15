import {useEffect,useRef,useState} from 'react';
import {useMutation,useQuery,useQueryClient} from '@tanstack/react-query';
import {api} from '../api';
import type {ProcessingProfile} from '../preparation';
import {ProviderEditor} from './ProviderEditor';
import {TtsTemplateBindingEditor} from './TtsTemplateBindingEditor';
import {Alert,Button,Card,Col,Collapse,Flex,Form,Input,InputNumber,List,Row,Space,Tag,Tooltip,Typography,theme} from 'antd';
import {CheckCircleOutlined,ExclamationCircleOutlined,LockOutlined,QuestionCircleOutlined} from '@ant-design/icons';
import type {WorkflowRun} from '../types';
import type {RouteState} from '../navigation';
import {ValueSelect} from './ui';

const initial:ProcessingProfile={schema_version:1,profile_id:'local_regression',revision:0,name:'本地工程回归',content_mode:'builtin',vision:{adapter:'builtin'},narration:{adapter:'builtin'},sampling:{max_frames:12,max_bytes:12582912,timeout_seconds:60},tts_voice:'Microsoft Huihui Desktop',export_target:'premiere'};
const externalEndpoint=(endpoint?:string)=>{if(!endpoint)return false;try{return !['localhost','127.0.0.1','[::1]'].includes(new URL(endpoint).hostname)}catch{return false}};
const checkLabel={passed:'已满足',not_checked:'未检查',blocked:'受阻'} as const;

/** 准备页：处理预设版本、服务商诊断、外发授权、固定版本预检与启动（API-17~21、23~26）。 */
export function PreparationPanel({
  assetId,
  onStarted,
  activeSection = 'all',
  onNavigate
}: {
  assetId: string;
  onStarted?: (run: WorkflowRun) => void;
  activeSection?: 'profiles' | 'providers' | 'preparation' | 'all' | 'quickstart';
  /** 跳到修复页（资源治理 / 服务商 / 预设保存等），由设置工作区注入。 */
  onNavigate?: (partial: Partial<RouteState>) => void;
}) {
  const client=useQueryClient();
  const {token}=theme.useToken();
  const [draft,setDraft]=useState<ProcessingProfile>(initial);
  const [selected,setSelected]=useState('');
  const [version,setVersion]=useState(0);
  const [error,setError]=useState('');
  const [notice,setNotice]=useState('');
  const profiles=useQuery({queryKey:['processing-profiles'],queryFn:api.listProfiles});
  const unsaved=notice.startsWith('有未保存');
  const autoCheck=Boolean(assetId)&&draft.revision>0&&!unsaved;
  const report=useQuery({
    queryKey:['prepared',assetId,draft.profile_id,draft.revision],
    queryFn:()=>api.preparedPreflight(assetId,draft.profile_id,draft.revision),
    enabled:autoCheck,
    refetchInterval:autoCheck?15000:false
  });
  // 进入运行准备分区或资产变化时主动刷新预检（治理状态可能已变）
  const prepFocus=activeSection==='preparation'||activeSection==='all';
  useEffect(()=>{if(prepFocus&&autoCheck)void report.refetch()},[prepFocus,autoCheck,assetId,draft.profile_id,draft.revision]);
  // quickstart（编辑页「开始生成」入口）：卡片上看到的预设就是启动时用的预设。
  // 1) 没选预设时自动载入第一个已保存预设；2) 下拉一变立刻载入该预设当前版本；3) 预设列表里该预设的版本号变了（例如在设置页保存了 v2）就重新载入。
  const quick=activeSection==='quickstart';
  const loadSeq=useRef(0);
  const inflight=useRef(false);
  const failedKey=useRef('');          // 上次失败的「预设@列表版本」；同一个键不再自动重试，避免失败后无限重试
  const [loading,setLoading]=useState(false);
  const [loadError,setLoadError]=useState('');
  const loadDraft=(id:string,rev:number)=>{
    const seq=++loadSeq.current;
    inflight.current=true;
    setLoading(true);setLoadError('');
    return api.getProfile(id,rev).then(p=>{if(seq!==loadSeq.current)return;failedKey.current='';setDraft(p.profile);setSelected(p.profile.profile_id);setError('');setNotice('')})
      .catch(e=>{if(seq===loadSeq.current)setLoadError(e instanceof Error?e.message:'读取预设失败')})
      .finally(()=>{if(seq===loadSeq.current){inflight.current=false;setLoading(false)}});
  };
  const autoLoaded=useRef(false);
  useEffect(()=>{
    if(!quick||autoLoaded.current||draft.revision>0||!profiles.data?.length)return;
    autoLoaded.current=true;
    setSelected(profiles.data[0].profile_id);
    void loadDraft(profiles.data[0].profile_id,0);
  },[quick,draft.revision,profiles.data]);
  const listedRevision=profiles.data?.find(p=>p.profile_id===draft.profile_id)?.revision;
  // 列表里该预设的版本与卡片 draft 不同 → 重新载入。用 ref 判断是否在载入、记住失败的版本，
  // 依赖里不放 loading：失败后 loading 翻转不会再触发这里（否则会每帧重试）。
  useEffect(()=>{
    if(!quick||inflight.current||draft.revision<1||listedRevision===undefined||listedRevision===draft.revision)return;
    if(selected!==draft.profile_id)return;                 // 用户刚换了预设，由选择器自己载入
    if(listedRevision<draft.revision){void profiles.refetch();return}  // 列表比 draft 旧：刷新列表，不反复载入
    const key=`${draft.profile_id}@${listedRevision}`;
    if(failedKey.current===key)return;                      // 这个版本已经失败过：等列表版本/所选预设变化或手动重试
    failedKey.current=key;
    void loadDraft(draft.profile_id,0);
  },[quick,draft.profile_id,draft.revision,listedRevision,selected]);
  const retryLoad=()=>{const id=selected||draft.profile_id;if(!id)return;failedKey.current='';void loadDraft(id,0)};
  // 安全网：选择器显示的预设/版本必须与已载入的 draft 一致，否则不允许开始
  const picked=profiles.data?.find(p=>p.profile_id===selected);
  const pickerMismatch=quick&&(loading||Boolean(loadError)||selected!==draft.profile_id||(picked!==undefined&&picked.revision!==draft.revision));
  const refresh=async()=>{await Promise.all(['processing-profiles','prepared','workflows','assets','audit'].map(k=>client.invalidateQueries({queryKey:[k]})))};
  const action=useMutation({mutationFn:(fn:()=>Promise<unknown>)=>fn(),onSuccess:async()=>{setError('');await refresh()},onError:e=>{setError(e instanceof Error?e.message:'操作失败');void refresh()}});
  const busy=action.isPending;
  const canConsent=draft.revision>0&&!unsaved&&(externalEndpoint(draft.vision.endpoint)||externalEndpoint(draft.narration.endpoint)||externalEndpoint(draft.tts_template?.settings?.endpoint));
  const edit=(next:ProcessingProfile)=>{setDraft(next);setNotice('有未保存修改，请先保存新版本')};
  const changeMode=(mode:'builtin'|'configured')=>edit({...draft,content_mode:mode,vision:mode==='builtin'?{adapter:'builtin'}:{adapter:'vision',api_format:'openai',endpoint:'https://api.openai.com/v1',model:'',token_env:'VAC_VISION_TOKEN'},narration:mode==='builtin'?{adapter:'builtin'}:{adapter:'narration',api_format:'openai',endpoint:'https://api.openai.com/v1',model:'',token_env:'VAC_NARRATION_TOKEN'}});
  const sampling=(patch:Partial<ProcessingProfile['sampling']>)=>edit({...draft,sampling:{...draft.sampling,...patch}});
  const save=()=>action.mutate(async()=>{const saved=await api.saveProfile({...draft,revision:draft.revision+1},draft.revision);setDraft(saved);setSelected(saved.profile_id);setNotice(`已保存 v${saved.revision}`)});
  const fetchError=error||(profiles.error instanceof Error?profiles.error.message:report.error instanceof Error?report.error.message:profiles.error||report.error?'读取失败':'');
  const starting=useRef(false);  // 同步锁：同一拍内的双击只发一次启动请求（busy 状态要等下一次渲染才生效）
  const startFlow=()=>{
    if(starting.current)return;
    starting.current=true;
    action.mutate(async()=>{
      try{
        const run=await api.startPrepared(assetId,draft.profile_id,draft.revision,report.data!.expected_asset_version!);
        setNotice('已开始生成。请到「编辑」页确认场景并批准解说。');
        onStarted?.(run);
      }finally{starting.current=false}
    });
  };
  const goSave=()=>{
    if(activeSection==='all'){
      document.querySelector<HTMLElement>('[data-nav-anchor="profile-save"]')?.focus();
      document.querySelector<HTMLElement>('[data-nav-anchor="profile-save"]')?.scrollIntoView({block:'center',behavior:'smooth'});
      return;
    }
    onNavigate?.({workspace:'settings',settingsCategory:'profiles'});
    // 分区切换后聚焦保存
    requestAnimationFrame(()=>{
      setTimeout(()=>{
        const el=document.querySelector<HTMLElement>('[data-nav-anchor="profile-save"]');
        el?.scrollIntoView({block:'center',behavior:'smooth'});
        el?.focus();
      },80);
    });
  };
  const blockers=report.data?.checks.filter(c=>c.status==='blocked')??[];
  const blockerTip=blockers.slice(0,3).map(c=>c.message).join('；')||'运行条件未满足';
  const hasGovBlock=blockers.some(c=>c.code==='asset_governance'||c.code.startsWith('role_'));
  const hasConsentBlock=blockers.some(c=>c.code==='external_authorization');
  const hasWorkerBlock=blockers.some(c=>c.code.startsWith('worker_'));
  const hasIngestBlock=blockers.some(c=>c.code.startsWith('ingest_')||c.code==='input_ready'||c.code==='controlled_edl');

  // 顶层公共预设选择工具栏
  const quickPicker = (
    <Flex gap={8} align="flex-end" wrap className="profile-picker profile-picker-quick">
      <Form.Item label="处理预设" layout="vertical" style={{marginBottom:0,flex:'1 1 240px',minWidth:0}}>
        <ValueSelect<string> className="select-saved-profile" aria-label="已保存预设" value={selected} loading={loading} disabled={busy}
          onChange={v=>{setSelected(v);setVersion(0);if(v)void loadDraft(v,0)}}
          options={[{value:'',label:'选择预设'},...(profiles.data??[]).map(p=>({value:p.profile_id,label:`${p.name} · v${p.revision}`}))]}/>
      </Form.Item>
      <Button type="link" className="quickstart-advanced" onClick={()=>onNavigate?.({workspace:'settings',settingsCategory:'profiles'})}>高级：到设置页管理预设</Button>
    </Flex>
  );
  const profilePicker = quick ? quickPicker : (
    <Flex gap={8} align="flex-end" wrap className="profile-picker">
      <Form.Item label="已保存预设" layout="vertical" style={{marginBottom:0,flex:'1 1 240px',minWidth:0}}>
        <ValueSelect<string> className="select-saved-profile" aria-label="已保存预设" value={selected} onChange={v=>{setSelected(v);setVersion(0)}}
          options={[{value:'',label:'选择预设'},...(profiles.data??[]).map(p=>({value:p.profile_id,label:`${p.name} · v${p.revision}`}))]}/>
      </Form.Item>
      <Form.Item label="历史版本（0 为当前）" layout="vertical" style={{marginBottom:0,width:150}}>
        <InputNumber aria-label="历史版本" style={{width:'100%'}} min={0} step={1} precision={0} value={version} onChange={v=>setVersion(Number(v??0))}/>
      </Form.Item>
      <Button disabled={busy||!selected} onClick={()=>action.mutate(async()=>{const p=await api.getProfile(selected,version);setDraft(p.profile);setNotice(`已加载 ${p.profile.profile_id} / v${p.profile.revision}`)})}>加载版本</Button>
      <Button disabled={busy} onClick={()=>{setDraft({...initial,profile_id:'profile_'+Date.now()});setNotice('新预设尚未保存')}}>新建预设</Button>
    </Flex>
  );

  const saveButtons=(
    <Space wrap>
      <Button type="primary" className="vac-cta" htmlType="submit" data-nav-anchor="profile-save">保存新版本</Button>
      <Typography.Text type="secondary">已保存版本不可覆盖，每次保存生成 v{draft.revision+1}。</Typography.Text>
    </Space>
  );

  // 一步式运行准备：自动预检 + 单一就绪卡片 + 主 CTA
  const readinessCard = (
    <Card size="small" className="prep-readiness" title={<span>下一步：检查并开始 <Tooltip title="「运行准备」就是开跑前的自动检查：素材是否对 Agent 可见、预设是否保存、所需服务是否就绪。"><QuestionCircleOutlined style={{fontSize:12,opacity:.65}}/></Tooltip></span>} aria-label="运行条件" data-nav-anchor="prepare-start" tabIndex={-1}
      extra={autoCheck?<Button size="small" disabled={busy||report.isFetching} onClick={()=>void report.refetch()} loading={report.isFetching&&!!report.data}>重新检查</Button>:null}>
      <Flex vertical gap={12} className="prep-readiness-body">
        <Typography.Text type="secondary">选好预设后会自动检查能否开始。条件满足时，点下方按钮即可启动。</Typography.Text>
        {!assetId&&<Alert type="info" showIcon message="请先在左侧项目面板选择资产，再核对运行条件。"/>}
        {assetId&&draft.revision<1&&!unsaved&&<Alert type="info" showIcon message="请先加载或保存一个预设版本，将自动检查运行条件。"/>}
        {unsaved&&(
          <Alert type="warning" showIcon message="有未保存修改"
            description="自动检查（预检）只对已保存的版本生效。请先保存新版本，再自动核对运行条件。"
            action={<Button type="primary" size="small" className="vac-cta" onClick={goSave}>先保存新版本</Button>}/>
        )}
        {autoCheck&&report.isFetching&&!report.data&&<Typography.Text type="secondary" role="status">正在核对环境和资产…</Typography.Text>}
        {autoCheck&&report.data&&<>
          <Alert
            type={pickerMismatch?'info':report.data.can_start?'success':'warning'}
            showIcon
            message={pickerMismatch?(loadError?'暂时无法确认所选预设版本':'正在载入所选预设…'):report.data.can_start?'可以开始生成':'还不能开始'}
            description={pickerMismatch?(loadError?'请先处理上方的读取失败提示，再开始生成。':'载入完成并重新检查后才能开始，确保启动的就是上面选中的预设版本。'):!report.data.can_start&&blockers.length?`还差：${blockerTip}`:report.data.can_start?'点击下方按钮开始。生成过程可在「编辑」页查看。':undefined}
          />
          <Collapse size="small" defaultActiveKey={report.data.can_start ? [] : ['checks']} items={[{ key: 'checks', label: '查看检查明细', children:
            <List size="small" className="check-list" dataSource={report.data.checks} renderItem={c=><List.Item key={c.code} className={`check-${c.status}`}>
              <List.Item.Meta avatar={c.status==='passed'?<CheckCircleOutlined style={{color:token.colorSuccess}}/>:c.status==='blocked'?<LockOutlined style={{color:token.colorWarning}}/>:c.status==='not_checked'?<QuestionCircleOutlined/>:<ExclamationCircleOutlined/>}
                title={<span style={{fontWeight:400}}>{checkLabel[c.status]??c.status}：{c.message}</span>}/>
            </List.Item>}/>
          }]} />
          {!report.data.can_start&&(
            <Space wrap size={[8,8]}>
              {hasGovBlock&&<Button onClick={()=>onNavigate?.({workspace:'resources'})}>去资源管理调整可见性/角色</Button>}
              {hasConsentBlock&&canConsent&&<Button onClick={()=>action.mutate(()=>api.profileConsent(draft.profile_id,draft.revision,true))}>授权此版本向外部服务发送取样</Button>}
              {hasWorkerBlock&&<Button onClick={()=>onNavigate?.({workspace:'settings',settingsCategory:'providers'})}>查看服务商接入</Button>}
              {hasIngestBlock&&<Button onClick={()=>onNavigate?.({workspace:'editing',editingSection:'import'})}>去导入与粗剪</Button>}
            </Space>
          )}
          <Tooltip title={pickerMismatch?'正在载入所选预设的最新版本，请稍候':report.data.can_start?undefined:blockerTip}>
            <span style={{display:'block',width:'100%'}}>
              <Button type="primary" block className="vac-cta" disabled={busy||report.isFetching||!report.data.can_start||unsaved||pickerMismatch} onClick={startFlow}>开始生成</Button>
            </span>
          </Tooltip>
        </>}
        <Space wrap>
          <Button disabled={busy||!canConsent} onClick={()=>action.mutate(()=>api.profileConsent(draft.profile_id,draft.revision,true))}>授权此版本向外部服务发送取样</Button>
          <Button danger disabled={busy||!canConsent} onClick={()=>action.mutate(()=>api.profileConsent(draft.profile_id,draft.revision,false))}>撤销此版本外发许可</Button>
        </Space>
        <Typography.Text type="secondary" style={{fontSize:12}}>外发授权为可选项：仅当预设指向外部端点时需要；不阻塞 builtin 工程回归。</Typography.Text>
        <Typography.Text type="secondary" style={{fontSize:12}}>「外发授权」＝ 同意把取样画面/文字发给你配置的外部模型服务；不授权就不会外发。</Typography.Text>
      </Flex>
    </Card>
  );

  if(activeSection==='quickstart')return <Flex vertical gap={12} className="quickstart-panel" data-nav-anchor="quickstart" tabIndex={-1}>
    {profilePicker}
    {loadError&&<Alert type="error" showIcon role="alert" className="quickstart-load-error" message={`读取预设失败：${loadError}`}
      description="已暂停自动重试，「开始生成」保持禁用。换一个预设、预设版本更新，或点右侧「重试」后会再次载入。"
      action={<Button type="link" size="small" onClick={retryLoad} disabled={loading}>重试</Button>}/>}
    {fetchError&&<Alert type="error" showIcon role="alert" message={fetchError}/>}
    {readinessCard}
  </Flex>;

  const titleText = activeSection === 'profiles' ? '处理预设' : activeSection === 'providers' ? '服务商接入' : activeSection === 'preparation' ? '运行准备' : '处理预设与运行准备';

  return <Card className="preparation-panel" aria-labelledby="preparation-title" title={<span id="preparation-title">{titleText}</span>}
    extra={<Tag color={draft.revision>0?'blue':'default'}>{draft.profile_id} · {draft.revision>0?`v${draft.revision}`:'未保存'}</Tag>}>
    <Flex vertical gap={12}>
      <Typography.Text type="secondary">
        {activeSection === 'profiles' && '配置预设基础参数、取样与声音，保存为不可变版本。'}
        {activeSection === 'providers' && '为当前预设配置画面识别与中文解说模型服务商。'}
        {activeSection === 'preparation' && (assetId ? '确认条件后即可开始生成。' : '请先在左侧选择要处理的素材。')}
        {activeSection === 'all' && '保存预设后会自动检查能否运行；开始生成后请在编辑页确认内容。'}
      </Typography.Text>
      
      {profilePicker}
      {(activeSection === 'profiles' || activeSection === 'all') && <TtsTemplateBindingEditor binding={draft.tts_template} onChange={binding=>edit({...draft,tts_template:binding})}/>}
      {fetchError&&<Alert type="error" showIcon role="alert" message={fetchError}/>}
      {busy&&<Typography.Text type="secondary" role="status">正在处理…</Typography.Text>}
      {notice&&!unsaved&&<Alert type="info" showIcon role="status" message={notice}/>}
      {unsaved&&activeSection!=='preparation'&&<Alert type="warning" showIcon role="status" message={notice}/>}

      {/* 1. profiles: 处理预设 */}
      {activeSection === 'profiles' && (
        <Form layout="vertical" className="prep-main" onFinish={save} disabled={busy}>
          <Typography.Title level={5}>基本信息</Typography.Title>
          <Row gutter={12}>
            <Col xs={24} md={8}><Form.Item label="预设 ID"><Input required maxLength={64} pattern="[A-Za-z0-9_-]+" disabled={draft.revision>0||busy} value={draft.profile_id} onChange={e=>edit({...draft,profile_id:e.target.value})}/></Form.Item></Col>
            <Col xs={24} md={8}><Form.Item label="名称"><Input required maxLength={100} value={draft.name} onChange={e=>edit({...draft,name:e.target.value})}/></Form.Item></Col>
            <Col xs={24} md={8}><Form.Item label="内容模式"><ValueSelect<'builtin'|'configured'> className="select-content-mode" value={draft.content_mode} onChange={changeMode}
              options={[{value:'builtin',label:'builtin 工程回归'},{value:'configured',label:'已配置模型'}]}/></Form.Item></Col>
          </Row>
          <Typography.Title level={5}>取样与语音</Typography.Title>
          <Row gutter={12}>
            <Col xs={12} md={6}><Form.Item label="单任务帧数"><InputNumber required style={{width:'100%'}} min={1} max={48} value={draft.sampling.max_frames} onChange={v=>sampling({max_frames:Number(v??0)})}/></Form.Item></Col>
            <Col xs={12} md={6}><Form.Item label="取样预算（MiB）"><InputNumber required style={{width:'100%'}} min={1} max={48} value={draft.sampling.max_bytes/1048576} onChange={v=>sampling({max_bytes:Number(v??0)*1048576})}/></Form.Item></Col>
            <Col xs={12} md={6}><Form.Item label="取样总期限（秒）"><InputNumber required style={{width:'100%'}} min={1} max={120} value={draft.sampling.timeout_seconds} onChange={v=>sampling({timeout_seconds:Number(v??0)})}/></Form.Item></Col>
            <Col xs={12} md={6}><Form.Item label="SAPI 声音名称"><Input required value={draft.tts_voice} onChange={e=>edit({...draft,tts_voice:e.target.value})}/></Form.Item></Col>
          </Row>
          {saveButtons}
        </Form>
      )}

      {/* 2. providers: 服务商接入 */}
      {activeSection === 'providers' && (
        <Form layout="vertical" className="prep-main" onFinish={save} disabled={busy}>
          <Typography.Title level={5}>模型服务商配置</Typography.Title>
          <div style={{marginBottom: 16}}>
            <Typography.Text type="secondary">当前内容模式：</Typography.Text>
            <Tag color={draft.content_mode==='configured'?'blue':'default'}>{draft.content_mode==='configured'?'已配置外部模型':'builtin 内部回归'}</Tag>
            {draft.content_mode==='builtin' && (
              <Button size="small" type="link" onClick={()=>changeMode('configured')}>切换为已配置模型以接入服务商</Button>
            )}
          </div>
          {draft.content_mode==='configured'?<Flex vertical gap={12} style={{marginBottom:16}}>{(['vision','narration'] as const).map(key=><ProviderEditor key={key} role={key} provider={draft[key]} disabled={busy} onChange={provider=>edit({...draft,[key]:provider})}/>)}</Flex>
            :<Alert type="info" style={{marginBottom:16}} message="builtin 模式不调用任何服务商；点击上方按钮切换为“已配置模型”后可配置画面识别与中文解说服务。"/>}
          <Space wrap><Button type="primary" className="vac-cta" htmlType="submit" data-nav-anchor="profile-save">保存新版本</Button><Typography.Text type="secondary">服务商修改从属于当前预设，保存生成 v{draft.revision+1}。</Typography.Text></Space>
        </Form>
      )}

      {/* 3. preparation: 运行准备 — 一步式就绪卡片为主 */}
      {activeSection === 'preparation' && (
        <Flex vertical gap={12}>
          <Card size="small" title="当前预设" bordered className="prep-summary">
            <Typography.Paragraph style={{marginBottom:4}}><strong>预设：</strong>{draft.name} ({draft.profile_id}) · {draft.revision>0?`v${draft.revision}`:'未保存'}</Typography.Paragraph>
            <Typography.Paragraph style={{marginBottom:4}}><strong>模式：</strong>{draft.content_mode === 'builtin' ? 'builtin 工程回归' : '已配置模型'}</Typography.Paragraph>
            <Typography.Paragraph style={{marginBottom:0}}><strong>取样帧数：</strong>{draft.sampling.max_frames} 帧 · 声音：{draft.tts_voice}</Typography.Paragraph>
          </Card>
          {readinessCard}
        </Flex>
      )}

      {/* 4. all: 完整并排展示（默认与向后兼容） */}
      {activeSection === 'all' && (
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
              {saveButtons}
            </Form>
          </Col>
          <Col xs={24} xl={8}>
            {readinessCard}
          </Col>
        </Row>
      )}
    </Flex>
  </Card>;
}
