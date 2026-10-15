import { Dropdown, Tooltip } from 'antd';
import { CheckOutlined, CloseOutlined, LoadingOutlined, MinusOutlined, DownOutlined, RightOutlined } from '@ant-design/icons';
import type { StageInfo, StageStatus, StageTarget } from '../pipeline';

export interface PipelineStepperProps {
  stages: readonly StageInfo[];
  onGo: (target: StageTarget) => void;
  /** 窄窗口：折叠为「第 N/5 步 · 阶段名」下拉，点开可跳到任一阶段 */
  compact?: boolean;
}

const WORD: Record<StageStatus, string> = { done: '已完成', active: '进行中', todo: '待办', blocked: '被阻塞', error: '出错', unknown: '读取中' };

function StatusGlyph({ status }: { status: StageStatus }) {
  const icon = status === 'done' ? <CheckOutlined /> : status === 'active' ? <LoadingOutlined spin /> : status === 'error' ? <CloseOutlined /> : status === 'blocked' ? <MinusOutlined /> : null;
  return <span className={`stage-glyph is-${status}`} aria-hidden="true">{icon}</span>;
}

/**
 * 流程阶段条：导入与粗剪 → 准备 → 生成 → 审查 → 交付。
 * 唯一的“流程走到哪了”指示：每个阶段点击都去它真正发生的页面；
 * 状态来自真实的资产/导入/流程数据（见 pipeline.ts）；当前路由所在阶段带下划线，推荐的下一步阶段带一个蓝点。
 */
const stageTip = (step: StageInfo) => `${step.label}：${WORD[step.status]} · ${step.statusText}${step.reason ? `。${step.reason}` : ''}${step.here ? '（当前位置）' : ''}${step.next ? '（推荐的下一步）' : ''}`;

/** 紧凑变体：始终能看到“现在在第几步”。优先显示当前页所属阶段，其次推荐的下一步，再次第一个未完成阶段。 */
function CompactStepper({ stages, onGo }: PipelineStepperProps) {
  const found = stages.findIndex((s) => s.here);
  const idx = found >= 0 ? found : Math.max(0, stages.findIndex((s) => s.next) >= 0 ? stages.findIndex((s) => s.next) : stages.findIndex((s) => s.status !== 'done'));
  const cur = stages[idx] ?? stages[0];
  if (!cur) return null;
  return (
    <nav className="pipeline-stepper pipeline-stepper-compact" aria-label="流程阶段" data-tour="stepper">
      <Dropdown trigger={['click']} placement="bottomLeft" menu={{
        items: stages.map((s) => ({
          key: s.id,
          icon: <StatusGlyph status={s.status} />,
          label: s.label,
          extra: <span className="stage-menu-status">{WORD[s.status]}</span>,
          title: stageTip(s)
        })),
        selectedKeys: [cur.id],
        onClick: ({ key }) => { const t = stages.find((s) => s.id === key); if (t) onGo(t.target); }
      }}>
        <button type="button" className={`pipeline-step-item is-compact is-${cur.status} is-current`} aria-label={`流程阶段：第 ${idx + 1} 步，共 ${stages.length} 步，${stageTip(cur)}`}>
          <StatusGlyph status={cur.status} />
          <span className="stage-compact-count">第 {idx + 1}/{stages.length} 步</span>
          <span>{cur.label}</span>
          <DownOutlined style={{ fontSize: 10 }} aria-hidden="true" />
        </button>
      </Dropdown>
    </nav>
  );
}

export function PipelineStepper(props: PipelineStepperProps) {
  const { stages, onGo, compact } = props;
  if (compact) return <CompactStepper {...props} />;
  return (
    <nav className="pipeline-stepper" aria-label="流程阶段" data-tour="stepper">
      <ol>
        {stages.map((step, idx) => {
          const tip = stageTip(step);
          return (
            <li key={step.id}>
              <Tooltip title={tip} mouseEnterDelay={0.3}>
                <button type="button" className={`pipeline-step-item is-${step.status}${step.here ? ' is-current' : ''}${step.next ? ' is-next' : ''}`}
                  aria-current={step.here ? 'step' : undefined} aria-label={tip} onClick={() => onGo(step.target)}>
                  <StatusGlyph status={step.status} />
                  <span>{step.label}</span>
                  {step.next && <span className="stage-next-dot" aria-hidden="true" />}
                </button>
              </Tooltip>
              {idx < stages.length - 1 && <span className="pipeline-step-arrow" aria-hidden="true"><RightOutlined style={{ fontSize: 9 }} /></span>}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}
