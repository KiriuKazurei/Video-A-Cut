import { useEffect, useMemo, useState } from 'react';
import { Alert, Button, Flex, Radio, Tabs, Tag, Typography } from 'antd';
import { useProjectWorkspace } from '../projectWorkspace';
import { useSequenceEditor } from '../sequenceEditor';
import type { RouteState } from '../navigation';
import {
  DRAW_HANDOFF_ACTIONS,
  DRAW_HANDOFF_COPY,
  DRAW_HANDOFF_STUB,
  DRAW_ZONES,
  PHASE1_COMPOSITIONS,
  drawAgentContext,
  requestDrawHandoff,
  zoneForFocus,
  type DrawComposition,
  type DrawEngine,
  type DrawFocus,
  type DrawHandoffAction
} from '../drawContracts';

interface Props {
  route: RouteState;
  navigate: (state: Partial<RouteState>) => void;
}

const ENGINE_LABEL: Record<DrawEngine, string> = {
  hyperframes: 'HyperFrames',
  remotion: 'Remotion'
};

function compositionsFor(engine: DrawEngine, slot: string | undefined): DrawComposition[] {
  const list = PHASE1_COMPOSITIONS.filter((item) => item.engine === engine);
  if (slot && !list.some((item) => item.id === slot) && !PHASE1_COMPOSITIONS.some((item) => item.id === slot && item.engine !== engine)) {
    list.unshift({ id: slot, name: slot, engine, durationFrames: 90, fps: 30, rendered: false });
  }
  if (slot && PHASE1_COMPOSITIONS.some((item) => item.id === slot && item.engine !== engine)) {
    const linked = PHASE1_COMPOSITIONS.find((item) => item.id === slot)!;
    if (!list.some((item) => item.id === linked.id)) list.unshift(linked);
  }
  return list;
}

/**
 * DRAW01 Phase 1 studio shell.
 * Four zones follow HyperFrames Studio / Remotion Studio. The bottom ruler is
 * composition time, not the Premiere multi-track editor. Lint, render, and the
 * three edit handoffs report 方案接线中 and never surface a shell.
 */
