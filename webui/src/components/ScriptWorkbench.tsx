import { useEffect, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, Button, Card, Checkbox, Collapse, Flex, Input, InputNumber, Modal, Popconfirm, Select, Space, Spin, Tag, Typography } from 'antd';
import type { TextAreaRef } from 'antd/es/input/TextArea';
import { api } from '../api';
import { useProjectWorkspace } from '../projectWorkspace';
import { errorText } from './ui';
import { initialScriptSegment, reconcileScriptSegments, scriptTokens, type ScriptDocument, type ScriptRevision, type ScriptSegment, type ScriptWorkbenchView } from '../scriptWorkbench';
import type { RouteState } from '../navigation';
import { ScriptDeliveryPanel } from './ScriptDeliveryPanel';
import { ScriptCompletion } from './ScriptCompletion';
import { useSequenceEditor } from '../sequenceEditor';
import { timelineApi } from '../timelineApi';
import type { ScriptReference } from '../timelineContracts';
import { candidateLabel, insertReference, rebaseReferences, synchronizeReferenceNames } from '../scriptReferences';

export function ScriptWorkbench({ assetId, navigate, selectedScriptId, active = true }: { assetId: string; navigate?: (route: Partial<RouteState>) => void; selectedScriptId?: string; active?: boolean }) {
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
  const [history, setHistory] = useState<ScriptRevision>();
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
  const consentIdentity = JSON.stringify([assetId, scriptId, record.data?.version, visible.data?.sha256, profile?.profile_id, profile?.revision]);
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
  const canEdit = Boolean(scriptId && record.data && visible.data?.confirmed && !mutate.isPending);
  const busy = mutate.isPending;
  const tokens = useMemo(() => scriptTokens(body), [body]);
  const referenceTokens=useMemo(()=>{let start=0;return tokens.flatMap(token=>{const offset=start;start+=token.text.length;const match=token.kind==='directive'?/^\{@([^{}]+)\}$/.exec(token.text):null;return match?[{start:offset,end:start,name:match[1]}]:[];});},[tokens]);
  const pendingReferences=referenceTokens.filter(token=>!references.some(ref=>ref.start===token.start&&ref.end===token.end&&!ref.unresolved&&(directory.data ?? []).some(c=>c.kind===ref.kind&&c.target_id===ref.target_id&&c.resource_id===ref.resource_id&&c.sequence_id===ref.sequence_id&&c.clip_id===ref.clip_id)));
  const editSegment = (i: number, value: Partial<ScriptSegment>) => { const next = [...drafts]; next[i] = { ...next[i], ...value }; setSegments(next); changed(); };
  const insert = (text: string) => {
    const { start, end } = selection.current;
    const next=body.slice(0,start)+text+body.slice(end);setReferences(rebaseReferences(body,next,references));setBody(next);changed();
    selection.current = { start: start + text.length, end: start + text.length };
    requestAnimationFrame(() => {
      const el = editor.current?.resizableTextArea?.textArea;
      el?.focus(); el?.setSelectionRange(start + text.length, start + text.length);
    });
  };
  useEffect(() => {
    if (!scriptId || !record.data) return;
    return projectWorkspace.registerEditor(`script:${scriptId}`, { dirty, flush: async () => {
      if (!dirty) return;
      const saved = await api.saveScript(scriptId, { expected_version: loadedVersion, visible_set_sha: visible.data?.sha256 ?? '', title, body, references,sequence_id:sequenceEditor.sequenceId, segments: drafts, tts_enabled: tts, subtitles_enabled: subtitles, allow_web_research: research });
      await accept(saved);
    } });
  }, [scriptId, dirty, loadedVersion, title, body, references,segments, tts, subtitles, research, visible.data?.sha256, projectWorkspace.registerEditor]);
  const latest = record.data;

  return <Card size="small" title="脚本工作台" className="script-workbench" data-testid="script-workbench"
    extra={<Tag>{latest?.script.status === 'finalized' ? '已定稿' : latest?.script.status === 'needs_refinalize' ? '待重新定稿' : '草稿'}</Tag>}>
    <Flex vertical gap={12}>
      <Typography.Text type="secondary">空行分段。普通文字是生成要求；花括号指定视频、字幕或画面效果。生成文稿后可逐段修改并人工定稿。</Typography.Text>
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
          {latest && ['expired', 'needs_refinalize'].includes(latest.script.status) && <Alert type="warning" message="这份脚本需要重新确认"
            description="素材集合或脚本内容已经变化，旧交付不可继续下载。请检查、保存并重新定稿。" />}
          {dirty && latest?.version !== loadedVersion && <Alert type="warning" message="其他操作已保存了新修订，当前编辑不会覆盖它"
            action={<Popconfirm title="放弃本地编辑并载入最新修订？" onConfirm={() => latest && apply(latest)}><Button>载入最新修订</Button></Popconfirm>} />}
          <label htmlFor="script-title">脚本标题</label>
          <Input id="script-title" value={title} disabled={!canEdit} maxLength={200} onChange={e => { setTitle(e.target.value); changed(); }} />
          <label htmlFor="script-body">脚本指令</label>
          <div className="script-code-field">
            <pre aria-hidden="true" ref={overlay} className="script-highlight">{tokens.map((t, i) => t.kind === 'text' ? t.text : <mark key={i} className={`script-token-${t.kind}`}>{t.text}</mark>)}{'\n'}</pre>
            <Input.TextArea ref={editor} id="script-body" aria-label="脚本指令" value={body} rows={9} disabled={!canEdit} spellCheck={false}
              onSelect={e => { const el = e.currentTarget; selection.current = { start: el.selectionStart, end: el.selectionEnd }; }}
              placeholder={'{@抽卡演示.mp4}介绍抽卡机制\n\n{"重点说明", 字号:32px, 字体:"Microsoft YaHei",}{白屏}展示结果'}
              onScroll={e => { if (overlay.current) overlay.current.scrollTop = e.currentTarget.scrollTop; }}
              onChange={e => {setReferences(rebaseReferences(body,e.target.value,references));setBody(e.target.value); changed(); }} />
            <ScriptCompletion editor={editor} body={body} references={references} directory={directory.data ?? []} onEdit={(next,refs)=>{setBody(next);setReferences(refs);changed();}}/>
          </div>
          {pendingReferences.length>0&&<Alert type="warning" message="引用需要选择具体对象；失效引用会保留，生成或渲染前请重新关联。" description={<Flex vertical gap={6}>{pendingReferences.map(token=><Select<number> key={token.start} aria-label={`关联引用 ${token.name} ${token.start}`} placeholder={`{@${token.name}} · 选择类型、所属素材及位置`} value={undefined} options={(directory.data ?? []).map((candidate,index)=>({value:index,label:candidateLabel(candidate)}))} showSearch optionFilterProp="label" onChange={index=>{const next=insertReference(body,references,token.start,token.end,directory.data![index]);setBody(next.body);setReferences(next.references);changed();}}/>)}</Flex>}/>}
          <Space wrap>
            <Select<number> aria-label="插入素材或标记引用" placeholder="插入素材或标记引用" showSearch optionFilterProp="label" disabled={!canEdit} value={undefined} style={{minWidth:240,maxWidth:'100%'}} options={(directory.data ?? []).map((item,index)=>({value:index,label:candidateLabel(item)}))} onChange={index=>{const item=directory.data![index],{start,end}=selection.current;const next=insertReference(body,references,start,end,item);setBody(next.body);setReferences(next.references);changed();requestAnimationFrame(()=>{const el=editor.current?.resizableTextArea?.textArea;el?.focus();el?.setSelectionRange(next.caret,next.caret);});}}/>
            <Select aria-label="插入系统字体字幕" placeholder="插入字体字幕" disabled={!canEdit || !fonts.data?.length} value={undefined} showSearch style={{ minWidth: 180 }}
              options={(fonts.data ?? []).map(f => ({ value: f, label: f }))} onChange={f => insert(`{"字幕文字", 字体:${JSON.stringify(f)}}`)} />
            {['黑屏', '白屏', '交叉溶解'].map(effect => <Button key={effect} disabled={!canEdit} onClick={() => insert(`{${effect}}`)}>插入{effect}</Button>)}
          </Space>
          {fonts.isError && <Typography.Text type="secondary">暂时无法读取本机字体；可以先编辑脚本，交付前会核验字体。</Typography.Text>}
          {parsed.isError && <Alert type="error" message="语法检查暂不可用，请稍后重试" />}
          {doc && doc.diagnostics.length > 0 && <Alert type="error" role="alert" message="请修正特殊语法" description={doc.diagnostics.map((d, i) => <div key={i}>第 {d.paragraph + 1} 段，第 {d.column} 字：{d.message}</div>)} />}
          <Space wrap>
            <Checkbox checked={tts} disabled={!canEdit} onChange={e => { setTts(e.target.checked); changed(); }}>默认启用配音</Checkbox>
            <Checkbox checked={subtitles} disabled={!canEdit} onChange={e => { setSubtitles(e.target.checked); changed(); }}>自动字幕</Checkbox>
            <Checkbox checked={research} disabled={!canEdit} onChange={e => { setResearch(e.target.checked); changed(); }}>允许联网查证文字知识</Checkbox>
          </Space>
          <Typography.Text type="secondary">每段可覆盖配音和字幕开关；声音细调在 TTS 专页保存回当前脚本修订。显式字幕始终生效，画面使用当前工程素材。</Typography.Text>
          <Space wrap>
            <Button type="primary" disabled={!canEdit || !canParse} onClick={() => invoke(async () => {
              await accept(await api.saveScript(scriptId!, { ...versionBody(), title, body, segments: drafts,
                tts_enabled: tts, subtitles_enabled: subtitles, allow_web_research: research }));
            })}>保存草稿</Button>
            <Button onClick={() => void projectWorkspace.exportScript(scriptId).catch(e => setFailure(errorText(e)))}>导出 .avs</Button>
            <Tag>{dirty ? '尚未保存' : `已保存 · 修订 ${latest?.version ?? 0}`}</Tag>
          </Space>
          <Card size="small" title="让 Agent 理解脚本并出稿">
            <Flex vertical gap={8}>
              <Select aria-label="脚本生成预设" disabled={busy} value={profile?.profile_id} placeholder="选择已配置模型的预设" onChange={v => { setProfileId(v); setConsent(''); }}
                options={usableProfiles.map(p => ({ value: p.profile_id, label: `${p.name} · v${p.revision}` }))} />
              <Typography.Text type="secondary">将向所选预设发送当前工程素材的抽帧和脚本要求。允许查证时会查询文字资料并保留实际来源；游戏或规则不明确时会先提出问题。</Typography.Text>
              <Space>
                <Button disabled={!canEdit || dirty || !canParse || !profile} onClick={() => invoke(async () => {
                  stop.current = new AbortController();
                  setGenerating(true);
                  stop.current.signal.throwIfAborted();
                  await accept(await api.generateScript(scriptId!, { ...versionBody(), profile_id: profile!.profile_id, profile_revision: profile!.revision, allow_web_research: research }, stop.current.signal));
                })}>理解脚本并生成文稿</Button>
                {busy && generating && <Button onClick={() => stop.current?.abort()}>停止生成</Button>}
              </Space>
            </Flex>
          </Card>
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
                <Space wrap>{p.video && <Tag>强制视频：{p.video}</Tag>}{p.subtitle && <Tag>强制字幕：{p.subtitle.text}</Tag>}{p.effect && <Tag>画面效果：{p.effect}</Tag>}</Space>
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
          <Space wrap>
            <Button disabled={!canEdit || dirty || !canParse || !complete || (latest?.questions?.length ?? 0) > 0 || latest?.rejected || latest?.script.status === 'finalized'}
              onClick={() => invoke(async () => accept(await api.finalizeScriptWorkbench(scriptId!, versionBody())))}>人工定稿</Button>
            <Popconfirm title="否决当前文稿？" description="保留指令和历史修订，清除当前生成文稿。" onConfirm={() => invoke(async () => accept(await api.rejectScript(scriptId!, versionBody())))}>
              <Button danger disabled={!canEdit}>否决文稿</Button>
            </Popconfirm>
          </Space>
          {!dirty && latest && <ScriptDeliveryPanel scriptId={scriptId} active={active} />}
          <Collapse items={[{ key: 'history', label: `修订历史（${revisions.data?.length ?? 0}）`, children: <Space wrap>{revisions.data?.map(r => <Button key={r.version} onClick={() => setHistory(r)}>修订 {r.version} · {r.action}</Button>)}</Space> }]} />
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
