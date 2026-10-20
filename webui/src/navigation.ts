import { useCallback, useEffect, useState } from 'react';

/**
 * 工作区：资源管理 → 编辑 → 导出 + 独立 TTS、设置。
 * 主工作区按 Premiere 式操作流程排列；TTS 和设置不依赖素材处理流程。
 */
export type WorkspaceId = 'resources' | 'editing' | 'export' | 'settings' | 'tts';

/**
 * 编辑页二级分区：导入与粗剪 / 内容编辑 / 审查
 */
export type EditingSection = 'import' | 'content' | 'review';

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
  { id: 'editing', label: '编辑', english: 'Editing', description: '导入、粗剪、内容编辑、场景审查', apis: 'API-03~16 · 19 · 28~38 · 42' },
  { id: 'export', label: '导出', english: 'Export', description: '交付文件、人工验收、任务监控', apis: 'API-28 · 30 · 39~41 · 43~49' },
  { id: 'tts', label: 'TTS', english: 'TTS', description: '文本试听、声音调控、模板版本与正式配音配置', apis: 'TTS API' },
  { id: 'settings', label: '设置', english: 'Settings', description: '处理预设、服务商、运行准备、日志诊断', apis: 'API-17~21 · 23~26 · 49~50' }
];

export const EDITING_SECTIONS: readonly EditingSectionDef[] = [
  { id: 'import', label: '导入与粗剪', description: '登记录像、自动切分、选段并生成短片' },
  { id: 'content', label: '内容编辑', description: '查看生成进度、调整场景与解说' },
  { id: 'review', label: '审查', description: '场景确认、解说批准、流程控制' }
];

export const EXPORT_SECTIONS: readonly ExportSectionDef[] = [
  { id: 'delivery', label: '交付', description: '预览成片、下载文件、导入已有成片包' },
  { id: 'acceptance', label: '人工验收', description: '检查预览并逐项通过或标出问题' },
  { id: 'tasks', label: '任务监控', description: '流程阶段、任务派发、失败重试、审计索引' }
];

export const SETTINGS_CATEGORIES: readonly SettingsCategoryDef[] = [
  { id: 'profiles', label: '处理预设', description: '预设版本、模型配置、历史版本加载' },
  { id: 'providers', label: '服务商接入', description: '识别/解说服务、模型发现、连接测试' },
  { id: 'preparation', label: '运行准备', description: '资产/预设选择、条件预检、外发授权、启动流程' },
  { id: 'skills', label: 'Agent 与 Skill', description: 'MCP 连接配置、Skill 包管理与运行能力' },
  { id: 'logs', label: '日志与诊断', description: '审计记录、人工操作日志、刷新快照' },
  { id: 'status', label: '状态监控', description: '系统连接与本机服务状态（技术人员）' }
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
}

/**
 * 解析路由：#/resources | #/editing/import | #/export/delivery | #/settings/profiles
 * 旧路径兼容：#/import → #/editing/import，#/monitor → #/export/tasks 等
 */
function parseHash(): RouteState {
  const [path, query = ''] = window.location.hash.replace(/^#\/?/, '').split('?');
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
    state.editingSection = EDITING_SECTIONS.find((s) => s.id === second)?.id ?? DEFAULT_EDITING_SECTION;
  } else if (workspace === 'export') {
    state.exportSection = EXPORT_SECTIONS.find((s) => s.id === second)?.id ?? DEFAULT_EXPORT_SECTION;
  } else if (workspace === 'settings') {
    state.settingsCategory = SETTINGS_CATEGORIES.find((c) => c.id === second)?.id ?? DEFAULT_SETTINGS_CATEGORY;
  }

  return state;
}

function buildHash(state: RouteState): string {
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
  return path + (query.size ? `?${query}` : '');
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
    const hash = buildHash(next);
    if (window.location.hash !== `#${hash}`) {
      window.location.hash = hash;
    }
    setRoute(next);
  }, [route]);

  return { route, navigate };
}
