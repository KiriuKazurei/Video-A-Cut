import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Alert, Button, Checkbox, Flex, Input, InputNumber, Modal, Progress, Select, Slider, Space, Tag, Typography
} from 'antd';
import type { Asset } from '../types';
import { deliveryVariantApi, previewApi, subtitleStyleApi } from '../api';
import { createPreviewSource, previewFileURL, previewQueryKey, previewStyleLabel, type PreviewView } from '../previewContracts';
import { variantFileURL, variantQueryKey, variantZIPURL } from '../deliveryVariantContracts';
import type { SourceCue, StyleDocument, SubtitleStyle, VariantView } from '../deliveryVariantContracts';
import type { PreviewSource } from './Monitor';
import { errorText } from './ui';
import {
  alignmentPlacement,
  applyPreset,
  applyStyleToAll,
  adoptServerDraft,
  canonicalJSON,
  combinedCueStyle,
  commitEdit,
  effectiveCue,
  formatCueMs,
  idleChain,
  noteSaveOutcome,
  recallLocalDraft,
  rememberLocalDraft,
  restoreInheritance,
  runStyledExport,
  setCueDuration,
  setCueStart,
  setCueStyles,
  setCueTiming,
  setGlobalField,
  sharedStyleValue,
  styleCacheKey,
  stylePresets,
  undoEdit,
  validateDocument,
  type ChainProgress,
  type EditorScope,
  type LocalDraft,
  type StyleField
} from '../assEditorModel';

const ACTIVE = new Set(['queued', 'claimed', 'running']);
const STYLE_LABELS: Record<StyleField, string> = {
  font_family: '字体', font_size: '字号', bold: '粗体', italic: '斜体', underline: '下划线',
  primary_color: '文字颜色', outline_color: '描边颜色', outline: '描边', shadow: '阴影',
  alignment: '位置', margin_l: '左边距', margin_r: '右边距', margin_v: '垂直边距',
  fade_in_ms: '渐入', fade_out_ms: '渐出'
};

interface Session extends LocalDraft {
  key: string;
}

