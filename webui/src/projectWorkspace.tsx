import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { App, Button, Flex, Modal, Select, Space, Typography } from 'antd';
import { setProjectSession } from './api';
import { desktopCommand, isDesktop, projectEventListener } from './desktop';
import { pauseMediaPlayers } from './humanSession';
import { projectApi } from './projectApi';
import type { AvsDocument, AvsImport, ProjectResource, ProjectSession, ResourcePatch } from './projectContracts';
import { errorText } from './components/ui';

type Editor = { dirty: boolean; flush: () => Promise<void> };
interface ProjectWorkspaceValue {
  session?: ProjectSession; resource?: ProjectResource; selectedId: string; busy: boolean; error: string; localDirty: boolean;
  selectResource: (id: string) => Promise<void>;
  create: () => Promise<void>; open: (path?: string) => Promise<void>; save: () => Promise<void>; saveAs: () => Promise<void>; close: () => Promise<boolean>;
  recover: (value: ProjectSession['recoveries'][number]) => Promise<void>;
  importFiles: () => Promise<void>; exportScript: (id?: string) => Promise<void>;
  patchResource: (id: string, patch: ResourcePatch) => Promise<void>; relink: (id: string) => Promise<void>;
  rebindScript: (script: string) => Promise<void>;
  registerEditor: (key: string, editor: Editor) => () => void;
  refresh: () => Promise<void>; flushEditors: () => Promise<void>; setWorkspaceState: (state: Record<string, unknown>) => Promise<void>; history: (action: 'undo' | 'redo') => Promise<void>;
}
const ProjectContext = createContext<ProjectWorkspaceValue | null>(null);

async function pick(mode: string, defaultPath?: string): Promise<string[]> {
  if (!isDesktop()) throw new Error('文件操作请在 VAC 桌面窗口中使用。');
  return await desktopCommand<string[]>('project_dialog', { mode, defaultPath: defaultPath ?? null });
}

