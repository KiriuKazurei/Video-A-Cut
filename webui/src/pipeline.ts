import type { Asset, WorkflowRun } from './types';
import type { IngestRun } from './ingest';
import type { RouteState } from './navigation.ts';
import { EDITING_SECTIONS, EXPORT_SECTIONS, SETTINGS_CATEGORIES, WORKSPACES } from './navigation.ts';

/**
 * 统一的流程模型（纯函数，无 React / 无网络）。
 *
 * 阶段（顶部阶段条 = 唯一的“我在流程哪一步”）：
 *   导入与粗剪 → 准备 → 生成 → 审查 → 交付
 * 每个阶段指向它“真正发生”的页面（可能在 编辑 / 设置 / 导出 之间跳转）。
 *
 * 状态只来自真实数据：资产（input_kind / agent_visible / locked / status）、
 * 导入运行（IngestRun.state）、内容流程（WorkflowRun.status / stage）。
 * 取不到的数据（尚未加载）保守处理为 'unknown'，不会被当作已完成。
 */
export type StageId = 'import' | 'prepare' | 'generate' | 'review' | 'delivery';
export type StageStatus = 'done' | 'active' | 'todo' | 'blocked' | 'error' | 'unknown';

export interface StageTarget { route: Partial<RouteState>; anchor?: string }

export interface StageInfo {
  id: StageId;
  label: string;
  target: StageTarget;
  status: StageStatus;
  /** 状态文字（给提示和读屏用） */
  statusText: string;
  /** blocked / error 时的具体原因 */
  reason?: string;
  /** 当前路由正处在该阶段 */
  here: boolean;
  /** 推荐的下一步所在阶段 */
  next: boolean;
}

export interface NextStep {
  label: string;
  stage: StageId | null;
  target?: StageTarget;
  /** 有值 = 按钮禁用，值就是具体缺什么 */
  disabledReason?: string;
  /** 一句话说明（提示文字） */
  hint: string;
  /** 点击时触发的特殊动作（不走路由）：retry-workflow = 重新请求内容流程列表 */
  action?: 'retry-workflow';
}

export interface PipelineInput {
  asset?: Asset;
  assetCount: number;
  route: RouteState;
  /** undefined = 尚未加载；仅原始录像资产需要 */
  ingestRuns?: readonly IngestRun[];
  /** undefined = 尚未加载；null = 已加载且没有流程 */
  workflow?: WorkflowRun | null;
  /** 内容流程查询失败时的错误文案；有值时下一步改为可点的「重新加载」 */
  workflowError?: string;
}

export const STAGE_LABEL: Record<StageId, string> = { import: '导入与粗剪', prepare: '准备', generate: '生成', review: '审查', delivery: '交付' };
export const STAGE_ORDER: readonly StageId[] = ['import', 'prepare', 'generate', 'review', 'delivery'];

export const STAGE_TARGET: Record<StageId, StageTarget> = {
  import: { route: { workspace: 'editing', editingSection: 'import' }, anchor: 'ingest' },
  prepare: { route: { workspace: 'settings', settingsCategory: 'preparation' }, anchor: 'prepare-start' },
  generate: { route: { workspace: 'export', exportSection: 'tasks' }, anchor: 'tasks' },
  review: { route: { workspace: 'editing', editingSection: 'review' }, anchor: 'review' },
  delivery: { route: { workspace: 'export', exportSection: 'delivery' }, anchor: 'delivery' }
};

/** 二级分区 → 对应的流程阶段（用于二级菜单标记；没有对应阶段的返回 undefined） */
export function stageOfRoute(route: RouteState): StageId | undefined {
  if (route.workspace === 'editing') return route.editingSection === 'import' ? 'import' : 'review';
  if (route.workspace === 'export') return route.exportSection === 'tasks' ? 'generate' : 'delivery';
  if (route.workspace === 'settings') return route.settingsCategory === 'preparation' ? 'prepare' : undefined;
  return undefined;
}
export const SECTION_STAGE = {
  editing: { import: 'import', content: 'review', review: 'review' },
  export: { delivery: 'delivery', acceptance: 'delivery', tasks: 'generate' },
  settings: { preparation: 'prepare' }
} as const;

