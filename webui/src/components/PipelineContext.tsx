import { createContext, useContext } from 'react';
import { Tooltip } from 'antd';
import type { StageId, StageInfo, StageStatus, StageTarget } from '../pipeline';

export interface PipelineCtx {
  stages: readonly StageInfo[];
  goTo: (target: StageTarget) => void;
}
export const PipelineContext = createContext<PipelineCtx>({ stages: [], goTo: () => undefined });
export const usePipelineCtx = () => useContext(PipelineContext);

const STATUS_WORD: Record<StageStatus, string> = { done: '已完成', active: '进行中', todo: '待办', blocked: '被阻塞', error: '出错', unknown: '读取中' };

/**
 * 二级菜单里的“对应流程阶段”小标记：一个状态色点，悬停说明「对应流程阶段：审查（待确认场景）」。
 * 没有对应阶段的分区不显示任何东西，不增加新的栏。
 */
export function StageDot({ stage }: { stage?: StageId }) {
  const { stages } = usePipelineCtx();
  const info = stage ? stages.find((s) => s.id === stage) : undefined;
  if (!info) return null;
  const text = `对应流程阶段：${info.label}（${STATUS_WORD[info.status]} · ${info.statusText}）${info.reason ? `：${info.reason}` : ''}`;
  return (
    <Tooltip title={text}>
      <span className={`stage-dot is-${info.status}`} role="img" aria-label={text} />
    </Tooltip>
  );
}
