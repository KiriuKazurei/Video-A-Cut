import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Badge, Button, Table, Typography } from 'antd';
import { TimelineInspector } from './TimelineInspector';
import { SkillRunModule } from './SkillRunModule';
import { CodeClipWorkbench } from './CodeClipWorkbench';
import { ScriptWorkspace } from '../workspaces/ScriptWorkspace';
import { useSequenceEditor } from '../sequenceEditor';
import { useSkillWorkspace } from '../skillWorkspace';
import { skillApi } from '../skillApi';
import { skillQueryKeys } from '../skillContracts';
import { protectedQueryRetry } from '../skillView';
import type { RouteState } from '../navigation';

export type DockModuleId = 'script' | 'properties' | 'skill' | 'history';
const MODULES: { id: DockModuleId; label: string }[] = [
  { id: 'script', label: '脚本' },
  { id: 'properties', label: '属性' },
  { id: 'skill', label: 'Skill 任务' },
  { id: 'history', label: '编辑历史' },
];
const ACTIVE_RUN = new Set(['queued', 'running', 'waiting_human']);

/** Editing dock: script stays mounted, code clips open inside properties, timeline stays outside this dock. */
export function EditorRightDock({ navigate, active, route, assetId, stripOnly = false, onExpand, onCollapse }: {
  navigate: (state: Partial<RouteState>) => void;
  active: boolean;
  route: RouteState;
  assetId?: string;
  stripOnly?: boolean;
  onExpand?: () => void;
  onCollapse?: () => void;
}) {
  const e = useSequenceEditor();
  const selectedRun = useSkillWorkspace().runId;
  const [open, setOpen] = useState<Record<DockModuleId, boolean>>({ script: false, properties: !selectedRun, skill: !!selectedRun, history: false });
  const [scriptFocus, setScriptFocus] = useState(false);
  const runs = useQuery({ queryKey: e.projectId ? skillQueryKeys.runs(e.projectId) : ['vu-runs', 'dock-idle'],
    queryFn: ({ signal }) => skillApi.runs(e.projectId, signal), enabled: Boolean(e.projectId) && active,
    refetchInterval: active ? 2500 : false, retry: protectedQueryRetry });
  const count = (runs.data ?? []).filter(item => ACTIVE_RUN.has(item.state)).length;
  const codeSelected = !!e.view?.sequence.tracks.some(track => track.kind === 'code' && track.clips.some(clip => e.selected.includes(clip.id)));
  const expanded = (id: DockModuleId) => !stripOnly && (scriptFocus ? id === 'script' : open[id]);
  useEffect(() => { if (active && selectedRun && !scriptFocus) { setOpen(prev => ({ ...prev, skill: true })); if (stripOnly) onExpand?.(); } }, [active, selectedRun, scriptFocus, stripOnly, onExpand]);
  useEffect(() => {
    const openScript = () => {
      if (!active) return;
      setOpen(prev => ({ ...prev, script: true }));
      onExpand?.();
    };
    window.addEventListener('vac:open-script-dock', openScript);
    return () => window.removeEventListener('vac:open-script-dock', openScript);
  }, [active, onExpand]);
  return <aside className={`editor-right-dock${stripOnly ? ' is-strip-only' : ''}${scriptFocus ? ' is-script-focus' : ''}`} aria-label="编辑器侧栏">
    {MODULES.map(mod => {
      const isOpen = expanded(mod.id);
      const badge = mod.id === 'skill' && count > 0 && !isOpen;
      return <section key={mod.id} className={`editor-dock-module${isOpen ? ' is-open' : ''}${mod.id === 'script' ? ' is-script' : ''}`}>
        <Button size="small" type="text" block className="editor-dock-strip" aria-expanded={isOpen} aria-label={stripOnly && mod.id === 'script' ? '展开脚本侧栏' : undefined} title={mod.label} onClick={() => {
          if (scriptFocus && mod.id !== 'script') setScriptFocus(false);
          setOpen(prev => stripOnly
            ? { script: mod.id === 'script', properties: mod.id === 'properties', skill: mod.id === 'skill', history: mod.id === 'history' }
            : { ...prev, [mod.id]: !prev[mod.id] });
          if (stripOnly) onExpand?.();
        }}>
          <span className="editor-dock-strip-label">{mod.label}</span>
          {badge && <Badge count={count} size="small" aria-label={`${count} 个 Skill 任务进行中`} />}
        </Button>
        <div className="editor-dock-body" hidden={!isOpen}>
          {mod.id === 'script' && <>
            <Button size="small" aria-pressed={scriptFocus} onClick={() => { setScriptFocus(value => !value); setOpen(prev => ({ ...prev, script: true })); onExpand?.(); }}>{scriptFocus ? '退出专注' : '专注脚本'}</Button>
            <ScriptWorkspace route={route} navigate={navigate} assetId={assetId} active={active && isOpen} dock />
          </>}
          {mod.id === 'properties' && <>
            <TimelineInspector />
            {codeSelected && <CodeClipWorkbench active={active && isOpen} navigate={navigate} />}
          </>}
          {mod.id === 'skill' && <SkillRunModule navigate={navigate} active={active && isOpen} />}
          {mod.id === 'history' && <Table size="small" pagination={false} rowKey="id" locale={{ emptyText: '暂无编辑历史' }} dataSource={e.view?.history ?? []} columns={[
            { title: '操作', dataIndex: 'label' }, { title: '来源', dataIndex: 'actor' }, { title: '状态', dataIndex: 'applied', render: (value: boolean) => value ? '已应用' : '已撤销' },
          ]} />}
        </div>
      </section>;
    })}
    {!stripOnly && <Button type="text" size="small" aria-label="收起脚本侧栏" onClick={() => onCollapse?.()}>收起侧栏</Button>}
  </aside>;
}
