import { useEffect, useState, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Empty, Table, Typography } from 'antd';
import { TimelineInspector } from './TimelineInspector';
import { SkillRunModule } from './SkillRunModule';
import { ScriptWorkbench } from './ScriptWorkbench';
import { useSequenceEditor } from '../sequenceEditor';
import { useProjectWorkspace } from '../projectWorkspace';
import { skillApi } from '../skillApi';
import { skillQueryKeys } from '../skillContracts';
import { protectedQueryRetry } from '../skillView';
import type { RouteState } from '../navigation';

export type DockModuleId = 'properties' | 'script' | 'skill' | 'history';

const MODULES: { id: DockModuleId; label: string }[] = [
  { id: 'properties', label: '属性' },
  { id: 'script', label: '脚本工作台' },
  { id: 'skill', label: 'Skill 任务' },
  { id: 'history', label: '编辑历史' },
];

const ACTIVE_RUN = new Set(['queued', 'running', 'waiting_human']);

/**
 * Premiere-style right dock: horizontal name strips; click expands body.
 * 脚本工作台 lives here (not as a competing main editing tab). Deep-link
 * `#/editing/content` and vac:open-script-dock open this strip. Timeline
 * stays in the center stage and remains interactive while script is open.
 */
export function EditorRightDock({
  navigate,
  active,
  stripOnly = false,
  onExpand,
  route,
  assetId,
  onModuleOpen,
  onModuleClose,
}: {
  navigate: (state: Partial<RouteState>) => void;
  active: boolean;
  stripOnly?: boolean;
  onExpand?: () => void;
  route?: RouteState;
  assetId?: string;
  /** Fired when a module is opened (e.g. widen sider for script). */
  onModuleOpen?: (id: DockModuleId) => void;
  /** Fired when a module is collapsed. */
  onModuleClose?: (id: DockModuleId) => void;
}) {
  const e = useSequenceEditor();
  const project = useProjectWorkspace();
  const wantScript = route?.editingSection === 'content' || Boolean(route?.scriptId);
  const [open, setOpen] = useState<Record<DockModuleId, boolean>>({
    properties: !wantScript,
    script: wantScript,
    skill: false,
    history: false,
  });
  const runs = useQuery({
    queryKey: e.projectId ? skillQueryKeys.runs(e.projectId) : ['vu-runs', 'dock-idle'],
    queryFn: ({ signal }) => skillApi.runs(e.projectId, signal),
    enabled: Boolean(e.projectId) && active,
    refetchInterval: active ? 2500 : false,
    retry: protectedQueryRetry,
  });
  const activeSkillCount = (runs.data ?? []).filter((item) => ACTIVE_RUN.has(item.state)).length;

  const openModule = (id: DockModuleId, exclusive = false) => {
    // Script shares the stage with the timeline — keep it exclusive in the dock
    // so the side panel stays usable without burying the center editor.
    const forceExclusive = exclusive || stripOnly || id === 'script';
    setOpen((prev) => {
      if (forceExclusive) {
        return { properties: id === 'properties', script: id === 'script', skill: id === 'skill', history: id === 'history' };
      }
      return { ...prev, [id]: true };
    });
    onModuleOpen?.(id);
    if (stripOnly) onExpand?.();
  };

  useEffect(() => {
    if (!active) return;
    if (route?.editingSection === 'content' || route?.scriptId) {
      setOpen({ properties: false, script: true, skill: false, history: false });
      onModuleOpen?.('script');
    }
  }, [active, route?.editingSection, route?.scriptId]);

  useEffect(() => {
    const onOpen = () => {
      onExpand?.();
      setOpen({ properties: false, script: true, skill: false, history: false });
      onModuleOpen?.('script');
    };
    window.addEventListener('vac:open-script-dock', onOpen);
    return () => window.removeEventListener('vac:open-script-dock', onOpen);
  }, [onExpand, onModuleOpen]);

  const toggle = (id: DockModuleId) => {
    if (stripOnly) {
      openModule(id, true);
      return;
    }
    setOpen((prev) => {
      const next = !prev[id];
      if (next) {
        onModuleOpen?.(id);
        if (id === 'script') {
          return { properties: false, script: true, skill: false, history: false };
        }
        return { ...prev, [id]: true };
      }
      onModuleClose?.(id);
      return { ...prev, [id]: false };
    });
  };

  const scriptAsset =
    project.session?.document?.resources.find((r) => r.script_id === route?.scriptId)?.asset_id
    ?? assetId
    ?? project.session?.document?.resources.find((r) => r.asset_id)?.asset_id;

  const body = (id: DockModuleId): ReactNode => {
    if (id === 'properties') return <TimelineInspector />;
    if (id === 'script') {
      if (!scriptAsset) return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="导入素材或 .avs 脚本后开始编辑" />;
      return (
        <div className="dock-script-workbench">
          <ScriptWorkbench
            key={`script-dock-${scriptAsset}`}
            assetId={scriptAsset}
            navigate={navigate}
            selectedScriptId={route?.scriptId}
            active={active && open.script && !stripOnly}
            variant="dock"
          />
        </div>
      );
    }
    if (id === 'skill') return <SkillRunModule navigate={navigate} active={active && open.skill && !stripOnly} />;
    return (
      <Table
        size="small"
        pagination={false}
        rowKey="id"
        locale={{ emptyText: '暂无编辑历史' }}
        dataSource={e.view?.history ?? []}
        columns={[
          { title: '操作', dataIndex: 'label' },
          { title: '来源', dataIndex: 'actor' },
          { title: '状态', dataIndex: 'applied', render: (value: boolean) => (value ? '已应用' : '已撤销') },
        ]}
      />
    );
  };

  return (
    <aside className={`editor-right-dock${stripOnly ? ' is-strip-only' : ''}`} aria-label="编辑器侧栏">
      {MODULES.map((mod) => {
        const expanded = !stripOnly && open[mod.id];
        const showBadge = mod.id === 'skill' && activeSkillCount > 0 && !expanded;
        return (
          <section key={mod.id} className={`editor-dock-module${expanded ? ' is-open' : ''}${mod.id === 'script' && expanded ? ' is-script' : ''}`}>
            <button
              type="button"
              className="editor-dock-strip"
              aria-expanded={expanded}
              title={showBadge ? `${mod.label}（${activeSkillCount} 个进行中）` : mod.label}
              onClick={() => toggle(mod.id)}
            >
              <span className="editor-dock-strip-label">{mod.label}</span>
              {showBadge ? (
                <span className="editor-dock-badge" aria-label={`${activeSkillCount} 个 Skill 任务进行中`}>
                  {activeSkillCount > 9 ? '9+' : activeSkillCount}
                </span>
              ) : null}
              {!stripOnly && <Typography.Text type="secondary" aria-hidden="true">{expanded ? '▾' : '▸'}</Typography.Text>}
            </button>
            {expanded && <div className="editor-dock-body">{body(mod.id)}</div>}
          </section>
        );
      })}
    </aside>
  );
}
