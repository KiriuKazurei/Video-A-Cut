import { useCallback, useEffect, useState } from 'react';
import { isDrawEngine, isDrawFocus, type DrawEngine, type DrawFocus } from './drawContracts.ts';

/**
 * 工作区：资源管理 → 编辑 → 绘制 → 导出 + 独立 TTS、设置。
 * 「绘制」是少开顶栏分页的显式例外，页内不再拆子顶栏。
 * 主工作区按操作流程排列；TTS 和设置不依赖素材处理流程。
 */
export type WorkspaceId = 'resources' | 'editing' | 'draw' | 'export' | 'settings' | 'tts';

/** Digit keys. 1–5 stay on the existing workspaces; 6 opens 绘制. */
export const WORKSPACE_KEYS: Record<string, WorkspaceId> = {
  '1': 'resources',
  '2': 'editing',
  '3': 'export',
  '4': 'settings',
  '5': 'tts',
  '6': 'draw'
};

/**
 * 编辑页二级分区：时间线 / 脚本工作台（审查已并入时间线节目监视，不再单独成页）
 */
export type EditingSection = 'import' | 'content';

/**
 * 导出页二级分区：交付 / 人工验收 / 任务监控
 */
export type ExportSection = 'delivery' | 'acceptance' | 'tasks';

/**
 * 设置页分类：处理预设 / 服务商接入 / 运行准备 / 日志与诊断
 */
export type SettingsCategory = 'profiles' | 'providers' | 'preparation' | 'skills' | 'logs' | 'status';

export interface WorkspaceDef {
  id: WorkspaceId;
  label: string;
  english: string;
  description: string;
  /** 该工作区调用的接口编号（docs/frontend-api-inventory.json）*/
  apis: string;
}

export interface EditingSectionDef {
  id: EditingSection;
  label: string;
  description: string;
}

export interface ExportSectionDef {
  id: ExportSection;
  label: string;
  description: string;
}

export interface SettingsCategoryDef {
  id: SettingsCategory;
  label: string;
  description: string;
}

export const WORKSPACES: readonly WorkspaceDef[] = [
  { id: 'resources', label: '资源管理', english: 'Resources', description: '资产库、素材预览、属性治理、本地草稿', apis: 'API-01 · 03 · 43~45' },
  { id: 'editing', label: '编辑', english: 'Editing', description: '多序列轨道编辑、脚本标记引用、Skill 与历史', apis: 'TL API · Script API · Skill API' },
  { id: 'draw', label: '绘制', english: 'Draw', description: '构图预览、构图时间轴、源码与 Inspector。不是工程多轨时间线', apis: 'Draw API · Skill API' },
  { id: 'export', label: '导出', english: 'Export', description: '交付文件、人工验收、任务监控', apis: 'API-28 · 30 · 39~41 · 43~49' },
  { id: 'tts', label: 'TTS', english: 'TTS', description: '文本试听、声音调控、模板版本与正式配音配置', apis: 'TTS API' },
  { id: 'settings', label: '设置', english: 'Settings', description: '处理预设、服务商、运行准备、日志诊断', apis: 'API-17~21 · 23~26 · 49~50' }
];

export const EDITING_SECTIONS: readonly EditingSectionDef[] = [
  { id: 'import', label: '时间线', description: '当前序列的轨道、片段、效果及标记' },
  { id: 'content', label: '脚本工作台', description: '右侧栏脚本工作台（深链打开）' },
];

export const EXPORT_SECTIONS: readonly ExportSectionDef[] = [
  { id: 'delivery', label: '交付', description: '预览成片、下载文件、导入已有成片包' },
  { id: 'acceptance', label: '人工验收', description: '检查预览并逐项通过或标出问题' },
  { id: 'tasks', label: '任务监控', description: '流程阶段、任务派发、失败重试、审计索引' }
];

