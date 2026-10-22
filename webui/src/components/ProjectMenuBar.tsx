import { useEffect, useRef, useState } from 'react';
import { App, Menu, Typography, type MenuProps } from 'antd';
import { desktopCommand, isDesktop } from '../desktop';
import { useProjectWorkspace } from '../projectWorkspace';
import type { WorkspaceId } from '../navigation';
import appPackage from '../../package.json';

interface Props { navigate: (workspace: WorkspaceId) => void; toggleLeft: () => void; toggleRight: () => void; resetLayout: () => void; refresh: () => void; shortcuts: () => void; tour: () => void }
export function ProjectMenuBar(props: Props) {
  const [openKeys, setOpenKeys] = useState<string[]>([]);
  const project = useProjectWorkspace();
  const { modal, message } = App.useApp();
  const title = project.session?.document
    ? `Video Auto Cut — ${project.session.document.project.name}${project.session.dirty || project.localDirty ? ' *' : ''}`
    : 'Video Auto Cut';
  useEffect(() => {
    document.title = title;
    if (isDesktop()) void desktopCommand('set_window_title', { title }).catch(error => message.error(String(error)));
  }, [title, message]);
  const focus = useRef<HTMLElement | null>(null);
  useEffect(() => { const capture = (event: FocusEvent) => { const target = event.target; if (target instanceof HTMLElement && !target.closest('.project-menu-bar, [role=menu], [role=menuitem]')) focus.current = target; }; document.addEventListener('focusin', capture); return () => document.removeEventListener('focusin', capture); }, []);
  const editCommand = async (command: 'undo' | 'redo' | 'cut' | 'copy' | 'paste') => {
    const target = focus.current;
    if (target?.isConnected && target.getClientRects().length > 0 && (target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement || target.isContentEditable)) {
      target.focus();
      if (command === 'paste') {
        const value = isDesktop() ? await desktopCommand<string>('clipboard_read_text') : await navigator.clipboard.readText();
        if (target.isConnected) { target.focus(); document.execCommand('insertText', false, value); }
      } else { document.execCommand(command); }
      return;
    }
    const event = new CustomEvent('vac:edit-command', { detail: command, cancelable: true }); if (!window.dispatchEvent(event)) return;
    if (command === 'undo' || command === 'redo') { await project.history(command); }
    else if (command === 'copy') { const text = window.getSelection()?.toString(); if (text) await navigator.clipboard.writeText(text); }
  };
  const active = Boolean(project.session?.document);
  const execute = (action: () => Promise<unknown>) => { void action().catch(e => message.error(e instanceof Error ? e.message : String(e))); };
  const commands: Record<string, () => void> = {
    new: () => execute(project.create), open: () => execute(() => project.open()), save: () => execute(project.save), saveAs: () => execute(project.saveAs), import: () => execute(project.importFiles), exportScript: () => execute(() => project.exportScript()), close: () => execute(project.close),
    settings: () => props.navigate('settings'), preferences: () => props.navigate('settings'), exit: () => window.dispatchEvent(new Event('vac:request-exit')),
    undo: () => execute(() => editCommand('undo')), redo: () => execute(() => editCommand('redo')),
    cut: () => execute(() => editCommand('cut')), copy: () => execute(() => editCommand('copy')), paste: () => execute(() => editCommand('paste')),
    remove: () => { if (project.resource) execute(() => project.patchResource(project.resource!.id, { removed: true })); },
    left: props.toggleLeft, right: props.toggleRight, reset: props.resetLayout, refresh: props.refresh,
    data: () => execute(() => desktopCommand('open_data_directory')),
    updates: () => window.dispatchEvent(new Event('vac:check-updates')),
    shortcuts: props.shortcuts, tour: props.tour,
    about: () => modal.info({ title: '关于 Video Auto Cut', content: <Typography.Paragraph>Video Auto Cut {appPackage.version}<br />AI（Agent）Video Project · AVP 工程工作区</Typography.Paragraph> })
  };
  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      if (!(event.ctrlKey || event.metaKey) || event.altKey) return;
      const key = event.key.toLowerCase();
      const action = key === 'n' ? commands.new : key === 'o' ? commands.open : key === 's' ? (event.shiftKey ? commands.saveAs : commands.save) : key === 'i' ? commands.import : undefined;
      if (!action) return; event.preventDefault();
      if (project.busy || (!active && (key === 's' || key === 'i'))) return;
      action();
    };
    window.addEventListener('keydown', handler); return () => window.removeEventListener('keydown', handler);
  }, [project, active]);
  const edit = (key: string) => {
    if (key.startsWith('recent:')) { execute(() => project.open(key.slice(7))); return; }
    if (key.startsWith('workspace:')) { props.navigate(key.slice(10) as WorkspaceId); return; }
    commands[key]?.();
  };
  const items: MenuProps['items'] = [
    { key: 'file', label: '文件', children: [
      { key: 'new', label: '新建项目', extra: 'Ctrl+N' }, { key: 'open', label: '打开项目', extra: 'Ctrl+O' },
      { key: 'recent', label: '最近的项目', children: project.session?.recent.length ? project.session.recent.map(item => ({ key: `recent:${item.path}`, label: item.name, title: item.path })) : [{ key: 'no-recent', label: '暂无最近项目', disabled: true }] },
      { type: 'divider' }, { key: 'save', label: '保存', disabled: !active, extra: 'Ctrl+S' }, { key: 'saveAs', label: '另存为', disabled: !active, extra: 'Ctrl+Shift+S' },
      { key: 'import', label: '导入', disabled: !active, extra: 'Ctrl+I' }, { key: 'exportScript', label: '导出脚本', disabled: !project.resource?.script_id }, { key: 'close', label: '关闭项目', disabled: !active },
      { type: 'divider' }, { key: 'settings', label: '设置' }, { key: 'exit', label: '退出', disabled: !isDesktop() }
    ] },
    { key: 'edit', label: '编辑', children: [{ key: 'undo', label: '撤销', disabled: !active }, { key: 'redo', label: '重做', disabled: !active }, { type: 'divider' }, { key: 'cut', label: '剪切' }, { key: 'copy', label: '复制' }, { key: 'paste', label: '粘贴' }, { key: 'remove', label: '移除所选资源', disabled: !project.resource || project.resource.removed }, { type: 'divider' }, { key: 'preferences', label: '首选项' }] },
    { key: 'view', label: '视图', children: [
      ...([['resources', '资源管理'], ['editing', '编辑'], ['export', '导出'], ['tts', '配音']] as const).map(([id, label]) => ({ key: `workspace:${id}`, label, disabled: !active })),
      { type: 'divider' }, { key: 'left', label: '显示／隐藏资源栏' }, { key: 'right', label: '显示／隐藏元信息栏' }, { key: 'reset', label: '重置布局' }, { key: 'refresh', label: '刷新' }, { key: 'data', label: '打开数据目录', disabled: !isDesktop() }
    ] },
    { key: 'about-menu', label: '关于', children: [{ key: 'about', label: '版本信息' }, { key: 'updates', label: '检查更新', disabled: !isDesktop() }, { key: 'shortcuts', label: '快捷键说明' }, { key: 'tour', label: '新手引导', disabled: !active }] }
  ];
  return <div className="project-menu-bar" aria-busy={project.busy}><Menu mode="horizontal" aria-label="应用菜单" selectable={false} disabled={project.busy} items={items} openKeys={openKeys} onOpenChange={setOpenKeys} onClick={({ key }) => { setOpenKeys([]); edit(key); }} /></div>;
}