const isRaw = (asset?: Asset) => Boolean(asset && (asset.input_kind === 'raw_recording' || asset.ingest_run_id));

/** 导入阶段：非原始录像（EDL 包）视为已导入；原始录像看最新导入运行。 */
function importStage(input: PipelineInput): { status: StageStatus; text: string; reason?: string; run?: IngestRun } {
  const { asset, ingestRuns } = input;
  if (!asset) return { status: 'todo', text: '尚无资产' };
  if (!isRaw(asset)) return { status: 'done', text: '已导入（EDL 包）' };
  if (asset.input_kind !== 'raw_recording') return { status: 'done', text: '已导入' };
  if (!ingestRuns) return { status: 'unknown', text: '正在读取导入状态' };
  const run = ingestRuns[0];
  if (!run) return { status: 'todo', text: '尚未导入切分' };
  switch (run.state) {
    case 'ready': return { status: 'done', text: '导入完成', run };
    case 'failed': return { status: 'error', text: '导入失败', reason: run.error_message || run.error_code || '导入运行失败', run };
    case 'cancelled': return { status: 'todo', text: '导入已取消', run };
    case 'awaiting_review': return { status: 'todo', text: '等待选段', run };
    default: return { status: 'active', text: '导入处理中', run };
  }
}

function prepareBlock(asset: Asset): string | undefined {
  if (asset.locked) return '资产已锁定：运行准备要求资产未锁定。请在属性面板点击「解锁」。';
  if (!asset.agent_visible) return '资产对 Agent 不可见：运行准备要求资产对 Agent 可见。请在属性面板点击「设为 Agent 可见」。';
  return undefined;
}

export function derivePipeline(input: PipelineInput): { stages: StageInfo[]; next: NextStep } {
  const { asset, route, workflow } = input;
  const imp = importStage(input);
  const hereStage = stageOfRoute(route);

  const stat: Record<StageId, { status: StageStatus; text: string; reason?: string }> = {
    import: imp, prepare: { status: 'todo', text: '待准备' }, generate: { status: 'todo', text: '尚未生成' },
    review: { status: 'blocked', text: '尚无可审查内容', reason: '还没有内容流程：先到「准备」启动' },
    delivery: { status: 'blocked', text: '尚不能交付', reason: '还没有内容流程' }
  };

  if (!asset) {
    stat.prepare = { status: 'blocked', text: '需要资产', reason: '还没有资产：先到「导入与粗剪」登记录像或导入交付包' };
    stat.generate = { status: 'blocked', text: '需要资产', reason: '还没有资产' };
  } else if (input.workflowError && imp.status === 'done') {
    const why = `内容流程读取失败：${input.workflowError}`;
    stat.prepare = { status: 'error', text: '流程状态读取失败', reason: why };
    stat.generate = { status: 'error', text: '流程状态读取失败', reason: why };
    stat.review = { status: 'error', text: '流程状态读取失败', reason: why };
    stat.delivery = { status: 'error', text: '流程状态读取失败', reason: why };
  } else if (workflow === undefined && imp.status === 'done') {
    stat.prepare = { status: 'unknown', text: '正在读取流程状态' };
    stat.generate = { status: 'unknown', text: '正在读取流程状态' };
    stat.review = { status: 'unknown', text: '正在读取流程状态' };
    stat.delivery = { status: 'unknown', text: '正在读取流程状态' };
  } else if (imp.status !== 'done') {
    const why = '需先完成「导入与粗剪」：原始录像要先切分、选段并生成短片';
    stat.prepare = { status: 'blocked', text: '需先完成导入', reason: why };
    stat.generate = { status: 'blocked', text: '需先完成导入', reason: why };
    stat.review = { status: 'blocked', text: '需先完成导入', reason: why };
    stat.delivery = { status: 'blocked', text: '需先完成导入', reason: why };
  } else if (!workflow) {
    const block = prepareBlock(asset);
    stat.prepare = block ? { status: 'blocked', text: '运行条件未满足', reason: block } : { status: 'todo', text: '可以运行准备' };
    // 没有流程时，后续阶段的状态沿用上面的“尚无流程”默认值
  } else {
    stat.prepare = { status: 'done', text: '已启动内容流程' };
    const st = `${workflow.status} / ${workflow.stage}`;
    switch (workflow.status) {
      case 'created':
      case 'running':
        stat.generate = { status: 'active', text: '生成中' };
        stat.review = { status: 'blocked', text: '生成中', reason: `流程仍在生成（${st}），到达审核点后才能审查` };
        stat.delivery = { status: 'blocked', text: '生成中', reason: `流程仍在生成（${st}），审查完成后才能交付` };
        break;
      case 'failed':
        stat.generate = { status: 'error', text: '生成失败', reason: workflow.blocked_reason || workflow.error_code || `流程失败（${st}），到「审查」页重试` };
        stat.review = { status: 'error', text: '需要重试', reason: '流程失败：在「审查」页的流程控制里重试失败步骤' };
        stat.delivery = { status: 'blocked', text: '流程失败', reason: `流程失败（${st}）` };
        break;
      case 'cancelled':
        stat.generate = { status: 'todo', text: '流程已取消' };
        stat.review = { status: 'blocked', text: '流程已取消', reason: '流程已取消，需重新启动' };
        stat.delivery = { status: 'blocked', text: '流程已取消', reason: '流程已取消，需重新启动' };
        break;
      case 'awaiting_review':
        stat.generate = { status: 'done', text: '已生成，等待审查' };
        stat.review = { status: 'todo', text: workflow.stage === 'scene_review' ? '待确认场景' : workflow.stage === 'draft_review' ? '待批准解说' : '待审查' };
        stat.delivery = { status: 'blocked', text: '审查未完成', reason: `审查完成（ready_for_acceptance）后才能交付，当前 ${st}` };
        break;
      case 'ready_for_acceptance':
        stat.generate = { status: 'done', text: '已生成' };
        stat.review = { status: 'done', text: '审查完成' };
        stat.delivery = asset.status === 'exported' ? { status: 'done', text: '已导出' } : { status: 'todo', text: '待交付与验收' };
        break;
      default:
        stat.generate = { status: 'unknown', text: `流程状态 ${st}` };
        stat.review = { status: 'unknown', text: `流程状态 ${st}` };
        stat.delivery = { status: 'unknown', text: `流程状态 ${st}` };
    }
  }

  const next = deriveNext(input, imp, stat);
  const stages: StageInfo[] = STAGE_ORDER.map((id) => ({
    id, label: STAGE_LABEL[id], target: STAGE_TARGET[id],
    status: stat[id].status, statusText: stat[id].text, reason: stat[id].reason,
    here: hereStage === id, next: next.stage === id
  }));
  return { stages, next };
}