export const SETTINGS_CATEGORIES: readonly SettingsCategoryDef[] = [
  { id: 'profiles', label: '处理预设', description: '配置处理参数、取样和配音模板，保存并管理预设版本。' },
  { id: 'providers', label: '服务商接入', description: '配置画面识别、解说与转录，管理云端配音调用。' },
  { id: 'preparation', label: '运行准备', description: '选择已保存预设，检查素材与运行环境并开始生成。' },
  { id: 'skills', label: 'Agent 与 Skill', description: '连接外部 Agent，管理本地 Skill 包与依赖。' },
  { id: 'logs', label: '日志与诊断', description: '查询审计日志、Agent 操作与人工注释。' },
  { id: 'status', label: '状态监控', description: '查看后台连接、本机导入与语音服务的运行状态。' }
];

const DEFAULT_WORKSPACE: WorkspaceId = 'resources';
const DEFAULT_EDITING_SECTION: EditingSection = 'import';
const DEFAULT_EXPORT_SECTION: ExportSection = 'delivery';
const DEFAULT_SETTINGS_CATEGORY: SettingsCategory = 'profiles';

export interface RouteState {
  workspace: WorkspaceId;
  editingSection?: EditingSection;
  exportSection?: ExportSection;
  settingsCategory?: SettingsCategory;
  scriptId?: string;
  scriptVersion?: number;
  scriptSegment?: number;
  /** Draw deep link. Query only: project, run, slot, engine, focus. */
  drawProject?: string;
  drawRun?: string;
  drawSlot?: string;
  drawEngine?: DrawEngine;
  drawFocus?: DrawFocus;
}

/**
 * 解析路由：#/resources | #/editing/import | #/draw?project&run&slot&engine&focus | #/export/delivery | #/settings/profiles
 * 绘制页不使用路径二级分段。#/draw/lint 仍是绘制页，focus 只来自查询参数。
 * 旧路径兼容：#/import → #/editing/import，#/monitor → #/export/tasks 等
 */
