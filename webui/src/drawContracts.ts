/**
 * DRAW01 Phase 1 contracts.
 * The three edit handoffs are product behavior. HTTP, sandbox, and workers
 * stay unwired; this module never builds or exposes a shell command.
 */

export const DRAW_HANDOFF_STUB = '方案接线中' as const;

export const DRAW_ENGINES = ['hyperframes', 'remotion'] as const;
export type DrawEngine = (typeof DRAW_ENGINES)[number];

/** Panel focus inside the single draw page. Never a top-bar sub-route. */
export const DRAW_FOCUSES = ['canvas', 'timeline', 'source', 'inspector', 'lint', 'render-queue'] as const;
export type DrawFocus = (typeof DRAW_FOCUSES)[number];

export const DRAW_ZONES = [
  { id: 'source', label: '构图、图层与源码' },
  { id: 'canvas', label: '构图预览' },
  { id: 'timeline', label: '构图时间轴' },
  { id: 'inspector', label: 'Inspector、Lint 与渲染队列' }
] as const;
export type DrawZoneId = (typeof DRAW_ZONES)[number]['id'];

export const DRAW_HANDOFF_ACTIONS = ['import_into_editing', 'composite_preview', 'merge_render'] as const;
export type DrawHandoffAction = (typeof DRAW_HANDOFF_ACTIONS)[number];

export const DRAW_ENTRY_SKILLS = ['video-use', 'vac-draw-hyperframes', 'vac-draw-remotion', 'vac-draw-pipeline'] as const;

export interface DrawComposition {
  id: string;
  name: string;
  engine: DrawEngine;
  durationFrames: number;
  fps: number;
  /** Phase 1 placeholders are never rendered pixels. */
  rendered: false;
}

/** Local studio placeholders. They are not fetched and are not MP4s. */
export const PHASE1_COMPOSITIONS: readonly DrawComposition[] = [
  { id: 'slot-title', name: '片头构图', engine: 'hyperframes', durationFrames: 90, fps: 30, rendered: false },
  { id: 'slot-lower-third', name: '信息层', engine: 'remotion', durationFrames: 150, fps: 30, rendered: false }
];

export interface DrawDeepLink {
  project?: string;
  run?: string;
  slot?: string;
  engine?: DrawEngine;
  focus?: DrawFocus;
}

export interface DrawHandoffRequest {
  action: DrawHandoffAction;
  project_id: string;
  skill_run_id?: string;
  /** One slot for import; several when merge-rendering compositions together. */
  slot_ids: string[];
  /** Editing sequence to receive a later import, composite preview, or merge render. */
  sequence_id?: string;
  engine: DrawEngine;
}

export interface DrawHandoffResult {
  status: 'stub';
  message: typeof DRAW_HANDOFF_STUB;
  action: DrawHandoffAction;
  /** Phase 1 does not call HTTP, a sandbox, or a renderer. */
  transport: 'unwired';
}

export const DRAW_HANDOFF_COPY: Record<DrawHandoffAction, { label: string; detail: string }> = {
  import_into_editing: {
    label: '渲后导入编辑',
    detail: '画面生产完成后，把产物登记为工程资源并进入编辑时间线。'
  },
  composite_preview: {
    label: '未渲拼合预览',
    detail: '未渲染导出的构图可与编辑轨拼合预览，不要求先有 MP4。'
  },
  merge_render: {
    label: '一并渲染',
    detail: '多个绘制构图与编辑轨合并到最后一起渲染，不必按段先导出。'
  }
};

export function isDrawEngine(value: string | null | undefined): value is DrawEngine {
  return value === 'hyperframes' || value === 'remotion';
}

export function isDrawFocus(value: string | null | undefined): value is DrawFocus {
  return !!value && (DRAW_FOCUSES as readonly string[]).includes(value);
}

export function isDrawEntrySkill(id: string | null | undefined): boolean {
  return !!id && (DRAW_ENTRY_SKILLS as readonly string[]).includes(id);
}

/** HyperFrames Studio is the default page structure. Remotion only when that skill is chosen. */
export function engineForSkill(id: string | null | undefined): DrawEngine {
  return id === 'vac-draw-remotion' ? 'remotion' : 'hyperframes';
}

export function zoneForFocus(focus: DrawFocus | undefined): DrawZoneId {
  if (focus === 'timeline') return 'timeline';
  if (focus === 'source') return 'source';
  if (focus === 'inspector' || focus === 'lint' || focus === 'render-queue') return 'inspector';
  return 'canvas';
}

export function drawRoute(link: DrawDeepLink = {}): {
  workspace: 'draw';
  drawProject: string;
  drawRun: string;
  drawSlot: string;
  drawEngine?: DrawEngine;
  drawFocus?: DrawFocus;
} {
  return {
    workspace: 'draw',
    drawProject: link.project ?? '',
    drawRun: link.run ?? '',
    drawSlot: link.slot ?? '',
    drawEngine: link.engine,
    drawFocus: link.focus
  };
}

/**
 * Phase 1 stub. The signature is the contract the later worker must accept.
 * It does not call the network and does not assemble a command line.
 */
export function requestDrawHandoff(input: DrawHandoffRequest): DrawHandoffResult {
  if (!DRAW_HANDOFF_ACTIONS.includes(input.action) || !isDrawEngine(input.engine)) {
    return { status: 'stub', message: DRAW_HANDOFF_STUB, action: input.action, transport: 'unwired' };
  }
  return { status: 'stub', message: DRAW_HANDOFF_STUB, action: input.action, transport: 'unwired' };
}

export function drawAgentContext(input: {
  project_id?: string;
  skill_run_id?: string;
  slot_id?: string;
  sequence_id?: string;
  engine: DrawEngine;
  focus?: DrawFocus;
}): string {
  return [
    'VAC 绘制上下文',
    `project=${input.project_id ?? ''}`,
    `run=${input.skill_run_id ?? ''}`,
    `slot=${input.slot_id ?? ''}`,
    `sequence=${input.sequence_id ?? ''}`,
    `engine=${input.engine}`,
    `focus=${input.focus ?? 'canvas'}`,
    'shell=unavailable',
    `transport=${DRAW_HANDOFF_STUB}`,
    ...DRAW_HANDOFF_ACTIONS.map((action) => `${action}=${DRAW_HANDOFF_COPY[action].label}`)
  ].join('\n');
}
