import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Table, Typography } from 'antd';
import { TimelineInspector } from './TimelineInspector';
import { SkillRunModule } from './SkillRunModule';
import { useSequenceEditor } from '../sequenceEditor';
import { useSkillWorkspace } from '../skillWorkspace';
import { skillApi } from '../skillApi';
import { skillQueryKeys } from '../skillContracts';
import { protectedQueryRetry } from '../skillView';
import type { RouteState } from '../navigation';

export type DockModuleId = 'properties' | 'skill' | 'history';
const MODULES: { id: DockModuleId; label: string }[] = [
  { id: 'properties', label: '属性' }, { id: 'skill', label: 'Skill 任务' }, { id: 'history', label: '编辑历史' },
];
const ACTIVE_RUN = new Set(['queued', 'running', 'waiting_human']);

/** Editing properties and tasks. Script authoring has its own workspace. */
export function EditorRightDock({ navigate, active, stripOnly = false, onExpand }: {
  navigate: (state: Partial<RouteState>) => void; active: boolean; stripOnly?: boolean; onExpand?: () => void;
}) {
  const e = useSequenceEditor();
  const selectedRun = useSkillWorkspace().runId;
  const [open, setOpen] = useState<Record<DockModuleId, boolean>>({ properties: !selectedRun, skill: !!selectedRun, history: false });
  const runs = useQuery({ queryKey: e.projectId ? skillQueryKeys.runs(e.projectId) : ['vu-runs', 'dock-idle'],
    queryFn: ({ signal }) => skillApi.runs(e.projectId, signal), enabled: Boolean(e.projectId) && active,
    refetchInterval: active ? 2500 : false, retry: protectedQueryRetry });
  const count = (runs.data ?? []).filter(item => ACTIVE_RUN.has(item.state)).length;
  useEffect(() => { if (active && selectedRun) { setOpen({ properties: false, skill: true, history: false }); if (stripOnly) onExpand?.(); } }, [active, selectedRun]);
  useEffect(() => {
    const openScript = () => { if (active) navigate({ workspace: 'script' }); };
    window.addEventListener('vac:open-script-dock', openScript);
    return () => window.removeEventListener('vac:open-script-dock', openScript);
  }, [active, navigate]);
  return <aside className={`editor-right-dock${stripOnly ? ' is-strip-only' : ''}`} aria-label="编辑器侧栏">
    {MODULES.map(mod => {
      const expanded = !stripOnly && open[mod.id], badge = mod.id === 'skill' && count > 0 && !expanded;
      return <section key={mod.id} className={`editor-dock-module${expanded ? ' is-open' : ''}`}>
        <button type="button" className="editor-dock-strip" aria-expanded={expanded} title={mod.label} onClick={() => {
          setOpen(prev => stripOnly ? { properties: mod.id === 'properties', skill: mod.id === 'skill', history: mod.id === 'history' } : { ...prev, [mod.id]: !prev[mod.id] });
          if (stripOnly) onExpand?.();
        }}><span className="editor-dock-strip-label">{mod.label}</span>
          {badge && <span className="editor-dock-badge" aria-label={`${count} 个 Skill 任务进行中`}>{count > 9 ? '9+' : count}</span>}
          {!stripOnly && <Typography.Text type="secondary" aria-hidden="true">{expanded ? '▾' : '▸'}</Typography.Text>}
        </button>
        {expanded && <div className="editor-dock-body">{mod.id === 'properties' ? <TimelineInspector /> : mod.id === 'skill' ? <SkillRunModule navigate={navigate} active={active && !stripOnly} /> : <Table size="small" pagination={false} rowKey="id" locale={{ emptyText: '暂无编辑历史' }} dataSource={e.view?.history ?? []} columns={[
          { title: '操作', dataIndex: 'label' }, { title: '来源', dataIndex: 'actor' }, { title: '状态', dataIndex: 'applied', render: (value: boolean) => value ? '已应用' : '已撤销' },
        ]} />}</div>}
      </section>;
    })}
  </aside>;
}