export function ProjectWorkspaceProvider({ children }: { children: ReactNode }) {
  const client = useQueryClient();
  const { modal, message } = App.useApp();
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (!isDesktop()) return;
    let active = true;
    const disposers: (() => void)[] = [];
    const consume = async () => {
      if (!active) return;
      const path = await desktopCommand<string | null>('project_open_request');
      if (path) window.dispatchEvent(new CustomEvent('vac:open-project', { detail: path }));
    };
    void (async () => {
      const openDispose = await projectEventListener('project-open-requested', () => { void consume(); });
      const closeDispose = await projectEventListener('project-close-requested', () => window.dispatchEvent(new Event('vac:request-exit')));
      if (!active) { openDispose(); closeDispose(); return; }
      disposers.push(openDispose, closeDispose); await consume();
    })().catch(reason => { if (active) setError(errorText(reason)); });
    return () => { active = false; disposers.forEach(dispose => dispose()); };
  }, []);
  const [error, setError] = useState('');
  const [selectedId, setSelectedId] = useState('');
  const [localDirty, setLocalDirty] = useState(false);
  const editors = useRef(new Map<string, Editor>());
  const activeSession = useRef('');
  const operation = useRef(false);
  const operationDone = useRef<Promise<void>>(Promise.resolve());
  const exitRequested = useRef(false);
  const workspace = useQuery({ queryKey: ['project-workspace'], queryFn: ({ signal }) => projectApi.workspace(signal), staleTime: 0, refetchInterval: busy ? false : 2000 });
  const session = workspace.data;
  useEffect(() => {
    if (!session) return;
    setProjectSession(session.id);
    if (activeSession.current !== session.id) { pauseMediaPlayers(); editors.current.clear(); setLocalDirty(false); setSelectedId(String(session.document?.workspace?.selected_resource ?? '')); }
    activeSession.current = session.id;
  }, [session?.id]);
  const flushEditors = async () => { for (const editor of [...editors.current.values()]) if (editor.dirty) await editor.flush(); };
  useEffect(() => {
    const timer = window.setInterval(() => { if (!operation.current && [...editors.current.values()].some(editor => editor.dirty)) void flushEditors().catch(reason => setError(errorText(reason))); }, 15000);
    return () => window.clearInterval(timer);
  }, []);
  const accept = async (next: ProjectSession) => {
    const switched = activeSession.current !== next.id;
    if (switched) {
      pauseMediaPlayers(); await client.cancelQueries();
      client.removeQueries({ predicate: query => query.queryKey[0] !== 'project-workspace' });
      editors.current.clear(); setLocalDirty(false); setSelectedId(String(next.document?.workspace?.selected_resource ?? ''));
    }
    activeSession.current = next.id; setProjectSession(next.id); client.setQueryData(['project-workspace'], next);
    if (!switched) void Promise.all(['assets', 'visible-set', 'vu-project', 'vu-visible', 'scripts', 'script-media'].map(key => client.invalidateQueries({ queryKey: [key] })));
  };
  const run = async (action: () => Promise<void>) => {
    if (operation.current) return;
    let complete!: () => void;
    operationDone.current = new Promise<void>(resolve => { complete = resolve; });
    operation.current = true; setBusy(true); setError('');
    try { await action(); } catch (reason) { setError(errorText(reason)); throw reason; }
    finally { operation.current = false; setBusy(false); complete(); }
  };
  const leaveDecision = (view: ProjectSession, clone = false): Promise<'save' | 'discard' | 'cancel'> => {
    if ((!view.dirty || clone) && view.active_tasks === 0) return Promise.resolve('discard');
    return new Promise(resolve => {
      const dialog = modal.confirm({
        title: clone ? '另存为工程' : '离开当前工程',
        content: <Space direction="vertical">{view.dirty && !clone && <span>当前工程有未保存的更改。</span>}{view.active_tasks > 0 && <span>将停止当前工程的 {view.active_tasks} 个任务，等待退出后继续。</span>}</Space>,
        onCancel: () => resolve('cancel'),
        footer: <Space>
          <Button onClick={() => { dialog.destroy(); resolve('cancel'); }}>取消</Button>
          {view.dirty && !clone && <Button onClick={() => { dialog.destroy(); resolve('discard'); }}>不保存并继续</Button>}
          <Button type="primary" onClick={() => { dialog.destroy(); resolve(view.dirty && !clone ? 'save' : 'discard'); }}>{view.dirty && !clone ? '保存并继续' : '停止任务并继续'}</Button>
        </Space>
      });
    });
  };
  const change = async (action: string, value: { path?: string; name?: string; recovery_path?: string } = {}): Promise<boolean> => {
    if (action === 'open' && value.path) await projectApi.inspectProject(value.path);
    await flushEditors();
    const view = await projectApi.workspace();
    const decision = await leaveDecision(view, action === 'save-as'); if (decision === 'cancel') return false;
    pauseMediaPlayers(); await client.cancelQueries();
    const next = await projectApi.command(action, { ...value, decision, stop_tasks: true, expected_session: view.id });
    await accept(next); return true;
  };
  const create = () => run(async () => {
    const [path] = await pick('new-project', '新项目.avp'); if (!path) return;
    const name = path.split(/[\\/]/).at(-1)!.replace(/\.avp$/i, '');
    await change('new', { path, name });
  });
  const open = (path?: string) => run(async () => { const target = path ?? (await pick('open-project'))[0]; if (target) await change('open', { path: target }); });
  const save = () => run(async () => { if (!session?.document) return; await flushEditors(); await accept(await projectApi.command('save', { expected_session: activeSession.current })); void message.success('工程已保存'); });
  const saveAs = () => run(async () => { const [path] = await pick('save-project', session?.path); if (path) await change('save-as', { path }); });
  const close = async () => { let closed = false; await run(async () => { closed = await change('close'); }); return closed; };
  const recover = (value: ProjectSession['recoveries'][number]) => run(async () => { await change('recover', { path: value.path, recovery_path: value.recovery_path }); });
  const selectResource = async (id: string) => { if (id === selectedId) return; try { await flushEditors(); setSelectedId(id); } catch (reason) { setError(errorText(reason)); } };
  const patchResource = async (id: string, patch: ResourcePatch) => {
    const projectId = session?.document?.project.id; if (!projectId) return;
    const started = activeSession.current;
    const next = await projectApi.patchResource(projectId, id, patch);
    if (activeSession.current === started) await accept(next);
  };
  const relink = (id: string) => run(async () => { const [path] = await pick('relink-resource'); if (path) await patchResource(id, { path, removed: false }); });
  const [binding, setBinding] = useState<{ document: AvsDocument; path: string; resolve: (value?: AvsImport) => void }>();
  const [bindingIDs, setBindingIDs] = useState<Record<string, string>>({});
  const [bindingFiles, setBindingFiles] = useState<Record<string, string>>({});
  const importFiles = () => run(async () => {
    const projectId = session?.document?.project.id; if (!projectId) return;
    const paths = await pick('import-resources');
    await flushEditors();
    for (const path of paths) {
      let next: ProjectSession;
      if (/\.avs$/i.test(path)) {
        const document = await projectApi.inspectScript(path);
        let value: AvsImport | undefined = { path };
        if (document.bindings.length) {
          setBindingIDs({}); setBindingFiles({});
          value = await new Promise<AvsImport | undefined>(resolve => setBinding({ document, path, resolve }));
          if (!value) continue;
        }
        next = await projectApi.importScript(projectId, value);
      } else next = await projectApi.importResource(projectId, path);
      await accept(next); setSelectedId(next.document?.resources.at(-1)?.id ?? '');
    }
  });
  const rebindScript = (id: string) => run(async () => {
    await flushEditors(); const view = await projectApi.workspace(); const document = view.document?.scripts.find(script => script.id === id); if (!document || !view.document) return;
    setBindingIDs(Object.fromEntries(document.bindings.filter(binding => binding.resource_id).map(binding => [binding.id, binding.resource_id!]))); setBindingFiles({});
    const value = await new Promise<AvsImport | undefined>(resolve => setBinding({ document, path: '', resolve })); setBinding(undefined);
    if (value) await accept(await projectApi.bindScript(view.document.project.id, id, value));
  });
  const exportScript = (scriptId?: string) => run(async () => {
    const projectId = session?.document?.project.id; if (!projectId) return;
    const script = scriptId ?? session.document?.resources.find(r => r.id === selectedId)?.script_id;
    if (!script) throw new Error('请先选择脚本，或在脚本工作台中导出。');
    const [path] = await pick('save-script', '脚本.avs'); if (!path) return;
    await flushEditors(); await projectApi.exportScript(projectId, script, path); void message.success('脚本已导出');
  });
  const registerEditor = useCallback((key: string, editor: Editor) => {
    editors.current.set(key, editor); setLocalDirty([...editors.current.values()].some(v => v.dirty));
    return () => { if (editors.current.get(key) === editor) editors.current.delete(key); setLocalDirty([...editors.current.values()].some(v => v.dirty)); };
  }, []);
  const setWorkspaceState = async (state: Record<string, unknown>) => { if (!session?.document) return; const started = activeSession.current; const next = await projectApi.state(started, { ...session.document.workspace, ...state }); if (activeSession.current === started) { client.setQueryData(['project-workspace'], next); } };
  const history = (action: 'undo' | 'redo') => run(async () => { await flushEditors(); await accept(await projectApi.command(action, { expected_session: activeSession.current })); });
  const refresh = async () => { await workspace.refetch(); };
  const resource = session?.document?.resources.find(r => r.id === selectedId);
  const report = (reason: unknown) => setError(errorText(reason));
  useEffect(() => {
    const closeRequested = async () => {
      if (exitRequested.current) return;
      exitRequested.current = true;
      try {
        await operationDone.current;
        if (await close()) await desktopCommand('confirm_project_exit');
      } catch (reason) { report(reason); }
      finally { exitRequested.current = false; }
    };
    const fileOpened = (event: Event) => { const path = (event as CustomEvent<string>).detail; if (path) void open(path).catch(report); };
    const beforeInstall = (event: Event) => { event.preventDefault(); const complete = (event as CustomEvent<{ complete: (ok: boolean) => void }>).detail.complete; void close().then(complete).catch(reason => { report(reason); complete(false); }); };
    window.addEventListener('vac:before-install', beforeInstall);
    const browserClose = (event: BeforeUnloadEvent) => { if (session?.dirty || localDirty) { event.preventDefault(); event.returnValue = ''; } };
    window.addEventListener('vac:request-exit', closeRequested);
    window.addEventListener('vac:open-project', fileOpened);
    window.addEventListener('beforeunload', browserClose);
    return () => { window.removeEventListener('vac:request-exit', closeRequested); window.removeEventListener('vac:open-project', fileOpened); window.removeEventListener('beforeunload', browserClose); window.removeEventListener('vac:before-install', beforeInstall); };
  }, [session, localDirty]);
  return <ProjectContext.Provider value={{ session, resource, selectedId, busy, error: error || (workspace.error ? errorText(workspace.error) : ''), localDirty, selectResource, create, open, save, saveAs, close, recover, importFiles, exportScript, patchResource, relink, registerEditor, refresh, flushEditors, history, setWorkspaceState, rebindScript }}>
    {children}
    <Modal title="关联脚本素材" open={Boolean(binding)} onCancel={() => { binding?.resolve(); setBinding(undefined); }}
      footer={<Space><Button onClick={() => { binding?.resolve(); setBinding(undefined); }}>取消</Button><Button disabled={!binding?.path} onClick={() => { binding?.resolve({ path: binding.path, script_only: true }); setBinding(undefined); }}>仅导入脚本</Button><Button type="primary" disabled={!Object.values(bindingIDs).some(Boolean) && !Object.values(bindingFiles).some(Boolean)} onClick={() => { binding?.resolve({ path: binding.path, bindings: bindingIDs, files: bindingFiles }); setBinding(undefined); }}>关联并导入</Button></Space>}>
      <Typography.Paragraph>选择本工程中的资源或本地文件。仅导入脚本会把原绑定保留为待关联项。</Typography.Paragraph>
      <Flex vertical gap={16}>{binding?.document.bindings.map(item => <div key={item.id}>
        <Typography.Text strong>{item.name}</Typography.Text>
        <Flex gap={8} style={{ marginTop: 8 }}><Select aria-label={`关联 ${item.name}`} allowClear placeholder="选择当前工程资源" style={{ flex: 1 }} value={bindingIDs[item.id]} onChange={value => { setBindingIDs(v => ({ ...v, [item.id]: value ?? '' })); setBindingFiles(v => ({ ...v, [item.id]: '' })); }} options={session?.document?.resources.filter(r => !r.removed && !r.missing && (r.kind === 'video' || r.kind === 'audio')).map(r => ({ value: r.id, label: r.name }))} />
          <Button onClick={() => void pick('relink-resource').then(([path]) => { if (path) { setBindingFiles(v => ({ ...v, [item.id]: path })); setBindingIDs(v => ({ ...v, [item.id]: '' })); } }).catch(report)}>选择文件</Button></Flex>
        {bindingFiles[item.id] && <Typography.Text type="secondary">{bindingFiles[item.id]}</Typography.Text>}
      </div>)}</Flex>
    </Modal>
  </ProjectContext.Provider>;
}

export function useProjectWorkspace(): ProjectWorkspaceValue {
  const value = useContext(ProjectContext); if (!value) throw new Error('工程工作区尚未初始化'); return value;
}