export function AssStyleEditor({
  asset, runId, revisionId, open, onClose, onSelectPreview
}: {
  asset?: Asset;
  runId: string;
  revisionId: string;
  open: boolean;
  onClose: () => void;
  onSelectPreview?: (preview: PreviewSource) => void;
}) {
  const client = useQueryClient();
  const assetId = asset?.asset_id ?? '';
  const context = `${assetId}|${runId}|${revisionId}`;
  const draftQuery = useQuery({
    queryKey: ['subtitleStyle', assetId, runId, revisionId],
    queryFn: () => subtitleStyleApi.get(runId),
    enabled: open && Boolean(assetId && runId && revisionId),
    refetchInterval: open ? 3000 : false
  });
  const fontQuery = useQuery({
    queryKey: ['subtitleFonts'],
    queryFn: () => subtitleStyleApi.fonts(),
    enabled: open,
    staleTime: 30_000
  });
  const variants = useQuery({
    queryKey: variantQueryKey(assetId, runId, revisionId),
    queryFn: () => deliveryVariantApi.list(runId),
    enabled: open && Boolean(assetId && runId && revisionId),
    refetchInterval: open ? 2000 : false
  });
  const previews = useQuery({
    queryKey: previewQueryKey(assetId, runId, revisionId),
    queryFn: () => previewApi.list(runId),
    enabled: open && Boolean(assetId && runId && revisionId),
    refetchInterval: open ? 2000 : false
  });

  const server = draftQuery.data && draftQuery.data.asset_id === assetId && draftQuery.data.run_id === runId && draftQuery.data.revision_id === revisionId
    ? draftQuery.data : undefined;
  const currentKey = server ? styleCacheKey(assetId, runId, revisionId, server.document.source_srt_sha256) : null;
  const [session, setSession] = useState<Session | null>(null);
  const [selected, setSelected] = useState<number[]>([]);
  const [scope, setScope] = useState<EditorScope>('global');
  const [chain, setChain] = useState<ChainProgress>(idleChain);
  const [saveError, setSaveError] = useState<string | null>(null);
  const sessionRef = useRef<Session | null>(null);
  const tokenRef = useRef(0);
  const requestsRef = useRef<{ context: string; requests?: ChainProgress['requests'] } | null>(null);
  const anchorRef = useRef<number | null>(null);
  const captionRef = useRef<HTMLDivElement | null>(null);
  const animationRef = useRef<Animation | null>(null);
  const playerRef = useRef<HTMLVideoElement | null>(null);
  const bindPlayer = useCallback((node: HTMLVideoElement | null) => {
    if (playerRef.current && playerRef.current !== node) playerRef.current.pause();
    playerRef.current = node;
  }, []);
  sessionRef.current = session;
  const active = session && session.key === currentKey ? session : null;

  useEffect(() => {
    tokenRef.current += 1;
    setChain(idleChain);
    setSaveError(null);
    setSelected([]);
  }, [context]);

  useEffect(() => () => {
    if (sessionRef.current) rememberLocalDraft(sessionRef.current.key, sessionRef.current);
  }, []);

  useEffect(() => {
    if (!currentKey || !server) return;
    setSession(existing => {
      if (existing?.key === currentKey) return existing;
      if (existing) rememberLocalDraft(existing.key, existing);
      return { key: currentKey, ...adoptServerDraft(server, currentKey) };
    });
  }, [currentKey, server]);

  useEffect(() => {
    if (session) rememberLocalDraft(session.key, session);
  }, [session]);

  const edit = (next: StyleDocument) => {
    setSession(existing => {
      if (!existing || existing.key !== currentKey) return existing;
      const committed = commitEdit(existing.undo, existing.document, next);
      return { ...existing, document: committed.current, undo: committed.undo };
    });
  };

  const document = active?.document;
  const cues = server?.cues ?? [];
  const errors = document && server ? validateDocument(document, cues, server.duration_ms) : [];
  const dirty = Boolean(document && server && canonicalJSON(document) !== canonicalJSON(server.document));
  const versionConflict = Boolean(server && active && active.baseDraftVersion !== server.draft_version);
  const running = chain.phase === 'saving' || chain.phase === 'ass' || chain.phase === 'mp4';
  const singleId = selected.length === 1 ? selected[0] : undefined;
  const singleCue = cues.find(cue => cue.cue_id === singleId);
  const singleEffective = document && singleCue ? effectiveCue(singleCue, document) : undefined;
  const selectedOverrides = document?.overrides.filter(override => selected.includes(override.cue_id)) ?? [];
  const overriddenFields = (Object.keys(STYLE_LABELS) as StyleField[]).filter(field => selectedOverrides.some(override => override.style?.[field] !== undefined));
  const overlapping: number[] = [];
  if (document) {
    const timed = cues.map(cue => effectiveCue(cue, document)).sort((a, b) => a.start_ms - b.start_ms);
    let previous = timed[0];
    for (const cue of timed.slice(1)) {
      if (previous && Math.floor((cue.start_ms + 5) / 10) < Math.floor((previous.end_ms + 5) / 10)) overlapping.push(cue.cue_id);
      if (!previous || cue.end_ms > previous.end_ms) previous = cue;
    }
  }
  const formStyle: SubtitleStyle | undefined = !document ? undefined
    : scope === 'global' ? document.style
      : combinedCueStyle(document, cues, selected);
  const previewCue = singleCue ?? cues[0];
  const previewEffective = document && previewCue ? effectiveCue(previewCue, document) : undefined;
  const missingFonts = fontQuery.isSuccess && document
    ? [...new Set(cues.map(cue => effectiveCue(cue, document).style.font_family))].filter(family => !fontQuery.data.some(font => font.family === family))
    : [];
  const fontUnavailable = !fontQuery.isSuccess || missingFonts.length > 0;

  useEffect(() => {
    animationRef.current?.cancel();
    if (!open) playerRef.current?.pause();
    return () => { animationRef.current?.cancel(); };
  }, [open, context, document, previewCue?.cue_id]);

  const playSchematic = () => {
    if (!captionRef.current || !previewEffective || errors.length) return;
    animationRef.current?.cancel();
    const duration = (Math.floor((previewEffective.end_ms + 5) / 10) - Math.floor((previewEffective.start_ms + 5) / 10)) * 10;
    const { fade_in_ms: fadeIn, fade_out_ms: fadeOut } = previewEffective.style;
    animationRef.current = captionRef.current.animate([
      { opacity: fadeIn ? 0 : 1, offset: 0 },
      { opacity: 1, offset: Math.min(1, fadeIn / duration) },
      { opacity: 1, offset: Math.max(fadeIn / duration, 1 - fadeOut / duration) },
      { opacity: fadeOut ? 0 : 1, offset: 1 }
    ], { duration, fill: 'forwards' });
  };

  const savedSha = server?.style_sha256;
  const matchedAss = (variants.data?.variants ?? []).filter(variant =>
    variant.revision_id === revisionId && variant.spec_id === 'ass-basic-v2' && variant.style_sha256 === (chain.styleSha ?? savedSha) && !variant.invalidated);
  const assView = matchedAss.find(variant => variant.variant_id === chain.variantId)
    ?? matchedAss.find(variant => variant.status === 'succeeded')
    ?? matchedAss.find(variant => ACTIVE.has(variant.status));
  const matchedMp4 = (previews.data?.previews ?? []).filter(preview =>
    preview.revision_id === revisionId && preview.subtitle_variant_id === assView?.variant_id && preview.style_sha256 === assView?.style_sha256 && !preview.invalidated);
  const mp4View = matchedMp4.find(preview => preview.preview_id === chain.previewId)
    ?? matchedMp4.find(preview => preview.status === 'succeeded')
    ?? matchedMp4.find(preview => ACTIVE.has(preview.status));

  const runChain = (through: 'ass' | 'mp4', explicitRetry: boolean) => {
    if (!active || !document || !server || errors.length > 0 || asset?.locked) return;
    const token = ++tokenRef.current;
    const frozenContext = context;
    const reuse = !explicitRetry && requestsRef.current?.context === frozenContext && chain.phase === 'failed';
    const previous = reuse ? requestsRef.current?.requests : undefined;
    void runStyledExport({
      isCurrent: () => tokenRef.current === token,
      revisionId,
      draftVersion: active.baseDraftVersion,
      document,
      through,
      explicitRetry,
      reuseRequests: reuse,
      previousRequests: previous,
      requestId: () => crypto.randomUUID(),
      wait: (ms) => new Promise(resolve => { setTimeout(resolve, ms); }),
      save: (body) => subtitleStyleApi.save(runId, body),
      onDraftSaved: (saved) => {
        const savedKey = styleCacheKey(saved.asset_id, saved.run_id, saved.revision_id, saved.document.source_srt_sha256);
        const base = sessionRef.current?.key === savedKey
          ? sessionRef.current
          : recallLocalDraft(savedKey) ?? { document: saved.document, undo: [], baseDraftVersion: saved.draft_version };
        const editedSinceSend = canonicalJSON(base.document) !== canonicalJSON(document);
        const next = editedSinceSend
          ? { document: base.document, undo: base.undo, baseDraftVersion: saved.draft_version }
          : noteSaveOutcome(base, { ok: true, server: saved });
        rememberLocalDraft(savedKey, next);
        if (tokenRef.current !== token) return;
        setSession(existing => existing?.key === savedKey ? { ...existing, ...next } : existing);
        void client.invalidateQueries({ queryKey: ['subtitleStyle', saved.asset_id, saved.run_id, saved.revision_id] });
      },
      listVariants: () => deliveryVariantApi.list(runId),
      startVariant: (body) => deliveryVariantApi.start(runId, body),
      readVariant: (id) => deliveryVariantApi.get(id),
      listPreviews: () => previewApi.list(runId),
      startPreview: (body) => previewApi.start(runId, body),
      readPreview: (id) => previewApi.get(id),
      onProgress: (progress) => {
        if (tokenRef.current !== token) return;
        if (progress.requests) requestsRef.current = { context: frozenContext, requests: progress.requests };
        setChain(progress);
        void client.invalidateQueries({ queryKey: variantQueryKey(assetId, runId, revisionId) });
        void client.invalidateQueries({ queryKey: previewQueryKey(assetId, runId, revisionId) });
      }
    }).then(result => {
      if (tokenRef.current !== token) return;
      if (result.requests) requestsRef.current = { context: frozenContext, requests: result.requests };
      setChain(result);
      if (result.phase === 'failed') setSaveError(result.message);
    });
  };

  const saveOnly = async () => {
    if (!active || !document || errors.length > 0 || asset?.locked) return;
    const token = ++tokenRef.current;
    const frozenKey = active.key;
    setSaveError(null);
    setChain({ phase: 'saving', message: '正在保存样式草稿', progress: 0 });
    try {
      const saved = await subtitleStyleApi.save(runId, {
        revision_id: revisionId,
        expected_draft_version: active.baseDraftVersion,
        document
      });
      const savedKey = styleCacheKey(saved.asset_id, saved.run_id, saved.revision_id, saved.document.source_srt_sha256);
      const base = sessionRef.current?.key === savedKey ? sessionRef.current : { document, undo: active.undo, baseDraftVersion: active.baseDraftVersion };
      const editedSinceSend = canonicalJSON(base.document) !== canonicalJSON(document);
      const next = editedSinceSend
        ? { document: base.document, undo: base.undo, baseDraftVersion: saved.draft_version }
        : noteSaveOutcome(base, { ok: true, server: saved });
      rememberLocalDraft(savedKey, next);
      if (tokenRef.current !== token || savedKey !== frozenKey) return;
      setSession(existing => existing && existing.key === savedKey ? { ...existing, ...next } : existing);
      setChain({ phase: 'done', message: '草稿已保存', progress: 1, styleSha: saved.style_sha256, draftVersion: saved.draft_version });
      void client.invalidateQueries({ queryKey: ['subtitleStyle', assetId, runId, revisionId] });
    } catch (error) {
      if (tokenRef.current !== token) return;
      setSaveError(errorText(error));
      setChain({ phase: 'failed', message: errorText(error), progress: 0 });
    }
  };

  const reloadServer = () => {
    if (!server || !currentKey) return;
    tokenRef.current += 1;
    const next = { key: currentKey, document: structuredClone(server.document), undo: [], baseDraftVersion: server.draft_version };
    rememberLocalDraft(currentKey, next);
    setSession(next);
    setSaveError(null);
    setChain(idleChain);
  };

  const cancelChain = () => {
    const variantId = chain.variantId ?? assView?.variant_id;
    const previewId = chain.previewId ?? mp4View?.preview_id;
    const phase = chain.phase === 'mp4' || ACTIVE.has(mp4View?.status ?? '') ? 'mp4'
      : chain.phase === 'ass' || ACTIVE.has(assView?.status ?? '') ? 'ass' : chain.phase;
    const stoppedToken = ++tokenRef.current;
    setChain({ phase: 'stopped', message: '已停止后续步骤', progress: 0, variantId, previewId });
    const cancellation = phase === 'mp4' && previewId ? previewApi.cancel(previewId)
      : variantId && phase === 'ass' ? deliveryVariantApi.cancel(variantId) : undefined;
    void cancellation?.catch(error => { if (tokenRef.current === stoppedToken) setSaveError(errorText(error)); });
  };

  const patchStyle = (key: StyleField, value: SubtitleStyle[StyleField]) => {
    if (!document) return;
    if (scope === 'global') edit(setGlobalField(document, key, value));
    else if (selected.length > 0) edit(setCueStyles(document, selected, { [key]: value }));
  };

  const fieldValue = (key: StyleField): SubtitleStyle[StyleField] | undefined => {
    if (!document) return undefined;
    if (scope === 'global') return document.style[key];
    return sharedStyleValue(document, cues, selected, key);
  };

  const selectCue = (cueId: number, shift: boolean) => {
    if (shift && anchorRef.current != null) {
      const ids = cues.map(cue => cue.cue_id);
      const from = ids.indexOf(anchorRef.current);
      const to = ids.indexOf(cueId);
      if (from >= 0 && to >= 0) {
        const [start, end] = from < to ? [from, to] : [to, from];
        setSelected(ids.slice(start, end + 1));
        return;
      }
    }
    anchorRef.current = cueId;
    setSelected([cueId]);
  };

  const placement = previewEffective ? alignmentPlacement(previewEffective.style.alignment) : undefined;
  const playX = document?.play_res_x ?? 1280;

  return <Modal
    open={open}
    onCancel={onClose}
    footer={null}
    width="92vw"
    style={{ maxWidth: 1120, top: 16 }}
    destroyOnHidden
    title="ASS 样式编辑"
    className="ass-editor-modal"
  >
    {!server || !document || !active ? <Typography.Text type="secondary">{draftQuery.isError ? errorText(draftQuery.error) : '正在读取字幕草稿…'}</Typography.Text> : (
      <div className="ass-editor-layout" data-testid="ass-editor">
        <div className="ass-cue-column" data-testid="ass-cue-list">
          <Flex justify="space-between" align="center" gap={8}>
            <Checkbox
              checked={selected.length === cues.length && cues.length > 0}
              indeterminate={selected.length > 0 && selected.length < cues.length}
              onChange={(event) => setSelected(event.target.checked ? cues.map(cue => cue.cue_id) : [])}
            >全选</Checkbox>
            <Typography.Text type="secondary">{selected.length} 条已选</Typography.Text>
          </Flex>
          <div className="ass-cue-scroll">
            {cues.map(cue => <CueRow key={cue.cue_id} cue={cue} document={document} selected={selected.includes(cue.cue_id)}
              onToggle={(checked) => setSelected(current => checked ? [...current, cue.cue_id] : current.filter(id => id !== cue.cue_id))}
              onSelect={(shift) => selectCue(cue.cue_id, shift)} />)}
          </div>
        </div>
        <Flex vertical gap={12} style={{ minWidth: 0 }}>
          <Typography.Text data-testid="ass-schematic-note" type="secondary">
            右侧画面是脚本坐标里的示意，用浏览器字体近似描边和位置。它不是 FFmpeg 烧录成片；换行、字距和实际字形以生成后的 MP4 为准。
          </Typography.Text>
          <div className="ass-stage" data-testid="ass-stage" style={{ aspectRatio: `${playX} / 720` }}>
            {previewEffective && <div className="ass-caption-placement" style={{
              ...placement,
              top: `${previewEffective.style.margin_v / 720 * 100}%`, bottom: `${previewEffective.style.margin_v / 720 * 100}%`,
              left: `${previewEffective.style.margin_l / playX * 100}%`, right: `${previewEffective.style.margin_r / playX * 100}%`
            }}><div ref={captionRef} className="ass-caption" style={{
              color: previewEffective.style.primary_color,
              fontFamily: previewEffective.style.font_family,
              fontSize: `${previewEffective.style.font_size / playX * 100}cqw`,
              fontWeight: previewEffective.style.bold ? 700 : 400,
              fontStyle: previewEffective.style.italic ? 'italic' : 'normal',
              textDecoration: previewEffective.style.underline ? 'underline' : 'none',
              WebkitTextStroke: `${previewEffective.style.outline / playX * 100}cqw ${previewEffective.style.outline_color}`,
              textShadow: `${previewEffective.style.shadow / playX * 100}cqw ${previewEffective.style.shadow / playX * 100}cqw 0 ${previewEffective.style.outline_color}`
            }}>{previewEffective.text}</div></div>}
          </div>
          <Space wrap>
            <Button data-testid="ass-play-schematic" disabled={!previewEffective || errors.length > 0} onClick={playSchematic}>播放字幕渐入渐出</Button>
            <Button data-testid="ass-scope-global" type={scope === 'global' ? 'primary' : 'default'} onClick={() => setScope('global')}>全局继承</Button>
            <Button data-testid="ass-scope-cues" type={scope === 'cues' ? 'primary' : 'default'} onClick={() => setScope('cues')}>逐条覆盖</Button>
            {stylePresets().map(preset => <Button key={preset.id} data-testid={`ass-preset-${preset.id}`} onClick={() => edit(applyPreset(document, scope === 'global' ? { kind: 'global' } : { kind: 'cues', cueIds: selected }, preset.style))}>{preset.name}</Button>)}
          </Space>
          {scope === 'cues' && selected.length === 0 && <Alert type="info" showIcon message="先选择一条或多条字幕，样式修改会写成这些条目的覆盖。" />}
          <div className="ass-style-grid">
            <label>字体<Select data-testid="ass-font-family" showSearch value={asText(fieldValue('font_family'))} placeholder={fieldValue('font_family') == null && scope === 'cues' ? '多项' : '选择字体'}
              options={fontOptions(fontQuery.data ?? [], document.style.font_family)} onChange={(value) => patchStyle('font_family', value)} disabled={scope === 'cues' && selected.length === 0} /></label>
            <NumberField testId="ass-font-size" label="字号" min={12} max={120} step={1} value={asNumber(fieldValue('font_size'))} mixed={scope === 'cues' && selected.length > 0 && fieldValue('font_size') == null} disabled={scope === 'cues' && selected.length === 0} onChange={(value) => patchStyle('font_size', value)} />
            <label data-testid="ass-font-size-slider">字号滑块<Slider min={12} max={120} step={1} value={asNumber(fieldValue('font_size')) ?? 36}
              ariaLabelForHandle="字幕字号" disabled={scope === 'cues' && selected.length === 0} onChange={value => patchStyle('font_size', value)} /></label>
            <label className="ass-checks"><Checkbox checked={fieldValue('bold') === true} indeterminate={scope === 'cues' && selected.length > 0 && fieldValue('bold') == null} onChange={(event) => patchStyle('bold', event.target.checked)}>粗体</Checkbox>
              <Checkbox checked={fieldValue('italic') === true} indeterminate={scope === 'cues' && selected.length > 0 && fieldValue('italic') == null} onChange={(event) => patchStyle('italic', event.target.checked)}>斜体</Checkbox>
              <Checkbox checked={fieldValue('underline') === true} indeterminate={scope === 'cues' && selected.length > 0 && fieldValue('underline') == null} onChange={(event) => patchStyle('underline', event.target.checked)}>下划线</Checkbox></label>
            <NumberField testId="ass-outline" label="描边" min={0} max={10} step={0.01} value={asNumber(fieldValue('outline'))} mixed={scope === 'cues' && selected.length > 0 && fieldValue('outline') == null} disabled={scope === 'cues' && selected.length === 0} onChange={(value) => patchStyle('outline', value)} />
            <NumberField testId="ass-shadow" label="阴影" min={0} max={10} step={0.01} value={asNumber(fieldValue('shadow'))} mixed={scope === 'cues' && selected.length > 0 && fieldValue('shadow') == null} disabled={scope === 'cues' && selected.length === 0} onChange={(value) => patchStyle('shadow', value)} />
            <ColorField testId="ass-primary-color" label="填充色" value={asText(fieldValue('primary_color'))} onChange={(value) => patchStyle('primary_color', value)} />
            <ColorField testId="ass-outline-color" label="描边色" value={asText(fieldValue('outline_color'))} onChange={(value) => patchStyle('outline_color', value)} />
            <NumberField testId="ass-margin-l" label="左边距" min={0} max={playX - 1} step={1} value={asNumber(fieldValue('margin_l'))} mixed={scope === 'cues' && selected.length > 0 && fieldValue('margin_l') == null} disabled={scope === 'cues' && selected.length === 0} onChange={(value) => patchStyle('margin_l', value)} />
            <NumberField testId="ass-margin-r" label="右边距" min={0} max={playX - 1} step={1} value={asNumber(fieldValue('margin_r'))} mixed={scope === 'cues' && selected.length > 0 && fieldValue('margin_r') == null} disabled={scope === 'cues' && selected.length === 0} onChange={(value) => patchStyle('margin_r', value)} />
            <NumberField testId="ass-margin-v" label="垂直边距" min={0} max={719} step={1} value={asNumber(fieldValue('margin_v'))} mixed={scope === 'cues' && selected.length > 0 && fieldValue('margin_v') == null} disabled={scope === 'cues' && selected.length === 0} onChange={(value) => patchStyle('margin_v', value)} />
            <NumberField testId="ass-fade-in" label="渐入毫秒" min={0} step={1} value={asNumber(fieldValue('fade_in_ms'))} mixed={scope === 'cues' && selected.length > 0 && fieldValue('fade_in_ms') == null} disabled={scope === 'cues' && selected.length === 0} onChange={(value) => patchStyle('fade_in_ms', value)} />
            <NumberField testId="ass-fade-out" label="渐出毫秒" min={0} step={1} value={asNumber(fieldValue('fade_out_ms'))} mixed={scope === 'cues' && selected.length > 0 && fieldValue('fade_out_ms') == null} disabled={scope === 'cues' && selected.length === 0} onChange={(value) => patchStyle('fade_out_ms', value)} />
          </div>
          <div className="ass-align-grid" data-testid="ass-align-grid">
            {[7, 8, 9, 4, 5, 6, 1, 2, 3].map(alignment => <Button key={alignment} data-testid={`ass-align-${alignment}`} type={fieldValue('alignment') === alignment ? 'primary' : 'default'}
              disabled={scope === 'cues' && selected.length === 0} onClick={() => patchStyle('alignment', alignment)}>{alignment}</Button>)}
          </div>
          <Typography.Text type="secondary">九宫格和边距使用脚本坐标（高 720，宽 {playX}）。全局修改只写继承样式；已覆盖的字段保持原样。时间只改当前这一条。</Typography.Text>
          {scope === 'cues' && overriddenFields.length > 0 && <Space wrap aria-label="选中字幕的覆盖字段">
            {overriddenFields.map(field => <Tag key={field} data-testid={`ass-override-${field}`}>{STYLE_LABELS[field]}已覆盖</Tag>)}
          </Space>}
          {singleEffective && singleId != null && <Space wrap>
            <NumberField testId="ass-start-ms" label="开始毫秒" min={0} max={server.duration_ms} step={1} value={singleEffective.start_ms} onChange={(value) => edit(setCueStart(document, cues, singleId, value))} />
            <NumberField testId="ass-end-ms" label="结束毫秒" min={0} max={server.duration_ms} step={1} value={singleEffective.end_ms} onChange={(value) => edit(setCueTiming(document, singleId, { end_ms: value }))} />
            <NumberField testId="ass-duration-ms" label="时长毫秒" min={1} step={1} value={singleEffective.end_ms - singleEffective.start_ms} onChange={(value) => edit(setCueDuration(document, cues, singleId, value))} />
          </Space>}
          {selected.length !== 1 && <Typography.Text type="secondary">选择单条字幕后才能改时间和时长。</Typography.Text>}
          <Space wrap>
            <Button data-testid="ass-undo" disabled={active.undo.length === 0 || running} onClick={() => {
              const previous = undoEdit(active.undo, active.document);
              if (previous) setSession({ ...active, document: previous.current, undo: previous.undo });
            }}>撤销</Button>
            <Button data-testid="ass-restore" disabled={selected.length === 0 || running} onClick={() => edit(restoreInheritance(document, selected))}>恢复继承</Button>
            <Button data-testid="ass-apply-all" disabled={running} onClick={() => edit(applyStyleToAll(document, document.style))}>应用全局样式到全部字幕</Button>
            <Button data-testid="ass-save" type="primary" disabled={running || errors.length > 0 || !!asset?.locked} loading={chain.phase === 'saving'} onClick={() => { void saveOnly(); }}>保存草稿</Button>
            <Button data-testid="ass-reload-server" disabled={!dirty && !versionConflict} onClick={reloadServer}>放弃未保存并重新载入</Button>
          </Space>
          {dirty && <Tag color="gold">有未保存修改</Tag>}
          {dirty && (assView || mp4View) && <Alert data-testid="ass-old-style" type="warning" message="下方产物是旧样式版本；保存并重新生成后才会应用当前修改。" />}
          {fontUnavailable && <Alert type={missingFonts.length ? 'error' : 'info'} message={missingFonts.length ? `字体不可用：${missingFonts.join('、')}` : '正在读取可用字体目录…'} />}
          {overlapping.length > 0 && <Alert data-testid="ass-overlap" type="warning" message={`字幕时间存在重叠（#${overlapping.slice(0, 8).join('、#')}${overlapping.length > 8 ? '等' : ''}）；仍可保存，请检查成片时序。`} />}
          {versionConflict && <Alert type="warning" showIcon message={`本地草稿基于版本 ${active.baseDraftVersion}，服务端已是 ${server.draft_version}。保存会得到冲突，当前输入仍保留。`} />}
          {errors.map(error => <Alert key={error} type="error" showIcon message={error} />)}
          {(saveError || draftQuery.error || fontQuery.error) && <Alert type="error" showIcon message={saveError ?? errorText(draftQuery.error ?? fontQuery.error)} />}
          {chain.phase !== 'idle' && chain.message && <div>
            <Typography.Text>{chain.message}</Typography.Text>
            {(chain.phase === 'ass' || chain.phase === 'mp4' || chain.phase === 'saving') && <Progress percent={Math.round(chain.progress * 100)} />}
          </div>}
          <JobBar
            kind="ASS"
            view={assView}
            running={running}
            onStart={() => runChain('ass', false)}
            onRetry={() => runChain('ass', true)}
            onCancel={cancelChain}
            cancelVisible={Boolean(chain.phase === 'ass' && chain.variantId && chain.variant?.status !== 'succeeded') || ACTIVE.has(assView?.status ?? '')}
            disabled={running || errors.length > 0 || fontUnavailable || !!asset?.locked || variants.data?.can_start_editable === false}
            downloads={assView?.status === 'succeeded' ? <>
              <Typography.Link href={variantFileURL(assView.variant_id, 'ass')} download>下载 ASS</Typography.Link>
              <Typography.Link href={variantFileURL(assView.variant_id, 'srt')} download>下载原 SRT</Typography.Link>
              <Typography.Link href={variantZIPURL(assView.variant_id)} download>下载交付包</Typography.Link>
            </> : null}
          />
          <JobBar
            kind="MP4"
            view={mp4View}
            running={running}
            onStart={() => runChain('mp4', false)}
            onRetry={() => runChain('mp4', true)}
            onCancel={cancelChain}
            cancelVisible={Boolean(chain.phase === 'mp4' && chain.previewId) || ACTIVE.has(mp4View?.status ?? '')}
            disabled={running || errors.length > 0 || fontUnavailable || !!asset?.locked || variants.data?.can_start_editable === false}
            downloads={mp4View?.status === 'succeeded' ? <>
              <Typography.Link href={previewFileURL(mp4View.preview_id, 'mp4', true)} download>下载 MP4</Typography.Link>
              <Button size="small" onClick={() => onSelectPreview?.(createPreviewSource(mp4View))}>在监视器播放</Button>
            </> : null}
          />
          {mp4View?.status === 'succeeded' && <Alert showIcon type={mp4View.basic_quality?.status === 'passed' ? 'success' : 'warning'} message={mp4View.basic_quality?.status === 'passed' ? '基础检验通过' : mp4View.basic_quality?.status === 'failed' ? '基础检验发现异常，仍可交付' : '基础检验未完成'} description={mp4View.basic_quality?.issues.map(i=>`${i.kind === 'black' ? '黑屏' : '静音'} ${i.start.toFixed(1)}–${i.end.toFixed(1)} 秒`).join('、')} />}
          {mp4View?.status === 'succeeded' && <video ref={bindPlayer} data-testid="ass-editor-player" className="ass-editor-player" controls preload="metadata" src={previewFileURL(mp4View.preview_id, 'mp4')} />}
          {variants.data?.can_start_editable === false && <Alert type="info" showIcon message="当前导出还不能生成可编辑 ASS" description={variants.data.checks.filter(check => check.status !== 'passed').map(check => check.message || check.code).join('；')} />}
          <Typography.Text type="secondary">生成 MP4 会先保存草稿，再复用或创建 style hash 相同的 ASS，成功后才烧录这一条字幕版本。切换资产或导出版本会停下尚未发出的后续步骤。</Typography.Text>
        </Flex>
      </div>
    )}
  </Modal>;
}