export function parseWorkspaceHash(hash: string): RouteState {
  const [path, query = ''] = hash.replace(/^#\/?/, '').split('?');
  const raw = path.split('/');
  const [first, second] = raw;

  // 旧路径兼容：仅当没有二级子路径（单段旧 URL，如 #/import、#/export、#/monitor 等）时触发
  const legacyMap: Record<string, RouteState> = {
    'import': { workspace: 'editing', editingSection: 'import' },
    'assembly': { workspace: 'editing', editingSection: 'import' },
    'prepare': { workspace: 'settings', settingsCategory: 'preparation' },
    'edit': { workspace: 'editing', editingSection: 'content' },
    'export': { workspace: 'export', exportSection: 'delivery' },
    'monitor': { workspace: 'export', exportSection: 'tasks' }
  };
  if (!second && legacyMap[first]) return legacyMap[first];

  // 新路径解析
  const workspace = WORKSPACES.find((w) => w.id === first)?.id ?? DEFAULT_WORKSPACE;
  const state: RouteState = { workspace };
  const parameters = new URLSearchParams(query);
  if (parameters.get('script')) state.scriptId = parameters.get('script')!;
  const version = Number(parameters.get('version'));
  if (Number.isInteger(version) && version >= 0 && parameters.has('version')) state.scriptVersion = version;
  const segment = Number(parameters.get('segment'));
  if (Number.isInteger(segment) && segment >= 0 && parameters.has('segment')) state.scriptSegment = segment;

  if (workspace === 'editing') {
    // Legacy `#/editing/review` (旧「审查」页) → 时间线；效果在节目监视器查看
    state.editingSection = second === 'review' ? 'import'
      : (EDITING_SECTIONS.find((s) => s.id === second)?.id ?? DEFAULT_EDITING_SECTION);
  } else if (workspace === 'export') {
    state.exportSection = EXPORT_SECTIONS.find((s) => s.id === second)?.id ?? DEFAULT_EXPORT_SECTION;
  } else if (workspace === 'settings') {
    state.settingsCategory = SETTINGS_CATEGORIES.find((c) => c.id === second)?.id ?? DEFAULT_SETTINGS_CATEGORY;
  } else if (workspace === 'draw') {
    const project = parameters.get('project');
    const run = parameters.get('run');
    const slot = parameters.get('slot');
    const engine = parameters.get('engine');
    const focus = parameters.get('focus');
    if (project) state.drawProject = project;
    if (run) state.drawRun = run;
    if (slot) state.drawSlot = slot;
    if (isDrawEngine(engine)) state.drawEngine = engine;
    if (isDrawFocus(focus)) state.drawFocus = focus;
  }

  return state;
}

function parseHash(): RouteState {
  return parseWorkspaceHash(window.location.hash);
}

export function buildWorkspaceHash(state: RouteState): string {
  const { workspace, editingSection, exportSection, settingsCategory } = state;
  const path = workspace === 'editing' && editingSection ? `/${workspace}/${editingSection}`
    : workspace === 'export' && exportSection ? `/${workspace}/${exportSection}`
    : workspace === 'settings' && settingsCategory ? `/${workspace}/${settingsCategory}` : `/${workspace}`;
  const query = new URLSearchParams();
  if (state.scriptId && (workspace === 'tts' || (workspace === 'editing' && editingSection === 'content'))) {
    query.set('script', state.scriptId);
    if (state.scriptVersion !== undefined) query.set('version', String(state.scriptVersion));
    if (state.scriptSegment !== undefined) query.set('segment', String(state.scriptSegment));
  }
  if (workspace === 'draw') {
    if (state.drawProject) query.set('project', state.drawProject);
    if (state.drawRun) query.set('run', state.drawRun);
    if (state.drawSlot) query.set('slot', state.drawSlot);
    if (state.drawEngine) query.set('engine', state.drawEngine);
    if (state.drawFocus) query.set('focus', state.drawFocus);
  }
  return path + (query.size ? `?${query}` : '');
}

function buildHash(state: RouteState): string {
  return buildWorkspaceHash(state);
}

function clearDrawLink(state: RouteState) {
  delete state.drawProject;
  delete state.drawRun;
  delete state.drawSlot;
  delete state.drawEngine;
  delete state.drawFocus;
}

/** 路由 Hook：返回当前路由状态和导航函数 */
export function useWorkspaceRoute() {
  const [route, setRoute] = useState<RouteState>(parseHash);

  useEffect(() => {
    const sync = () => setRoute(parseHash());
    window.addEventListener('hashchange', sync);
    return () => window.removeEventListener('hashchange', sync);
  }, []);

  const navigate = useCallback((state: Partial<RouteState>) => {
    const next: RouteState = { ...route, ...state };
    if (state.scriptId === '') {
      delete next.scriptId; delete next.scriptVersion; delete next.scriptSegment;
    }
    if (next.workspace !== 'tts' && !(next.workspace === 'editing' && next.editingSection === 'content')) {
      delete next.scriptId; delete next.scriptVersion; delete next.scriptSegment;
    }
    const bareDrawTab = state.workspace === 'draw'
      && state.drawProject === undefined && state.drawRun === undefined && state.drawSlot === undefined
      && state.drawEngine === undefined && state.drawFocus === undefined
      && route.workspace !== 'draw';
    if (next.workspace !== 'draw' || bareDrawTab) clearDrawLink(next);
    else {
      if (next.drawProject === '') delete next.drawProject;
      if (next.drawRun === '') delete next.drawRun;
      if (next.drawSlot === '') delete next.drawSlot;
      if (!next.drawEngine) delete next.drawEngine;
      if (!next.drawFocus) delete next.drawFocus;
    }
    const hash = buildHash(next);
    if (window.location.hash !== `#${hash}`) {
      window.location.hash = hash;
    }
    setRoute(next);
  }, [route]);

  return { route, navigate };
}
