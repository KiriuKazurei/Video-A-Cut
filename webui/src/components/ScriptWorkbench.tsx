import { useEffect, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, Button, Card, Checkbox, Collapse, Flex, Input, InputNumber, Modal, Popconfirm, Select, Space, Spin, Tag, Typography } from 'antd';
import type { TextAreaRef } from 'antd/es/input/TextArea';
import { api } from '../api';
import { projectApi } from '../projectApi';
import { useProjectWorkspace } from '../projectWorkspace';
import { errorText } from './ui';
import { initialScriptSegment, reconcileScriptSegments, scriptTokens, type ScriptDocument, type ScriptRevision, type ScriptSegment, type ScriptWorkbenchView } from '../scriptWorkbench';
import type { RouteState } from '../navigation';
import { drawRoute } from '../drawContracts';
import { ScriptDeliveryPanel } from './ScriptDeliveryPanel';
import { ScriptCompletion } from './ScriptCompletion';
import { useSequenceEditor } from '../sequenceEditor';
import { timelineApi } from '../timelineApi';
import type { ReferenceCandidate, ScriptReference } from '../timelineContracts';
import { candidateLabel, insertReference, rebaseReferences, synchronizeReferenceNames } from '../scriptReferences';
import { applyAuthoringInsert, CHEATSHEET, EFFECT_INSERTS, invalidReplacements, PARAGRAPH_BREAK, paragraphTemplate, SUBTITLE_INSERT, type AuthoringInsert } from '../scriptAuthoring.ts';
import { ScriptTimelineMarkers } from './ScriptTimelineMarkers';
import { ScriptAgentHandoff } from './ScriptAgentHandoff';
import { scriptAgentHandoff } from '../scriptWorkbench';