function CueRow({ cue, document, selected, onToggle, onSelect }: {
  cue: SourceCue; document: StyleDocument; selected: boolean;
  onToggle: (checked: boolean) => void; onSelect: (shift: boolean) => void;
}) {
  const effective = effectiveCue(cue, document);
  const override = document.overrides.find(item => item.cue_id === cue.cue_id);
  return <div className={`ass-cue${selected ? ' is-selected' : ''}`} onClick={(event) => onSelect(event.shiftKey)}>
    <Checkbox checked={selected} onClick={(event) => event.stopPropagation()} onChange={(event) => onToggle(event.target.checked)} />
    <div>
      <Space size={4} wrap>
        <Typography.Text type="secondary">#{cue.cue_id}</Typography.Text>
        <Typography.Text type="secondary">{formatCueMs(effective.start_ms)} – {formatCueMs(effective.end_ms)}</Typography.Text>
        {override?.style && <Tag>样式覆盖</Tag>}
        {(override?.start_ms != null || override?.end_ms != null) && <Tag>时间覆盖</Tag>}
      </Space>
      <Typography.Paragraph className="ass-cue-text" ellipsis={{ rows: 2 }}>{cue.text}</Typography.Paragraph>
    </div>
  </div>;
}

function NumberField({ testId, label, value, onChange, mixed, disabled, min, max, step }: {
  testId: string; label: string; value: number | undefined; onChange: (value: number) => void;
  mixed?: boolean; disabled?: boolean; min?: number; max?: number; step: number;
}) {
  return <label>{label}<InputNumber data-testid={testId} value={typeof value === 'number' ? value : null} placeholder={mixed ? '多项' : undefined}
    disabled={disabled} min={min} max={max} step={step} onChange={(next) => { if (typeof next === 'number') onChange(next); }} /></label>;
}

