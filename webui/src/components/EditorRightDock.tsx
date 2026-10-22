import { useState, type ReactNode } from 'react';
import { Table, Typography } from 'antd';
import { TimelineInspector } from './TimelineInspector';
import { SkillRunModule } from './SkillRunModule';
import { useSequenceEditor } from '../sequenceEditor';
import type { RouteState } from '../navigation';

type ModuleId = 'properties' | 'skill' | 'history';

const MODULES: { id: ModuleId; label: string }[] = [
  { id: 'properties', label: '属性' },
  { id: 'skill', label: 'Skill 任务' },
  { id: 'history', label: '编辑历史' },
];

/**
 * Premiere-style right dock: each utility is a horizontal name strip;
 * click expands/collapses its body. Strip-only mode keeps just the names
 * when the whole dock is collapsed.
 */
export function EditorRightDock({
  navigate,
  active,
  stripOnly = false,
  onExpand,
}: {
  navigate: (state: Partial<RouteState>) => void;
  active: boolean;
  stripOnly?: boolean;
  /** When the dock is strip-only, clicking a name expands the whole sidebar. */
  onExpand?: () => void;
}) {
  const e = useSequenceEditor();
  const [open, setOpen] = useState<Record<ModuleId, boolean>>({
    properties: true,
    skill: false,
    history: false,
  });
  const toggle = (id: ModuleId) => {
    if (stripOnly) {
      setOpen({ properties: id === 'properties', skill: id === 'skill', history: id === 'history' });
      onExpand?.();
      return;
    }
    setOpen((prev) => ({ ...prev, [id]: !prev[id] }));
  };

  const body = (id: ModuleId): ReactNode => {
    if (id === 'properties') return <TimelineInspector />;
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
        return (
          <section key={mod.id} className={`editor-dock-module${expanded ? ' is-open' : ''}`}>
            <button
              type="button"
              className="editor-dock-strip"
              aria-expanded={expanded}
              title={mod.label}
              onClick={() => toggle(mod.id)}
            >
              <span className="editor-dock-strip-label">{mod.label}</span>
              {!stripOnly && <Typography.Text type="secondary" aria-hidden="true">{expanded ? '▾' : '▸'}</Typography.Text>}
            </button>
            {expanded && <div className="editor-dock-body">{body(mod.id)}</div>}
          </section>
        );
      })}
    </aside>
  );
}