export function ScriptWorkbench({ assetId, navigate, selectedScriptId, active = true, readOnly = false, variant = 'page' }: { assetId: string; navigate?: (route: Partial<RouteState>) => void; selectedScriptId?: string; active?: boolean; readOnly?:boolean; /** dock = compact side panel; timeline stays primary */ variant?: 'page' | 'dock' }) {
  const client = useQueryClient();
  const projectWorkspace = useProjectWorkspace();
  const sequenceEditor=useSequenceEditor();
  const projectId=projectWorkspace.session?.document?.project.id ?? '';
  const directory=useQuery({queryKey:['reference-directory',projectWorkspace.session?.id,sequenceEditor.sequenceId],queryFn:({signal})=>timelineApi.references(projectId,sequenceEditor.sequenceId,signal),enabled:!!projectId,retry:false});
  const list = useQuery({ queryKey: ['scripts', assetId], queryFn: () => api.listScripts(assetId), retry: false });
  const visible = useQuery({ queryKey: ['visible-set'], queryFn: api.visibleSet, retry: false });
  const media = useQuery({ queryKey: ['script-media', visible.data?.sha256], queryFn: api.scriptMedia, enabled: Boolean(visible.data?.confirmed), retry: false });
  const fonts = useQuery({ queryKey: ['script-fonts'], queryFn: api.scriptFonts, retry: false, staleTime: 60_000 });
  const profiles = useQuery({ queryKey: ['processing-profiles'], queryFn: api.listProfiles });
  const [picked, setPicked] = useState('');
  useEffect(() => { if (selectedScriptId) setPicked(selectedScriptId); }, [selectedScriptId]);
  const scriptId = list.data?.find(s => s.script_id === (picked || selectedScriptId))?.script_id ?? list.data?.at(-1)?.script_id;
  const record = useQuery({ queryKey: ['script-workbench', scriptId], queryFn: () => api.scriptWorkbench(scriptId!), enabled: Boolean(scriptId), retry: false });
  const revisions = useQuery({ queryKey: ['script-revisions', scriptId], queryFn: () => api.scriptRevisions(scriptId!), enabled: Boolean(scriptId), retry: false });
  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [references,setReferences]=useState<ScriptReference[]>([]);
  const [parseBody, setParseBody] = useState('');
  const [segments, setSegments] = useState<ScriptSegment[]>([]);
  const [tts, setTts] = useState(false);
  const [subtitles, setSubtitles] = useState(false);
  const [research, setResearch] = useState(true);
  const [dirty, setDirty] = useState(false);
  const [loadedId, setLoadedId] = useState('');
  const [loadedVersion, setLoadedVersion] = useState(0);
  const [profileId, setProfileId] = useState('');
  const [consent, setConsent] = useState('');
  const [failure, setFailure] = useState('');
  const [notice, setNotice] = useState('');
  const [generating, setGenerating] = useState(false);
  const [handoffBusy,setHandoffBusy]=useState(false);
  const [history, setHistory] = useState<ScriptRevision>();
  const [showSegments,setShowSegments]=useState(false);
  const overlay = useRef<HTMLPreElement>(null);
  const stop = useRef<AbortController | null>(null);
  const locked = useRef(false);
  const mounted = useRef(true);
  const editor = useRef<TextAreaRef>(null);
  const selection = useRef({ start: 0, end: 0 });
  const previousDocument = useRef<ScriptDocument | undefined>(undefined);
  const previousBody = useRef('');
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; stop.current?.abort(); }; }, []);
  const apply = (w: ScriptWorkbenchView) => {
    if(w.segments?.some(segment=>segment.narration.trim())) setShowSegments(true);
    previousDocument.current = w.document; previousBody.current = w.script.body ?? '';
    setTitle(w.script.title ?? ''); setBody(w.script.body ?? ''); setParseBody(w.script.body ?? '');
    setReferences(w.references ?? []);
    setSegments(w.segments ?? []); setTts(w.tts_enabled); setSubtitles(w.subtitles_enabled);
    setResearch(w.allow_web_research); setDirty(false); setLoadedId(w.script.script_id); setLoadedVersion(w.version); setConsent('');
  };
  useEffect(() => { if (record.data && (!dirty || loadedId !== record.data.script.script_id)) apply(record.data); }, [record.data]); // query data changes only
  useEffect(()=>{if(!dirty||!directory.data)return;const next=synchronizeReferenceNames(body,references,directory.data);if(next.body!==body){setBody(next.body);setReferences(next.references);}},[directory.data]);
  useEffect(() => { const timer = setTimeout(() => setParseBody(body), 200); return () => clearTimeout(timer); }, [body]);
  const parsed = useQuery({ queryKey: ['script-parse', parseBody], queryFn: () => api.parseScript(parseBody), retry: false });
  const doc = parsed.data;
  const paragraphs = doc?.paragraphs ?? [];
  const currentMedia = media.data ?? [];
  useEffect(() => {
    if (!doc || parseBody !== body || previousBody.current === body) return;
    const before = previousDocument.current;
    previousDocument.current = doc; previousBody.current = body;
    setSegments(current => reconcileScriptSegments(before, doc, current, currentMedia,references));
  }, [doc, parseBody, body, currentMedia]);
  const drafts = paragraphs.map((p, i) => segments[i] ?? initialScriptSegment(p, currentMedia));
  const usableProfiles = (profiles.data ?? []).filter(p => p.content_mode === 'configured');
  const profile = usableProfiles.find(p => p.profile_id === profileId) ?? usableProfiles[0];
  const modelNotice = useQuery({ queryKey: ['script-model-notice', profile?.profile_id, profile?.revision], queryFn: () => api.autoNotice(profile!.profile_id, profile!.revision), enabled: Boolean(profile), retry: false });
  const consentIdentity = JSON.stringify([assetId, scriptId, record.data?.version, visible.data?.sha256, profile?.profile_id, profile?.revision, modelNotice.data?.profile_sha256]);
  const previousIdentity = useRef(consentIdentity);
  if (previousIdentity.current !== consentIdentity) { previousIdentity.current = consentIdentity; if (consent) setConsent(''); }
  const canParse = Boolean(doc && doc.diagnostics.length === 0 && paragraphs.length && parseBody === body);
  const complete = drafts.length === paragraphs.length && drafts.every((s, i) => (!paragraphs[i].instruction || s.narration.trim()) && s.media_id && s.end > s.start);
  const mutate = useMutation({
    mutationFn: async (fn: () => Promise<unknown>) => { try { if (!mounted.current) throw new Error('工作台已切换，本次操作已取消'); await fn(); } finally { locked.current = false; stop.current = null; setGenerating(false); } },
    onError: e => { if (e.name === 'AbortError') setNotice('已停止生成，已保存的草稿保留。'); else setFailure(errorText(e)); },
    retry: false,
    networkMode: 'always'
  });
  const invoke = (fn: () => Promise<unknown>) => { if (locked.current) return; locked.current = true; setFailure(''); setNotice(''); mutate.mutate(fn); };
  const changed = () => { setDirty(true); setConsent(''); };
  const accept = async (w: ScriptWorkbenchView) => {
    client.setQueryData(['script-workbench', w.script.script_id], w); apply(w);
    await Promise.all(['scripts', 'script-revisions', 'auto', 'workflow', 'audit'].map(k => client.invalidateQueries({ queryKey: [k] })));
  };
  const versionBody = () => ({ expected_version: loadedVersion, visible_set_sha: visible.data!.sha256, references, sequence_id:sequenceEditor.sequenceId });
  const canEdit = Boolean(scriptId && record.data && visible.data?.confirmed && !mutate.isPending && !handoffBusy && !readOnly);
  const busy = mutate.isPending || handoffBusy || readOnly;
  const tokens = useMemo(() => scriptTokens(body), [body]);
  const invalidTokens = useMemo(() => tokens.filter(token => token.kind === 'invalid'), [tokens]);
  const referenceTokens=useMemo(()=>tokens.flatMap(token=>{const match=token.kind==='directive'?/^\{@([^{}]+)\}$/.exec(token.text):null;return match?[{start:token.start,end:token.end,name:match[1]}]:[];}),[tokens]);
  const pendingReferences=referenceTokens.filter(token=>!references.some(ref=>ref.start===token.start&&ref.end===token.end&&!ref.unresolved&&(directory.data ?? []).some(c=>c.kind===ref.kind&&c.target_id===ref.target_id&&c.resource_id===ref.resource_id&&c.sequence_id===ref.sequence_id&&c.clip_id===ref.clip_id)));
  const editSegment = (i: number, value: Partial<ScriptSegment>) => { const next = [...drafts]; next[i] = { ...next[i], ...value }; setSegments(next); changed(); };
  const place = (snippet: AuthoringInsert, candidate?: ReferenceCandidate) => {
    const { start, end } = selection.current;
    const next = applyAuthoringInsert(body, references, start, end, snippet, candidate);
    setBody(next.body); setReferences(next.references); changed();
    selection.current = { start: next.selectStart, end: next.selectEnd };
    requestAnimationFrame(() => {
      const el = editor.current?.resizableTextArea?.textArea;
      el?.focus(); el?.setSelectionRange(next.selectStart, next.selectEnd);
    });
  };
  const insert = (text: string) => place({ text, selectStart: text.length, selectEnd: text.length });
  const bodyRef = useRef(body);
  const referencesRef = useRef(references);
  bodyRef.current = body;
  referencesRef.current = references;
  useEffect(() => {
    const onCite = (event: Event) => {
      if (!active) return;
      const name = (event as CustomEvent<{ citeName?: string }>).detail?.citeName?.trim();
      if (!name) return;
      const run = async () => {
        await client.invalidateQueries({ queryKey: ['reference-directory'] });
        await directory.refetch();
        const dir = client.getQueryData<import('../timelineContracts').ReferenceCandidate[]>(
          ['reference-directory', projectWorkspace.session?.id, sequenceEditor.sequenceId],
        ) ?? directory.data ?? [];
        const hit = dir.find((c) => c.name === name);
        const { start, end } = selection.current;
        const currentBody = bodyRef.current;
        const currentRefs = referencesRef.current;
        if (hit) {
          const next = insertReference(currentBody, currentRefs, start, end, hit);
          setBody(next.body);
          setReferences(next.references);
          selection.current = { start: next.caret, end: next.caret };
          changed();
          requestAnimationFrame(() => {
            const el = editor.current?.resizableTextArea?.textArea;
            el?.focus();
            el?.setSelectionRange(next.caret, next.caret);
          });
        } else {
          const token = `{@${name}}`;
          const next = currentBody.slice(0, start) + token + currentBody.slice(end);
          setReferences(rebaseReferences(currentBody, next, currentRefs));
          setBody(next);
          selection.current = { start: start + token.length, end: start + token.length };
          changed();
          requestAnimationFrame(() => {
            const el = editor.current?.resizableTextArea?.textArea;
            el?.focus();
            el?.setSelectionRange(start + token.length, start + token.length);
          });
        }
      };
      void run();
    };
    window.addEventListener('vac:script-cite', onCite);
    return () => window.removeEventListener('vac:script-cite', onCite);
  }, [active, client, directory.data, directory.refetch, projectWorkspace.session?.id, sequenceEditor.sequenceId]);
  useEffect(() => {
    const onReveal = (event: Event) => {
      if (!active) return;
      const name = (event as CustomEvent<{ name?: string }>).detail?.name?.trim();
      if (!name) return;
      const token = `{@${name}}`;
      const at = bodyRef.current.indexOf(token);
      if (at < 0) { setNotice(`脚本里还没有 ${token}`); return; }
      selection.current = { start: at, end: at + token.length };
      requestAnimationFrame(() => {
        const el = editor.current?.resizableTextArea?.textArea;
        el?.focus();
        el?.setSelectionRange(at, at + token.length);
        el?.scrollIntoView({ block: 'nearest' });
      });
    };
    window.addEventListener('vac:script-reveal', onReveal);
    return () => window.removeEventListener('vac:script-reveal', onReveal);
  }, [active]);
  useEffect(() => {
    if (!scriptId || !record.data) return;
    return projectWorkspace.registerEditor(`script:${scriptId}`, { dirty, flush: async () => {
      if (!dirty) return;
      const saved = await api.saveScript(scriptId, { expected_version: loadedVersion, visible_set_sha: visible.data?.sha256 ?? '', title, body, references,sequence_id:sequenceEditor.sequenceId, segments: drafts, tts_enabled: tts, subtitles_enabled: subtitles, allow_web_research: research });
      await accept(saved);
    } });
  }, [scriptId, dirty, loadedVersion, title, body, references,segments, tts, subtitles, research, visible.data?.sha256, projectWorkspace.registerEditor]);
  const latest = record.data;
  const saveScript = async () => {
    if (!scriptId || !latest || locked.current && generating) throw new Error('请等待脚本读取或生成完成');
    const saved=dirty?await api.saveScript(scriptId,{...versionBody(),title,body,segments:drafts,tts_enabled:tts,subtitles_enabled:subtitles,allow_web_research:research}):latest;
    if(dirty) await accept(saved);
    if(projectWorkspace.session?.path) await projectApi.command('save',{expected_session:projectWorkspace.session.id});
    await projectWorkspace.refresh();
    return saved;
  };
  const prepareAgentTask = async () => {
    if(locked.current || readOnly) throw new Error('请等待当前操作完成');
    locked.current=true;
    setHandoffBusy(true);
    try { const saved=await saveScript();return scriptAgentHandoff({projectId,scriptId:saved.script.script_id,resourceId:projectWorkspace.session?.document?.resources.find(r=>r.script_id===saved.script.script_id)?.id,version:saved.version,title:saved.script.title??'',body:saved.script.body??'',dirty:false}); }
    finally { locked.current=false;setHandoffBusy(false); }
  };

  const dock = variant === 'dock';
  return <Card size="small" title={dock ? null : '脚本工作台'} className={`script-workbench${dock ? ' is-dock' : ''}`} data-testid="script-workbench"
    extra={<Tag>{latest?.script.status === 'finalized' ? '可导出' : '草稿'}</Tag>}>
    <Flex vertical gap={dock ? 8 : 12}>
      {!dock && <Typography.Text type="secondary">先写下创作要求并保存。Agent 可读取脚本，按要求完成编辑；空行分段，花括号可引用素材、字幕或画面效果。</Typography.Text>}
      {dock && <Typography.Text type="secondary" style={{fontSize:12}}>空行分段。{'{@标记}'} 引用素材或时间线，{'{#镜头卡}'} 选择动效模板。</Typography.Text>}
      {paragraphs.some(p=>p.shot_card) && <Alert type="info" message="这份脚本使用动效镜头卡。交给外部 Agent 后，它会将指定镜头生成可编辑代码条，再与其他素材一起编排。" />}
      {scriptId && projectWorkspace.session?.document?.scripts.some(script => script.id === scriptId && script.bindings.some(binding => binding.unresolved)) && <Alert type="warning" message="脚本有待关联素材，可继续编辑。使用相关素材生成或渲染前请完成关联。" action={<Button onClick={() => void projectWorkspace.rebindScript(scriptId).catch(reason => setFailure(errorText(reason)))}>关联素材</Button>} />}
      {list.isError && <Alert type="error" message={errorText(list.error)} />}
      {!list.isPending && !list.data?.length && <Button type="primary" loading={busy} onClick={() => invoke(async () => {
        const sc = await api.enterScript(assetId); setPicked(sc.script_id); await client.invalidateQueries({ queryKey: ['scripts', assetId] });
      })}>新建脚本</Button>}
      {list.data && list.data.length > 1 && <Select aria-label="脚本版本组" value={scriptId} disabled={busy || dirty} onChange={v => { setPicked(v); setLoadedId(''); }}
        options={list.data.map(s => ({ value: s.script_id, label: s.title || s.script_id }))} />}
      {record.isError && <Alert type="error" message={errorText(record.error)} />}
      {scriptId && <Spin spinning={record.isLoading}>
        <Flex vertical gap={12}>
          {latest && ['expired', 'needs_refinalize'].includes(latest.script.status) && <Alert type="warning" message="这份脚本已更新"
            description="素材集合或脚本内容已经变化，旧交付不可继续下载。请保存当前脚本，后续生成会使用最新修订。" />}
          {dirty && latest?.version !== loadedVersion && <Alert type="warning" message="其他操作已保存了新修订，当前编辑不会覆盖它"
            action={<Popconfirm title="放弃本地编辑并载入最新修订？" onConfirm={() => latest && apply(latest)}><Button>载入最新修订</Button></Popconfirm>} />}
          <Space wrap>
            <Button type="primary" disabled={!canEdit || !canParse} onClick={() => invoke(async () => {
              await saveScript();setNotice('脚本已保存，Agent 可读取当前修订。');
            })}>保存脚本</Button>
            <Button onClick={() => void projectWorkspace.exportScript(scriptId).catch(e => setFailure(errorText(e)))}>导出 .avs</Button>
            {navigate && <Button onClick={() => navigate(drawRoute({ project: projectId, slot: scriptId, engine: 'hyperframes', focus: 'source' }))}>在绘制页打开</Button>}
            <Tag>{dirty ? '尚未保存' : `已保存 · 修订 ${latest?.version ?? 0}`}</Tag>
          </Space>
          <div className="script-compose-layout"><div className="script-compose-editor">
          <label htmlFor="script-title">脚本标题</label>
          <Input id="script-title" value={title} disabled={!canEdit} maxLength={200} onChange={e => { setTitle(e.target.value); changed(); }} />
          <label htmlFor="script-body">脚本指令</label>
          <div className="script-code-field">
            <pre aria-hidden="true" ref={overlay} className="script-highlight">{tokens.map((t, i) => t.kind === 'text' ? t.text : <mark key={i} className={`script-token-${t.kind}`}>{t.text}</mark>)}{'\n'}</pre>
            <Input.TextArea ref={editor} id="script-body" aria-label="脚本指令" value={body} rows={dock ? 7 : 9} disabled={!canEdit} spellCheck={false}
              onSelect={e => { const el = e.currentTarget; selection.current = { start: el.selectionStart, end: el.selectionEnd }; }}
              onMouseUp={event => {
                if (!active) return;
                const el = event.currentTarget;
                if (el.selectionStart !== el.selectionEnd) return;
                const token = referenceTokens.find(item => el.selectionStart >= item.start && el.selectionStart <= item.end);
                if (!token) return;
                const bound = references.find(ref => ref.start === token.start && ref.end === token.end && !ref.unresolved);
                const hit = (directory.data ?? []).find(candidate => bound
                  ? candidate.kind === bound.kind && candidate.target_id === bound.target_id && (candidate.sequence_id ?? '') === (bound.sequence_id ?? '')
                  : candidate.name === token.name && candidate.frame !== undefined);
                if (hit?.frame === undefined) return;
                sequenceEditor.setHead(hit.frame);
              }}
              placeholder={'{@抽卡演示.mp4}介绍抽卡机制\n\n{"重点说明", 字号:32px, 字体:"Microsoft YaHei",}{白屏}展示结果'}
              onScroll={e => { if (overlay.current) overlay.current.scrollTop = e.currentTarget.scrollTop; }}
              onChange={e => { const el = e.target; selection.current = { start: el.selectionStart, end: el.selectionEnd }; setReferences(rebaseReferences(body,e.target.value,references));setBody(e.target.value); changed(); }} />
            <ScriptCompletion editor={editor} body={body} references={references} directory={directory.data ?? []} fonts={fonts.data ?? []} onEdit={(next,refs)=>{setBody(next);setReferences(refs);changed();}}/>
          </div>
          {pendingReferences.length>0&&<Alert type="warning" message="引用需要选择具体对象；失效引用会保留，生成或渲染前请重新关联。" description={<Flex vertical gap={6}>{pendingReferences.map(token=><Select<number> key={token.start} aria-label={`关联引用 ${token.name} ${token.start}`} placeholder={`{@${token.name}} · 选择类型、所属素材及位置`} value={undefined} options={(directory.data ?? []).map((candidate,index)=>({value:index,label:candidateLabel(candidate)}))} showSearch optionFilterProp="label" onChange={index=>{const next=insertReference(body,references,token.start,token.end,directory.data![index]);setBody(next.body);setReferences(next.references);changed();}}/>)}</Flex>}/>}
          {invalidTokens.length > 0 && <div className="script-invalid-list" aria-label="可修正的语法">{invalidTokens.slice(0, 6).map(token => <Flex key={`${token.start}:${token.end}`} gap={4} wrap className="script-chip-bar"><Button size="small" danger className="script-chip-invalid" onClick={() => { selection.current = { start: token.start, end: token.end }; const el = editor.current?.resizableTextArea?.textArea; el?.focus(); el?.setSelectionRange(token.start, token.end); }}>{token.text}</Button>{invalidReplacements(token.text).map(fix => <Button key={fix.label} size="small" disabled={!canEdit} onClick={() => { selection.current = { start: token.start, end: token.end }; place(fix); }}>{fix.label}</Button>)}</Flex>)}{invalidTokens.length > 6 && <Typography.Text type="secondary">还有 {invalidTokens.length - 6} 处无效语法。</Typography.Text>}</div>}
          <Flex gap={4} wrap className="script-chip-bar" role="toolbar" aria-label="脚本插入">
            <Select<number> className="script-chip-select" size="small" aria-label="插入素材或标记引用" placeholder="引用" showSearch optionFilterProp="label" disabled={!canEdit} value={undefined} popupMatchSelectWidth={280} options={(directory.data ?? []).map((item,index)=>({value:index,label:candidateLabel(item)}))} onChange={index=>{const item=directory.data![index],{start,end}=selection.current;const next=insertReference(body,references,start,end,item);setBody(next.body);setReferences(next.references);changed();selection.current={start:next.caret,end:next.caret};requestAnimationFrame(()=>{const el=editor.current?.resizableTextArea?.textArea;el?.focus();el?.setSelectionRange(next.caret,next.caret);});}}/>
            <Button size="small" disabled={!canEdit} onClick={() => place(SUBTITLE_INSERT)}>字幕</Button>
            <Button size="small" disabled={!canEdit} onClick={() => place(EFFECT_INSERTS.黑屏)}>黑屏</Button>
            <Button size="small" disabled={!canEdit} onClick={() => place(EFFECT_INSERTS.白屏)}>白屏</Button>
            <Button size="small" disabled={!canEdit} onClick={() => place(EFFECT_INSERTS.溶解)}>溶解</Button>
            <Button size="small" disabled={!canEdit} onClick={() => place(PARAGRAPH_BREAK)}>空行分段</Button>
          </Flex>
          <Flex gap={4} wrap align="center" className="script-chip-bar" role="toolbar" aria-label="段落模板">
            <span className="script-chip-label">模板</span>
            <Button size="small" disabled={!canEdit} onClick={() => place(paragraphTemplate('open'))}>开场</Button>
            <Button size="small" disabled={!canEdit} onClick={() => place(paragraphTemplate('result'))}>结果</Button>
            <Button size="small" disabled={!canEdit} onClick={() => place(paragraphTemplate('transition'))}>转场</Button>
          </Flex>
          <Collapse size="small" items={[{ key: 'syntax', label: '语法速查', children: (
            <Flex vertical gap={4} className="script-cheat-list">
              <Typography.Text type="secondary" className="script-cheat-note">只有这五种写法。点击一行，插入到光标处。</Typography.Text>
              {CHEATSHEET.map(row => <Button key={row.label} size="small" block className="script-cheat-row" disabled={!canEdit} onClick={() => place(row.insert)}><code>{row.label}</code><span>{row.hint}</span></Button>)}
            </Flex>
          ) }]} />
          <Collapse size="small" items={[{key:"markers",label:"时间线标记",children:<ScriptTimelineMarkers canEdit={canEdit} document={doc} segments={drafts} onInsert={insert} />}]} />
          {fonts.isError && <Typography.Text type="secondary">暂时无法读取本机字体；可以先编辑脚本，交付前会核验字体。</Typography.Text>}
          {parsed.isError && <Alert type="error" message="语法检查暂不可用，请稍后重试" />}
          {doc && doc.diagnostics.length > 0 && <Alert type="error" role="alert" message="请修正特殊语法" description={doc.diagnostics.map((d, i) => <div key={i}>第 {d.paragraph + 1} 段，第 {d.column} 字：{d.message}</div>)} />}
          <Space wrap>
            <Checkbox checked={tts} disabled={!canEdit} onChange={e => { setTts(e.target.checked); changed(); }}>默认启用配音</Checkbox>
            <Checkbox checked={subtitles} disabled={!canEdit} onChange={e => { setSubtitles(e.target.checked); changed(); }}>自动字幕</Checkbox>
            <Checkbox checked={research} disabled={!canEdit} onChange={e => { setResearch(e.target.checked); changed(); }}>允许联网查证文字知识</Checkbox>
          </Space>
          </div><aside className="script-compose-actions" aria-label="脚本生成与Agent交接">
          <ScriptAgentHandoff task={scriptAgentHandoff({ projectId, scriptId, version: loadedVersion, title, body, dirty })} prepareTask={prepareAgentTask} navigate={navigate} active={active} />
          <Collapse size="small" items={[{key:"model",label:"使用内部模型生成文稿（兼容）",children:(
          <Card size="small" title="发送给内部模型">
            <Flex vertical gap={8}>
              <Select aria-label="脚本生成预设" disabled={busy} value={profile?.profile_id} placeholder="选择已配置模型的预设" onChange={value => { setProfileId(value); setConsent(''); }} options={usableProfiles.map(item => ({ value: item.profile_id, label: `${item.name} · v${item.revision} · ${item.narration.model || '已配置模型'}` }))} />
              <Typography.Text type="secondary">使用 VAC 中已保存的模型配置。点击发送会先保存当前脚本，再发送脚本要求和可见素材抽帧，结果回到下方逐段检查。</Typography.Text>
              {profiles.isError && <Alert type="error" message="模型预设读取失败" action={<Button onClick={() => void profiles.refetch()}>重试读取预设</Button>} />}
              {!profiles.isPending && !profiles.isError && !profile && <Alert type="info" message="尚无已配置模型的预设" action={navigate && <Button onClick={() => navigate({ workspace: 'settings', settingsCategory: 'providers' })}>配置内部模型</Button>} />}
              {modelNotice.isError && <Alert type="error" message="模型授权信息读取失败" action={<Button onClick={() => void modelNotice.refetch()}>重试</Button>} />}
              {modelNotice.data?.external && <Checkbox className="script-model-consent" checked={consent === consentIdentity} disabled={busy} onChange={event => setConsent(event.target.checked ? consentIdentity : '')}>允许本次向所选预设的外部服务发送脚本和可见素材抽帧</Checkbox>}
              <Space wrap>
                <Button type="primary" disabled={!canEdit || !canParse || !profile || !modelNotice.data || modelNotice.isError || (modelNotice.data.external && consent !== consentIdentity)} onClick={() => invoke(async () => {
                  stop.current = new AbortController(); setGenerating(true);
                  let version = loadedVersion;
                  if (dirty) {
                    const saved = await api.saveScript(scriptId!, { ...versionBody(), title, body, segments: drafts, tts_enabled: tts, subtitles_enabled: subtitles, allow_web_research: research });
                    version = saved.version; await accept(saved);
                  }
                  stop.current!.signal.throwIfAborted();
                  if (modelNotice.data?.external) await api.profileConsent(profile!.profile_id, profile!.revision, true);
                  stop.current!.signal.throwIfAborted();
                  await accept(await api.generateScript(scriptId!, { expected_version: version, visible_set_sha: visible.data!.sha256, profile_id: profile!.profile_id, profile_revision: profile!.revision, allow_web_research: research }, stop.current!.signal));
                })}>发送给内部模型</Button>
                {generating && <Button onClick={() => stop.current?.abort()}>停止生成</Button>}
                {navigate && <Button onClick={() => navigate({ workspace: 'settings', settingsCategory: 'providers' })}>配置内部模型</Button>}
              </Space>
            </Flex>
          </Card>

          )}]} />
          </aside></div>
          <Typography.Text type="secondary">每段可覆盖配音和字幕开关；声音细调在 TTS 专页保存回当前脚本修订。显式字幕始终生效，画面使用当前工程素材。</Typography.Text>

          <Collapse size="small" activeKey={showSegments?["segments"]:[]} onChange={keys=>setShowSegments(keys.length>0)} items={[{key:"segments",label:"分段文稿、配音与历史",children:<>
          {dock ? (
            <Collapse size="small" defaultActiveKey={[]} items={[{ key: 'advanced', label: 'Agent 出稿 · 分段 · 历史', children: (
              <Flex vertical gap={8}>
          {latest?.questions?.length ? <Alert type="warning" message="Agent 需要补充信息" description={latest.questions.map((q, i) => <div key={i}>{q}</div>)} /> : null}
          {paragraphs.map((p, i) => {
            const seg = drafts[i];
            const choices = p.video ? currentMedia.filter(m => m.name === p.video) : currentMedia;
            return <Card size="small" title={`第 ${i + 1} 段`} key={`${i}-${p.source}`} className="script-paragraph">
              <Flex vertical gap={8}>
                <Typography.Text>{p.instruction || '仅特殊语法段'}</Typography.Text>
                <Space wrap>
                  <Checkbox checked={seg.tts_enabled ?? tts} disabled={!canEdit || !canParse} onChange={e => editSegment(i, { tts_enabled: e.target.checked })}>本段配音</Checkbox>
                  <Checkbox checked={seg.subtitles_enabled ?? subtitles} disabled={!canEdit || !canParse} onChange={e => editSegment(i, { subtitles_enabled: e.target.checked })}>本段自动字幕</Checkbox>
                  {(seg.tts_enabled ?? tts) && <Button disabled={dirty || busy || !navigate} onClick={() => navigate?.({ workspace: 'tts', scriptId, scriptVersion: loadedVersion, scriptSegment: i })}>到 TTS 专页调整第 {i + 1} 段</Button>}
                </Space>
                <Space wrap>{p.video && <Tag>强制视频：{p.video}</Tag>}{p.shot_card && <Tag>镜头卡：{p.shot_card}</Tag>}{p.subtitle && <Tag>强制字幕：{p.subtitle.text}</Tag>}{p.effect && <Tag>画面效果：{p.effect}</Tag>}</Space>
                <label htmlFor={`script-narration-${i}`}>实际旁白文稿（可手写，也可由 Agent 生成）</label>
                <Input.TextArea id={`script-narration-${i}`} value={seg.narration} disabled={!canEdit || !canParse} rows={3} maxLength={2000}
                  onChange={e => editSegment(i, { narration: e.target.value })} />
                <Select aria-label={`第${i + 1}段视频`} value={seg.media_id || undefined} disabled={!canEdit || !canParse} placeholder="选择搭配的视频"
                  options={choices.map(m => ({ value: m.id, label: `${m.name} · ${m.asset_id}` }))} onChange={id => {
                    const m = currentMedia.find(m => m.id === id)!; editSegment(i, { media_id: id, start: m.in, end: Math.min(m.out, m.in + 8) });
                  }} />
                <Space wrap>
                  <span>源视频起点（秒）</span><InputNumber aria-label={`第${i + 1}段起点`} min={0} value={seg.start} disabled={!canEdit || !canParse} onChange={v => editSegment(i, { start: v ?? 0 })} />
                  <span>终点（秒）</span><InputNumber aria-label={`第${i + 1}段终点`} min={0} value={seg.end} disabled={!canEdit || !canParse} onChange={v => editSegment(i, { end: v ?? 0 })} />
                </Space>
              </Flex>
            </Card>;
          })}
          {(latest?.sources?.length ?? 0) > 0 && <Collapse items={[{ key: 'sources', label: '本次实际查证的文字资料', children: latest!.sources.map(s => <div key={s.url}><Typography.Link href={s.url} target="_blank" rel="noreferrer">{s.title}</Typography.Link></div>) }]} />}

          {!dirty && latest && <ScriptDeliveryPanel scriptId={scriptId} active={active} />}
          <Collapse items={[{ key: 'history', label: `修订历史（${revisions.data?.length ?? 0}）`, children: <Space wrap>{revisions.data?.map(r => <Button key={r.version} onClick={() => setHistory(r)}>修订 {r.version} · {r.action}</Button>)}</Space> }]} />
              </Flex>
            )}]} />
          ) : (
            <>
          {latest?.questions?.length ? <Alert type="warning" message="Agent 需要补充信息" description={latest.questions.map((q, i) => <div key={i}>{q}</div>)} /> : null}
          {paragraphs.map((p, i) => {
            const seg = drafts[i];
            const choices = p.video ? currentMedia.filter(m => m.name === p.video) : currentMedia;
            return <Card size="small" title={`第 ${i + 1} 段`} key={`${i}-${p.source}`} className="script-paragraph">
              <Flex vertical gap={8}>
                <Typography.Text>{p.instruction || '仅特殊语法段'}</Typography.Text>
                <Space wrap>
                  <Checkbox checked={seg.tts_enabled ?? tts} disabled={!canEdit || !canParse} onChange={e => editSegment(i, { tts_enabled: e.target.checked })}>本段配音</Checkbox>
                  <Checkbox checked={seg.subtitles_enabled ?? subtitles} disabled={!canEdit || !canParse} onChange={e => editSegment(i, { subtitles_enabled: e.target.checked })}>本段自动字幕</Checkbox>
                  {(seg.tts_enabled ?? tts) && <Button disabled={dirty || busy || !navigate} onClick={() => navigate?.({ workspace: 'tts', scriptId, scriptVersion: loadedVersion, scriptSegment: i })}>到 TTS 专页调整第 {i + 1} 段</Button>}
                </Space>
                <Space wrap>{p.video && <Tag>强制视频：{p.video}</Tag>}{p.shot_card && <Tag>镜头卡：{p.shot_card}</Tag>}{p.subtitle && <Tag>强制字幕：{p.subtitle.text}</Tag>}{p.effect && <Tag>画面效果：{p.effect}</Tag>}</Space>
                <label htmlFor={`script-narration-${i}`}>实际旁白文稿（可手写，也可由 Agent 生成）</label>
                <Input.TextArea id={`script-narration-${i}`} value={seg.narration} disabled={!canEdit || !canParse} rows={3} maxLength={2000}
                  onChange={e => editSegment(i, { narration: e.target.value })} />
                <Select aria-label={`第${i + 1}段视频`} value={seg.media_id || undefined} disabled={!canEdit || !canParse} placeholder="选择搭配的视频"
                  options={choices.map(m => ({ value: m.id, label: `${m.name} · ${m.asset_id}` }))} onChange={id => {
                    const m = currentMedia.find(m => m.id === id)!; editSegment(i, { media_id: id, start: m.in, end: Math.min(m.out, m.in + 8) });
                  }} />
                <Space wrap>
                  <span>源视频起点（秒）</span><InputNumber aria-label={`第${i + 1}段起点`} min={0} value={seg.start} disabled={!canEdit || !canParse} onChange={v => editSegment(i, { start: v ?? 0 })} />
                  <span>终点（秒）</span><InputNumber aria-label={`第${i + 1}段终点`} min={0} value={seg.end} disabled={!canEdit || !canParse} onChange={v => editSegment(i, { end: v ?? 0 })} />
                </Space>
              </Flex>
            </Card>;
          })}
          {(latest?.sources?.length ?? 0) > 0 && <Collapse items={[{ key: 'sources', label: '本次实际查证的文字资料', children: latest!.sources.map(s => <div key={s.url}><Typography.Link href={s.url} target="_blank" rel="noreferrer">{s.title}</Typography.Link></div>) }]} />}

          {!dirty && latest && <ScriptDeliveryPanel scriptId={scriptId} active={active} />}
          <Collapse items={[{ key: 'history', label: `修订历史（${revisions.data?.length ?? 0}）`, children: <Space wrap>{revisions.data?.map(r => <Button key={r.version} onClick={() => setHistory(r)}>修订 {r.version} · {r.action}</Button>)}</Space> }]} />
            </>
          )}

          </>}]} />
        </Flex>
      </Spin>}
      {failure && <Alert type="error" showIcon role="alert" message={failure} closable onClose={() => setFailure('')} />}
      {notice && <Alert type="info" showIcon message={notice} closable onClose={() => setNotice('')} />}
    </Flex>
    <Modal title={history ? `修订 ${history.version}` : ''} open={Boolean(history)} onCancel={() => setHistory(undefined)} footer={null}>
      <pre className="script-history">{history ? JSON.stringify(JSON.parse(history.snapshot), null, 2) : ''}</pre>
    </Modal>
  </Card>;
}
