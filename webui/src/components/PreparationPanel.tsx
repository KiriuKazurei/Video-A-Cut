import {useState} from 'react';
import {useMutation,useQuery,useQueryClient} from '@tanstack/react-query';
import {api} from '../api';
import type {ProcessingProfile} from '../preparation';
import {ProviderEditor} from './ProviderEditor';
import {Icon} from './Icon';

const initial:ProcessingProfile={schema_version:1,profile_id:'local_regression',revision:0,name:'本地工程回归',content_mode:'builtin',vision:{adapter:'builtin'},narration:{adapter:'builtin'},sampling:{max_frames:12,max_bytes:12582912,timeout_seconds:60},tts_voice:'Microsoft Huihui Desktop',export_target:'premiere'};
const externalEndpoint=(endpoint?:string)=>{if(!endpoint)return false;try{return !['localhost','127.0.0.1','[::1]'].includes(new URL(endpoint).hostname)}catch{return false}};
const checkLabel={passed:'已满足',not_checked:'未检查',blocked:'受阻'} as const;

/** 准备页：处理预设版本、服务商诊断、外发授权、固定版本预检与启动（API-17~26）。 */
export function PreparationPanel({assetId,onStarted}:{assetId:string;onStarted?:()=>void}){
  const client=useQueryClient();
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
  return <section className="panel preparation-panel" aria-labelledby="preparation-title">
    <div className="panel-heading"><div><p className="eyebrow">PREPARATION</p><h2 id="preparation-title">处理预设与运行准备</h2></div>
      <span className="pill">{draft.profile_id} · {draft.revision>0?`v${draft.revision}`:'未保存'}</span></div>
    <p className="hint">保存预设后检查实际 Worker 能力，再启动固定版本。工程回归仍需人工内容验收。</p>
    <div className="action-row profile-picker"><label className="field-label">已保存预设<select value={selected} onChange={e=>{setSelected(e.target.value);setVersion(0)}}><option value="">选择预设</option>{profiles.data?.map(p=><option key={p.profile_id} value={p.profile_id}>{p.name} · v{p.revision}</option>)}</select></label>
      <label className="field-label narrow">历史版本（0 为当前）<input type="number" min={0} step={1} value={version} onChange={e=>setVersion(Number(e.target.value))}/></label>
      <button disabled={busy||!selected} onClick={()=>action.mutate(async()=>{const p=await api.getProfile(selected,version);setDraft(p.profile);setRequested(false);setNotice(`已加载 ${p.profile.profile_id} / v${p.profile.revision}`)})}>加载版本</button>
      <button className="secondary-button" disabled={busy} onClick={()=>{setDraft({...initial,profile_id:'profile_'+Date.now()});setRequested(false);setNotice('新预设尚未保存')}}>新建预设</button></div>
    {(error||profiles.error||report.error)&&<p role="alert" className="inline-error">{error||(profiles.error instanceof Error?profiles.error.message:report.error instanceof Error?report.error.message:'读取失败')}</p>}
    {busy&&<p role="status">正在处理…</p>}{notice&&<p role="status" className={unsaved?'warn-text':'notice-text'}>{notice}</p>}
    <div className="prep-grid">
      <form className="prep-main" onSubmit={e=>{e.preventDefault();action.mutate(async()=>{const saved=await api.saveProfile({...draft,revision:draft.revision+1},draft.revision);setDraft(saved);setSelected(saved.profile_id);setRequested(false);setNotice(`已保存 v${saved.revision}`)})}}>
        <fieldset className="prep-group"><legend>基本信息</legend>
          <div className="action-row"><label className="field-label">预设 ID<input required maxLength={64} pattern="[A-Za-z0-9_-]+" disabled={draft.revision>0||busy} value={draft.profile_id} onChange={e=>edit({...draft,profile_id:e.target.value})}/></label><label className="field-label">名称<input required maxLength={100} disabled={busy} value={draft.name} onChange={e=>edit({...draft,name:e.target.value})}/></label><label className="field-label">内容模式<select disabled={busy} value={draft.content_mode} onChange={e=>changeMode(e.target.value as 'builtin'|'configured')}><option value="builtin">builtin 工程回归</option><option value="configured">已配置模型</option></select></label></div>
        </fieldset>
        {draft.content_mode==='configured'?(['vision','narration'] as const).map(key=><ProviderEditor key={key} role={key} provider={draft[key]} disabled={busy} onChange={provider=>edit({...draft,[key]:provider})}/>)
          :<p className="state">builtin 模式不调用任何服务商；切换为“已配置模型”后可设置画面识别与中文解说服务。</p>}
        <fieldset className="prep-group"><legend>取样与语音</legend>
          <div className="action-row"><label className="field-label narrow">单任务帧数<input required type="number" min={1} max={48} disabled={busy} value={draft.sampling.max_frames} onChange={e=>sampling({max_frames:Number(e.target.value)})}/></label><label className="field-label narrow">取样预算（MiB）<input required type="number" min={1} max={48} disabled={busy} value={draft.sampling.max_bytes/1048576} onChange={e=>sampling({max_bytes:Number(e.target.value)*1048576})}/></label><label className="field-label narrow">取样总期限（秒）<input required type="number" min={1} max={120} disabled={busy} value={draft.sampling.timeout_seconds} onChange={e=>sampling({timeout_seconds:Number(e.target.value)})}/></label><label className="field-label">SAPI 声音名称<input required disabled={busy} value={draft.tts_voice} onChange={e=>edit({...draft,tts_voice:e.target.value})}/></label></div>
        </fieldset>
        <div className="form-footer"><button disabled={busy}>保存新版本</button><span className="hint">已保存版本不可覆盖，每次保存生成 v{draft.revision+1}。</span></div>
      </form>
      <aside className="prep-side" aria-label="运行条件">
        <h3>运行条件</h3>
        <p className="hint">以服务端预检的 can_start 与逐项检查为准；阻断也会以 HTTP 200 返回。</p>
        <div className="prep-actions"><button disabled={busy||draft.revision<1||unsaved} onClick={()=>{setRequested(true);void report.refetch()}}>检查运行条件</button><button className="secondary-button" disabled={busy||!canConsent} onClick={()=>action.mutate(()=>api.profileConsent(draft.profile_id,draft.revision,true))}>授权此版本向外部服务发送取样</button><button className="secondary-button" disabled={busy||!canConsent} onClick={()=>action.mutate(()=>api.profileConsent(draft.profile_id,draft.revision,false))}>撤销此版本外发许可</button></div>
        {report.isFetching&&<p role="status">正在核对环境和资产…</p>}
        {!requested&&<p className="state">保存或加载一个预设版本后，点击“检查运行条件”。</p>}
        {requested&&report.data&&<>
          <p className={report.data.can_start?'ok-text':'warn-text'}><strong>预设 v{report.data.profile_revision}：{report.data.can_start?'运行条件满足':'尚不可运行'}</strong></p>
          <ul className="check-list">{report.data.checks.map(c=><li key={c.code} className={`check-${c.status}`}><Icon name={c.status==='passed'?'check':c.status==='blocked'?'lock':'eye'} />{checkLabel[c.status]??c.status}：{c.message}<small className="mono">{c.code}</small></li>)}</ul>
          <button className="primary-wide" disabled={busy||report.isFetching||!report.data.can_start} onClick={()=>action.mutate(async()=>{await api.startPrepared(assetId,draft.profile_id,draft.revision,report.data!.expected_asset_version!);setRequested(false);setNotice('已启动固定预设流程，请到「编辑」页审查证据和草稿');onStarted?.()})}>启动此版本流程</button>
        </>}
      </aside>
    </div>
  </section>;
}
