import { Typography } from 'antd';
import {
  CheckCircleOutlined,
  ClockCircleOutlined,
  RightOutlined
} from '@ant-design/icons';
import type { RouteState } from '../navigation';
import type { Asset, WorkflowRun } from '../types';

export interface PipelineStepperProps {
  route: RouteState;
  navigate: (state: Partial<RouteState>) => void;
  asset?: Asset;
  currentRun?: WorkflowRun;
}

interface StepItem {
  id: string;
  label: string;
  target: Partial<RouteState>;
  isMatch: (route: RouteState) => boolean;
}

const STEPS: StepItem[] = [
  {
    id: 'import',
    label: '导入',
    target: { workspace: 'editing', editingSection: 'import' },
    isMatch: (r) => r.workspace === 'editing' && r.editingSection === 'import'
  },
  {
    id: 'assembly',
    label: '粗剪',
    target: { workspace: 'editing', editingSection: 'import' },
    isMatch: (r) => r.workspace === 'editing' && r.editingSection === 'import'
  },
  {
    id: 'prepare',
    label: '准备',
    target: { workspace: 'settings', settingsCategory: 'preparation' },
    isMatch: (r) => r.workspace === 'settings' && r.settingsCategory === 'preparation'
  },
  {
    id: 'generate',
    label: '生成',
    target: { workspace: 'export', exportSection: 'tasks' },
    isMatch: (r) => r.workspace === 'export' && r.exportSection === 'tasks'
  },
  {
    id: 'review',
    label: '审查',
    target: { workspace: 'editing', editingSection: 'review' },
    isMatch: (r) => r.workspace === 'editing' && (r.editingSection === 'review' || r.editingSection === 'content')
  },
  {
    id: 'delivery',
    label: '交付',
    target: { workspace: 'export', exportSection: 'delivery' },
    isMatch: (r) => r.workspace === 'export' && (r.exportSection === 'delivery' || r.exportSection === 'acceptance')
  }
];

export function PipelineStepper({ route, navigate, asset, currentRun }: PipelineStepperProps) {
  return (
    <div className="pipeline-stepper" role="navigation" aria-label="全流程阶段指示器">
      {STEPS.map((step, idx) => {
        const isCurrent = step.isMatch(route);
        const hasAsset = Boolean(asset);
        const isPastRun = currentRun?.status === 'completed';
        const isRunning = currentRun?.status === 'running';

        return (
          <div key={step.id} style={{ display: 'inline-flex', alignItems: 'center' }}>
            <div
              className={`pipeline-step-item ${isCurrent ? 'is-current' : ''}`}
              onClick={() => navigate(step.target)}
              title={`跳转到${step.label}阶段`}
              role="button"
              tabIndex={0}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  navigate(step.target);
                }
              }}
            >
              {step.id === 'import' && hasAsset && !isCurrent && (
                <CheckCircleOutlined style={{ fontSize: 11, color: '#52c41a' }} />
              )}
              {step.id === 'generate' && isRunning && (
                <ClockCircleOutlined style={{ fontSize: 11, color: '#faad14' }} />
              )}
              {step.id === 'delivery' && isPastRun && !isCurrent && (
                <CheckCircleOutlined style={{ fontSize: 11, color: '#52c41a' }} />
              )}
              <span>{step.label}</span>
            </div>
            {idx < STEPS.length - 1 && (
              <span className="pipeline-step-arrow" aria-hidden="true">
                <RightOutlined style={{ fontSize: 9 }} />
              </span>
            )}
          </div>
        );
      })}
    </div>
  );
}
