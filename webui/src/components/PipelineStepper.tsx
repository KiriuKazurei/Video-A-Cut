import { Tooltip } from 'antd';
import { CheckOutlined, CloseOutlined, LoadingOutlined, MinusOutlined, RightOutlined } from '@ant-design/icons';
import type { StageInfo, StageStatus, StageTarget } from '../pipeline';

export interface PipelineStepperProps {
  stages: readonly StageInfo[];
  onGo: (target: StageTarget) => void;
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
export function PipelineStepper({ stages, onGo }: PipelineStepperProps) {
  return (
    <nav className="pipeline-stepper" aria-label="流程阶段">
      <ol>
        {stages.map((step, idx) => {
          const tip = `${step.label}：${WORD[step.status]} · ${step.statusText}${step.reason ? `。${step.reason}` : ''}${step.here ? '（当前位置）' : ''}${step.next ? '（推荐的下一步）' : ''}`;
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