function ColorField({ testId, label, value, onChange }: { testId: string; label: string; value?: string; onChange: (value: string) => void }) {
  const hex = value && /^#[0-9a-f]{6}$/i.test(value) ? value : '#000000';
  return <label>{label}<Space.Compact>
    <input aria-label={`${label}取色器`} data-testid={testId} type="color" value={hex} onChange={(event) => onChange(event.target.value)} />
    <Input aria-label={`${label} HEX`} data-testid={`${testId}-hex`} value={value ?? ''} placeholder="#RRGGBB" maxLength={7}
      status={value && !/^#[0-9a-f]{6}$/i.test(value) ? 'error' : undefined} onChange={event => onChange(event.target.value.toUpperCase())} />
  </Space.Compact></label>;
}

function asNumber(value: SubtitleStyle[StyleField] | undefined): number | undefined {
  return typeof value === 'number' ? value : undefined;
}

function asText(value: SubtitleStyle[StyleField] | undefined): string | undefined {
  return typeof value === 'string' ? value : undefined;
}

function fontOptions(fonts: Array<{ family: string }>, current: string) {
  const families = [...new Set([current, ...fonts.map(font => font.family)].filter(Boolean))];
  return families.map(family => ({ value: family, label: family }));
}

function JobBar({ kind, view, running, onStart, onRetry, onCancel, cancelVisible, disabled, downloads }: {
  kind: 'ASS' | 'MP4';
  view?: VariantView | PreviewView;
  running: boolean;
  onStart: () => void;
  onRetry: () => void;
  onCancel: () => void;
  cancelVisible: boolean;
  disabled: boolean;
  downloads: ReactNode;
}) {
  const status = view?.status;
  return <Flex vertical gap={6} data-testid={kind === 'ASS' ? 'ass-job' : 'mp4-job'}>
    <Space wrap>
      <Button type="primary" data-testid={kind === 'ASS' ? 'ass-generate' : 'mp4-generate'} disabled={disabled} loading={running} onClick={onStart}>
        {kind === 'ASS' ? '生成 ASS' : '生成 MP4'}
      </Button>
      <Button data-testid={kind === 'ASS' ? 'ass-retry' : 'mp4-retry'} disabled={disabled} onClick={onRetry}>重试</Button>
      {cancelVisible && <Button danger data-testid={kind === 'ASS' ? 'ass-cancel' : 'mp4-cancel'} onClick={onCancel}>取消</Button>}
      {status && <Tag>{previewStyleLabel(view ?? {})}</Tag>}
      {status && <Tag>{status}</Tag>}
      {downloads}
    </Space>
    {view?.message && <Typography.Text type={status === 'failed' ? 'danger' : 'secondary'}>{view.message}</Typography.Text>}
  </Flex>;
}