export function DrawWorkspace({ route, navigate }: Props) {
  const project = useProjectWorkspace();
  const editor = useSequenceEditor();
  const openProject = project.session?.document?.project.id ?? '';
  const engine = route.drawEngine ?? 'hyperframes';
  const focus = route.drawFocus;
  const zone = zoneForFocus(focus);
  const [sourceTab, setSourceTab] = useState(focus === 'source' ? 'source' : 'compositions');
  const [inspectorTab, setInspectorTab] = useState(focus === 'lint' ? 'lint' : focus === 'render-queue' ? 'queue' : 'inspector');
  const [notice, setNotice] = useState('');
  const compositions = useMemo(() => compositionsFor(engine, route.drawSlot), [engine, route.drawSlot]);
  const selected = compositions.find((item) => item.id === route.drawSlot) ?? compositions[0];
  const mismatch = Boolean(route.drawProject && openProject && route.drawProject !== openProject);

  useEffect(() => {
    if (focus === 'source') setSourceTab('source');
    if (focus === 'lint') setInspectorTab('lint');
    if (focus === 'render-queue') setInspectorTab('queue');
    if (focus === 'inspector') setInspectorTab('inspector');
    const node = document.querySelector<HTMLElement>(`[data-draw-zone="${zone}"]`);
    node?.focus();
  }, [focus, zone]);

  const setEngine = (next: DrawEngine) => {
    navigate({ workspace: 'draw', drawEngine: next, drawFocus: focus ?? 'canvas' });
  };
  const selectSlot = (id: string) => {
    const item = compositions.find((entry) => entry.id === id);
    navigate({
      workspace: 'draw',
      drawProject: openProject || route.drawProject || '',
      drawSlot: id,
      drawEngine: item?.engine ?? engine,
      drawFocus: 'canvas'
    });
  };
  const handoff = (action: DrawHandoffAction) => {
    const slots = action === 'merge_render' ? compositions.map((item) => item.id) : selected ? [selected.id] : [];
    const result = requestDrawHandoff({
      action,
      project_id: openProject,
      skill_run_id: route.drawRun,
      slot_ids: slots,
      sequence_id: editor.sequenceId || undefined,
      engine: selected?.engine ?? engine
    });
    setNotice(`${DRAW_HANDOFF_COPY[action].label}：${result.message}`);
  };
  const copyContext = () => {
    const text = drawAgentContext({
      project_id: openProject || route.drawProject,
      skill_run_id: route.drawRun,
      slot_id: selected?.id,
      sequence_id: editor.sequenceId,
      engine: selected?.engine ?? engine,
      focus
    });
    void navigator.clipboard?.writeText(text).then(
      () => setNotice('已复制绘制上下文。沙箱与渲染仍为方案接线中。'),
      () => setNotice(text)
    );
  };
  const stubTool = (label: string) => setNotice(`${label}：${DRAW_HANDOFF_STUB}`);

  const sourceItems = engine === 'remotion'
    ? [
        { key: 'compositions', label: 'Compositions' },
        { key: 'layers', label: '图层' },
        { key: 'source', label: '组件源码' }
      ]
    : [
        { key: 'compositions', label: '构图' },
        { key: 'layers', label: '图层' },
        { key: 'source', label: '源码' }
      ];

  return (
    <div className="draw-studio" data-testid="draw-workspace" data-draw-engine={engine} data-draw-focus={zone}>
      <Flex className="draw-toolbar" align="center" gap={8} wrap>
        <Typography.Text strong>绘制</Typography.Text>
        <Radio.Group
          size="small"
          value={engine}
          aria-label="绘制引擎"
          onChange={(event) => setEngine(event.target.value as DrawEngine)}
          options={(['hyperframes', 'remotion'] as const).map((item) => ({ value: item, label: ENGINE_LABEL[item] }))}
        />
        <Button size="small" onClick={() => { setSourceTab('compositions'); setInspectorTab('inspector'); navigate({ workspace: 'draw', drawFocus: 'canvas' }); }}>重置布局</Button>
        <Button size="small" onClick={copyContext}>复制上下文给 Agent</Button>
        <Button size="small" onClick={() => stubTool('Lint')}>Lint</Button>
        <Button size="small" onClick={() => stubTool('Check')}>Check</Button>
        <Button size="small" onClick={() => stubTool('Render')}>Render</Button>
        <Typography.Text type="secondary">预览、Lint 与渲染：{DRAW_HANDOFF_STUB}</Typography.Text>
      </Flex>

      <section className={`draw-zone draw-zone-source${zone === 'source' ? ' is-focused' : ''}`} data-draw-zone="source" tabIndex={-1} aria-label={DRAW_ZONES[0].label}>
        <Tabs size="small" activeKey={sourceTab} onChange={setSourceTab} items={sourceItems.map((item) => ({
          ...item,
          children: item.key === 'compositions' ? (
            <Flex vertical gap={6}>
              {compositions.map((item) => (
                <Button key={item.id} type={item.id === selected?.id ? 'primary' : 'default'} block onClick={() => selectSlot(item.id)} aria-pressed={item.id === selected?.id}>
                  {item.name}
                </Button>
              ))}
              <Typography.Text type="secondary">构图槽只属于绘制页。工程多轨仍在「编辑」。</Typography.Text>
            </Flex>
          ) : item.key === 'layers' ? (
            <Typography.Text type="secondary">{selected?.name ?? '未选择构图'} 的图层树会在预览嵌入后显示。当前为说明占位。</Typography.Text>
          ) : (
            <pre className="draw-source">{engine === 'remotion'
              ? 'Remotion 组件树占位。不在浏览器里执行构建。'
              : 'HTML / CSS 构图占位。不在浏览器里执行命令。'}</pre>
          )
        }))} />
      </section>

      <section className={`draw-zone draw-zone-canvas${zone === 'canvas' ? ' is-focused' : ''}`} data-draw-zone="canvas" tabIndex={-1} aria-label={DRAW_ZONES[1].label}>
        <div className="draw-canvas-frame" role="img" aria-label="构图预览画布">
          <Typography.Text className="draw-canvas-label">{selected?.name ?? '构图'} · {ENGINE_LABEL[selected?.engine ?? engine]}</Typography.Text>
          <Typography.Text className="draw-canvas-status">{DRAW_HANDOFF_STUB}</Typography.Text>
        </div>
        {mismatch && <Alert type="warning" showIcon message="深链里的工程与当前窗口不一致，没有切换工程。" />}
        {!openProject && <Alert type="info" showIcon message="请先打开工程。绘制页可以浏览结构，衔接动作需要当前工程。" />}
        {route.drawRun && <Typography.Text type="secondary">Skill 运行 {route.drawRun}</Typography.Text>}
      </section>

      <section className={`draw-zone draw-zone-timeline${zone === 'timeline' ? ' is-focused' : ''}`} data-draw-zone="timeline" tabIndex={-1} aria-label={DRAW_ZONES[2].label}>
        <Flex justify="space-between" align="center">
          <Typography.Text strong>构图时间轴</Typography.Text>
          <Typography.Text type="secondary">{selected ? `${selected.durationFrames} 帧 · ${selected.fps} fps` : '未选择'}</Typography.Text>
        </Flex>
        <div className="draw-comp-ruler" aria-hidden="true">
          <div className="draw-comp-block">{selected?.name ?? '构图'}</div>
          <div className="draw-playhead" />
        </div>
        <Typography.Text type="secondary">这里只编辑构图源时间，不是 Premiere 式多轨剪辑时间线。</Typography.Text>
      </section>

      <section className={`draw-zone draw-zone-inspector${zone === 'inspector' ? ' is-focused' : ''}`} data-draw-zone="inspector" tabIndex={-1} aria-label={DRAW_ZONES[3].label}>
        <Tabs size="small" activeKey={inspectorTab} onChange={setInspectorTab} items={[
          {
            key: 'inspector',
            label: 'Inspector',
            children: (
              <Flex vertical gap={8}>
                <Typography.Text>{selected?.name ?? '未选择层'}</Typography.Text>
                <Typography.Text type="secondary">选中层的安全属性会在预览嵌入后出现。深度改源码仍走 Agent 或源码面板。</Typography.Text>
                <Tag>未渲染</Tag>
              </Flex>
            )
          },
          {
            key: 'lint',
            label: 'Lint',
            children: <Alert type="info" showIcon message={`Lint / Check：${DRAW_HANDOFF_STUB}`} />
          },
          {
            key: 'queue',
            label: '渲染队列',
            children: <Alert type="info" showIcon message={`渲染队列：${DRAW_HANDOFF_STUB}`} description="成功后的产物才会登记为工程资源。当前没有可下载的 MP4。" />
          }
        ]} />
        <Flex vertical gap={8} className="draw-handoff">
          <Typography.Text strong>与编辑衔接</Typography.Text>
          {DRAW_HANDOFF_ACTIONS.map((action) => (
            <div key={action}>
              <Button block disabled={!openProject || !selected} onClick={() => handoff(action)}>{DRAW_HANDOFF_COPY[action].label}</Button>
              <Typography.Paragraph type="secondary" className="draw-handoff-detail">{DRAW_HANDOFF_COPY[action].detail}</Typography.Paragraph>
            </div>
          ))}
          {notice && <Alert type="info" showIcon message={notice} />}
        </Flex>
      </section>
    </div>
  );
}