function deriveNext(input: PipelineInput, imp: ReturnType<typeof importStage>, stat: Record<StageId, { status: StageStatus; text: string; reason?: string }>): NextStep {
  const { asset, workflow, route, assetCount } = input;
  const go = (stage: StageId, label: string, hint: string, extra?: Partial<NextStep>): NextStep => ({ label, stage, target: STAGE_TARGET[stage], hint, ...extra });

  if (!asset) {
    return assetCount === 0
      ? go('import', '去登记录像', '资产库还没有资产：登记本地录像或导入交付包')
      : { label: '选择资产', stage: null, hint: '先在项目面板选择一项资产', disabledReason: '尚未选择资产：在左侧项目面板（或页头的资产下拉）选择一项' };
  }
  // 1) 导入阶段
  if (imp.status !== 'done') {
    if (imp.status === 'unknown') return go('import', '去导入与粗剪', '正在读取导入状态');
    const run = imp.run;
    if (!run) return go('import', '去导入切分', '原始录像尚未导入：到「导入与粗剪」探测并切分');
    switch (run.state) {
      case 'awaiting_review': return go('import', '去选段并生成短片', '切分完成：选择片段并生成短片');
      case 'failed': return go('import', '查看导入失败原因', imp.reason ?? '导入失败');
      case 'cancelled': return go('import', '重新导入切分', '上次导入已取消');
      default: return go('import', '查看导入进度', '导入正在处理');
    }
  }
  // 2) 内容流程
  if (input.workflowError) {
    return {
      label: '重新加载流程状态',
      stage: null,
      hint: `内容流程读取失败：${input.workflowError}。可点击重新加载，或到「运行准备」检查预设。`,
      action: 'retry-workflow',
      target: STAGE_TARGET.prepare // 备用：若调用方不处理 action，仍可跳到运行准备
    };
  }
  if (workflow === undefined) return { label: '下一步', stage: null, hint: '正在读取流程状态', disabledReason: '正在读取内容流程状态…' };
  const restart = (label: string, hint: string): NextStep => {
    const block = prepareBlock(asset);
    const onPrepare = route.workspace === 'settings' && route.settingsCategory === 'preparation';
    return go('prepare', onPrepare ? '开始生成' : label, hint, block ? { disabledReason: block } : undefined);
  };
  if (!workflow) return restart('去运行准备', '选择处理预设、核对运行条件并启动内容流程');
  switch (workflow.status) {
    case 'created':
    case 'running': return go('generate', '查看生成进度', `内容流程正在生成（${workflow.status} / ${workflow.stage}）`);
    case 'failed': return go('review', '查看失败并重试', stat.generate.reason ?? '流程失败');
    case 'cancelled': return restart('重新启动流程', '上次流程已取消，可重新运行准备');
    case 'awaiting_review':
      return go('review', workflow.stage === 'scene_review' ? '去确认场景' : workflow.stage === 'draft_review' ? '去批准解说' : '去审查', stat.review.text);
    case 'ready_for_acceptance':
      return asset.status === 'exported' ? go('delivery', '查看交付结果', '已导出：下载文件或补充验收') : go('delivery', '去交付与验收', '审查完成：预览、下载并做人工验收');
    default: return { label: '下一步', stage: null, hint: `流程状态 ${workflow.status} / ${workflow.stage}`, disabledReason: `无法判断下一步：流程状态为 ${workflow.status} / ${workflow.stage}` };
  }
}

