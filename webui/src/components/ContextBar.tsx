import type { ReactNode } from 'react';
import { Button, Tooltip } from 'antd';
import { RightOutlined } from '@ant-design/icons';
import type { RouteState } from '../navigation';
import { locationCrumbs, type NextStep, type StageTarget } from '../pipeline';

export interface ContextBarProps {
  route: RouteState;
  assetId?: string;
  next: NextStep;
  onGo: (target: StageTarget) => void;
  onNavigate: (route: Partial<RouteState>) => void;
  /** 右端附加控件（侧栏折叠按钮） */
  extra?: ReactNode;
}

const hashOf = (r: Partial<RouteState>) => {
  const w = r.workspace ?? 'resources';
  const sub = w === 'editing' ? r.editingSection : w === 'export' ? r.exportSection : w === 'settings' ? r.settingsCategory : undefined;
  return `#/${w}${sub ? `/${sub}` : ''}`;
};

/**
 * 页内位置条：左侧「工作区 › 分区 › 当前资产」面包屑（每个工作区一致，可点击回到上级），
 * 右侧唯一的主操作「下一步」（由资产与流程的真实状态推导，被阻塞时禁用并写明缺什么）。
 */
export function ContextBar({ route, assetId, next, onGo, onNavigate, extra }: ContextBarProps) {
  const crumbs = locationCrumbs(route, assetId);
  const blocked = Boolean(next.disabledReason);
  // 当前页 = 最深的“位置”段；资产段只是对象，在非资源页是指向资源管理的链接
  const currentIdx = crumbs.reduce((acc, c, i) => (c.key.startsWith('asset/') && c.target ? acc : i), 0);
  const routeKey = crumbs.map((c) => c.key).join('>');
  return (
    <div className="context-bar">
      <nav aria-label="当前位置" className="context-crumbs" key={routeKey}>
        <h1 className="sr-only">{crumbs.map((c) => c.label).join(' › ')}</h1>
        <ol>
          {crumbs.map((c, i) => {
            const last = i === crumbs.length - 1;
            return (
              <li key={c.key} className={i === currentIdx ? 'is-current' : undefined}>
                {c.target
                  ? <a href={hashOf(c.target)} aria-current={i === currentIdx ? 'page' : undefined} onClick={(e) => { e.preventDefault(); onNavigate(c.target!); }}>{c.label}</a>
                  : <span aria-current={i === currentIdx ? 'page' : undefined} className={c.key.startsWith('asset/') ? 'crumb-asset' : undefined}>{c.label}</span>}
                {!last && <span className="crumb-sep" aria-hidden="true">›</span>}
              </li>
            );
          })}
        </ol>
      </nav>
      <div className="context-actions">
        <span className="next-step-prefix">下一步</span>
        <Tooltip title={next.disabledReason ?? next.hint} placement="bottomRight">
          <span className={`next-step-wrap${blocked ? ' is-blocked' : ''}`} tabIndex={blocked ? 0 : undefined}
            aria-label={blocked ? `下一步（暂不可用）：${next.label}。${next.disabledReason}` : undefined}>
            <Button key={next.label} className="next-step-btn" type="primary" size="small" disabled={blocked}
              icon={<RightOutlined />} iconPosition="end" data-next-stage={next.stage ?? ''}
              onClick={() => next.target && onGo(next.target)}>
              {next.label}
            </Button>
          </span>
        </Tooltip>
        {extra}
      </div>
    </div>
  );
}