// ---------- 位置面包屑（工作区 › 分区 › 当前对象）----------
export interface Crumb {
  key: string;
  label: string;
  /** 缺省 = 当前位置，不可点击 */
  target?: Partial<RouteState>;
}

/** 面包屑：每个工作区一致为「工作区 › 分区(如有) › 当前资产(如相关)」。 */
export function locationCrumbs(route: RouteState, assetId?: string): Crumb[] {
  const ws = WORKSPACES.find((w) => w.id === route.workspace)!;
  const crumbs: Crumb[] = [{ key: ws.id, label: ws.label, target: { workspace: ws.id } }];
  if (route.workspace === 'editing') {
    const s = EDITING_SECTIONS.find((x) => x.id === route.editingSection) ?? EDITING_SECTIONS[0];
    crumbs.push({ key: `editing/${s.id}`, label: s.label, target: { workspace: 'editing', editingSection: s.id } });
  } else if (route.workspace === 'export') {
    const s = EXPORT_SECTIONS.find((x) => x.id === route.exportSection) ?? EXPORT_SECTIONS[0];
    crumbs.push({ key: `export/${s.id}`, label: s.label, target: { workspace: 'export', exportSection: s.id } });
  } else if (route.workspace === 'settings') {
    const s = SETTINGS_CATEGORIES.find((x) => x.id === route.settingsCategory) ?? SETTINGS_CATEGORIES[0];
    crumbs.push({ key: `settings/${s.id}`, label: s.label, target: { workspace: 'settings', settingsCategory: s.id } });
  } else if (route.workspace === 'tts') {
    crumbs.push({ key: 'tts/console', label: '声音调控台' });
  }
  const assetRelevant = route.workspace === 'resources' || route.workspace === 'editing' || route.workspace === 'export'
    || (route.workspace === 'settings' && route.settingsCategory === 'preparation');
  if (assetRelevant) {
    crumbs.push({ key: `asset/${assetId ?? ''}`, label: assetId ?? '未选择资产', target: route.workspace === 'resources' ? undefined : { workspace: 'resources' } });
  }
  // 最后一段若是工作区/分区，就是“当前位置”，不可点击；资产段在非资源页可点击（回到资源管理查看该资产）
  const last = crumbs[crumbs.length - 1];
  if (!last.key.startsWith('asset/')) crumbs[crumbs.length - 1] = { key: last.key, label: last.label };
  return crumbs;
}
