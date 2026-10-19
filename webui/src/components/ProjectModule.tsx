import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, Button, Card, Checkbox, Flex, Form, Input, Space, Tag, Typography } from 'antd';
import { api, ingestApi } from '../api';
import { skillApi } from '../skillApi';
import { skillQueryKeys, type ProjectSave, type SkillFlow, type VacProject } from '../skillContracts';
import { useSkillWorkspace } from '../skillWorkspace';
import { ProtectedError } from './HumanSessionControl';
import { ValueSelect, errorText } from './ui';
import { FLOW_LABEL, consentAfterVisibility, isVersionConflict, newIdempotencyKey, projectDraft, protectedQueryRetry, sameIds, versionConflictMessage } from '../skillView';

const FLOWS: SkillFlow[] = ['lightweight', 'visual_heavy', 'custom'];

export function ProjectModule() {
  const workspace = useSkillWorkspace();
  const client = useQueryClient();
  const [draft, setDraft] = useState<ProjectSave | null>(null);
  const [name, setName] = useState('新项目');
  const [flow, setFlow] = useState<SkillFlow>('lightweight');
  const [rootId, setRootId] = useState('');
  const [relativePath, setRelativePath] = useState('');
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const seenProject = useRef('');
  const projects = useQuery({
    queryKey: skillQueryKeys.projects,
    queryFn: ({ signal }) => skillApi.projects(signal),
    retry: protectedQueryRetry
  });
  const project = useQuery({
    queryKey: workspace.projectId ? skillQueryKeys.project(workspace.projectId) : ['vu-project', 'none'],
    queryFn: ({ signal }) => skillApi.project(workspace.projectId, signal),
    enabled: Boolean(workspace.projectId),
    retry: protectedQueryRetry
  });
  const visible = useQuery({
    queryKey: workspace.projectId ? skillQueryKeys.visible(workspace.projectId) : ['vu-visible', 'none'],
    queryFn: ({ signal }) => skillApi.visibleSet(workspace.projectId, signal),
    enabled: Boolean(workspace.projectId),
    retry: protectedQueryRetry
  });
  const skills = useQuery({ queryKey: skillQueryKeys.skills, queryFn: () => skillApi.skills(), retry: protectedQueryRetry });
  const profiles = useQuery({ queryKey: ['processing-profiles'], queryFn: api.listProfiles });
  const roots = useQuery({ queryKey: ['ingest', 'roots'], queryFn: ingestApi.roots });

  useEffect(() => {
    if (projects.isSuccess) workspace.markUnlocked();
  }, [projects.isSuccess]);
  useEffect(() => {
    const current = project.data;
    if (!current) return;
    const key = `${current.id}:${current.version}`;
    if (seenProject.current === key) return;
    seenProject.current = key;
    setDraft(projectDraft(current));
  }, [project.data]);
  useEffect(() => {
    const reset = () => {
      if (document.visibilityState !== 'hidden') return;
      setDraft((current) => {
        if (!current) return current;
        const next = consentAfterVisibility(true, { external: Boolean(current.accept_external), transcription: Boolean(current.accept_transcription) });
        return { ...current, accept_external: next.external, accept_transcription: next.transcription };
      });
    };
    document.addEventListener('visibilitychange', reset);
    return () => document.removeEventListener('visibilitychange', reset);
  }, []);

  const refresh = async (saved?: VacProject) => {
    if (saved) workspace.setProjectId(saved.id);
    await Promise.all([
      client.invalidateQueries({ queryKey: skillQueryKeys.projects }),
      workspace.projectId ? client.invalidateQueries({ queryKey: skillQueryKeys.project(saved?.id || workspace.projectId) }) : Promise.resolve(),
      workspace.projectId ? client.invalidateQueries({ queryKey: skillQueryKeys.visible(saved?.id || workspace.projectId) }) : Promise.resolve()
    ]);
  };
  const fail = (reason: unknown) => {
    setError(isVersionConflict(reason) ? versionConflictMessage() : errorText(reason));
    void refresh();
  };
  const action = useMutation({ mutationFn: (fn: () => Promise<VacProject | unknown>) => fn(), onSuccess: () => setError(''), onError: fail });

  const create = () => action.mutate(async () => {
    const saved = await skillApi.createProject({ name: name.trim(), flow, expected_version: 0, assets: [], visible: [] });
    setNotice(`已创建项目 ${saved.name}`);
    workspace.markUnlocked();
    await refresh(saved);
    return saved;
  });
  const save = () => {
    if (!draft || !workspace.projectId) return;
    action.mutate(async () => {
      const saved = await skillApi.saveProject(workspace.projectId, { ...draft, expected_version: project.data?.version ?? draft.expected_version });
      setDraft(projectDraft(saved));
      setNotice('项目配置已保存。切换流程或预设后，旧的可见集确认和运行上下文会失效。');
      await refresh(saved);
    });
  };
  const confirm = () => {
    if (!visible.data || !project.data) return;
    action.mutate(async () => {
      await skillApi.confirmVisible(workspace.projectId, visible.data!.sha256, project.data!.version);
      setNotice('已确认当前项目可见集。请重新读取项目版本后再创建运行。');
      const next = await skillApi.project(workspace.projectId);
      setDraft(projectDraft(next));
      await refresh(next);
    });
  };
  const importMedia = () => {
    if (!project.data || !rootId || !relativePath.trim()) return;
    action.mutate(async () => {
      const saved = await skillApi.importMedia(workspace.projectId, {
        root_id: rootId,
        relative_path: relativePath.trim(),
        expected_version: project.data!.version,
        idempotency_key: newIdempotencyKey('import')
      });
      setRelativePath('');
      setDraft(projectDraft(saved));
      setNotice('素材已入库。它还不会自动进入可见集，需要勾选并确认。');
      await refresh(saved);
    });
  };

  const savedVisible = project.data?.visible ?? [];
  const draftMatches = Boolean(draft && sameIds(draft.visible, savedVisible));
  const confirmed = Boolean(visible.data?.confirmed && project.data?.confirmed_sha && project.data.confirmed_sha === visible.data.sha256);
  const skillOptions = (skills.data ?? []).map((item) => ({ value: item.id, label: `${item.name} · ${item.version}${item.enabled ? '' : '（已停用）'}` }));
  const authError = projects.error || project.error || visible.error;

  return <Card size="small" className="project-module" title="项目与可见集" extra={workspace.projectId ? <Tag>{workspace.projectId}</Tag> : <Tag>未选择项目</Tag>}>
    <Flex vertical gap={12}>
      <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
        这里管理项目级 scope v2 可见集。下方资产库里的全局可见集确认不能代替本项目的确认。
      </Typography.Paragraph>
      {authError ? <ProtectedError error={authError} onRetry={() => { void projects.refetch(); void project.refetch(); void visible.refetch(); }} /> : null}
      {error && <Alert type="error" showIcon message={error} />}
      {notice && <Alert type="info" showIcon message={notice} />}
      <Flex gap={8} wrap align="flex-end">
        <Form.Item label="项目" layout="vertical" style={{ marginBottom: 0, minWidth: 220, flex: '1 1 220px' }}>
          <ValueSelect<string> aria-label="项目" value={workspace.projectId} onChange={workspace.setProjectId}
            options={[{ value: '', label: '选择项目' }, ...(projects.data ?? []).map((item) => ({ value: item.id, label: `${item.name} · v${item.version}` }))]} />
        </Form.Item>
        <Form.Item label="新项目名称" layout="vertical" style={{ marginBottom: 0, minWidth: 180 }}>
          <Input aria-label="新项目名称" value={name} onChange={(event) => setName(event.target.value)} />
        </Form.Item>
        <Form.Item label="新项目流程" layout="vertical" style={{ marginBottom: 0, minWidth: 180 }}>
          <ValueSelect<SkillFlow> aria-label="新项目流程" value={flow} onChange={setFlow} options={FLOWS.map((item) => ({ value: item, label: FLOW_LABEL[item] }))} />
        </Form.Item>
        <Button type="primary" disabled={!name.trim()} loading={action.isPending} onClick={create}>创建项目</Button>
      </Flex>
      {draft && project.data && <>
        <Form layout="vertical" onFinish={save}>
          <Flex gap={8} wrap>
            <Form.Item label="名称" style={{ minWidth: 180, flex: '1 1 180px' }}><Input aria-label="项目名称" value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} /></Form.Item>
            <Form.Item label="流程" style={{ minWidth: 180 }}><ValueSelect<SkillFlow> aria-label="项目流程" value={draft.flow} onChange={(value) => setDraft({ ...draft, flow: value })} options={FLOWS.map((item) => ({ value: item, label: FLOW_LABEL[item] }))} /></Form.Item>
            <Form.Item label="固定预设" style={{ minWidth: 220, flex: '1 1 220px' }}>
              <ValueSelect<string> aria-label="项目固定预设" value={draft.profile_id ?? ''} onChange={(value) => {
                const profile = profiles.data?.find((item) => item.profile_id === value);
                setDraft({ ...draft, profile_id: value || undefined, profile_revision: profile?.revision });
              }} options={[{ value: '', label: '不固定' }, ...(profiles.data ?? []).map((item) => ({ value: item.profile_id, label: `${item.name} · v${item.revision}` }))]} />
            </Form.Item>
          </Flex>
          <Flex gap={8} wrap>
            <Form.Item label="流程 A 默认 Skill" style={{ minWidth: 220, flex: 1 }}><ValueSelect<string> aria-label="流程 A 默认 Skill" value={draft.default_skills?.lightweight ?? ''} onChange={(value) => setDraft({ ...draft, default_skills: { ...draft.default_skills, lightweight: value || undefined } })} options={[{ value: '', label: '未指定' }, ...skillOptions]} /></Form.Item>
            <Form.Item label="流程 B 默认 Skill" style={{ minWidth: 220, flex: 1 }}><ValueSelect<string> aria-label="流程 B 默认 Skill" value={draft.default_skills?.visual_heavy ?? ''} onChange={(value) => setDraft({ ...draft, default_skills: { ...draft.default_skills, visual_heavy: value || undefined } })} options={[{ value: '', label: '未指定' }, ...skillOptions]} /></Form.Item>
          </Flex>
          <Form.Item label="项目禁用的 Skill">
            <Checkbox.Group aria-label="禁用 Skill" value={draft.disabled_skills ?? []} onChange={(values) => setDraft({ ...draft, disabled_skills: values as string[] })} options={skillOptions} />
          </Form.Item>
          <Space wrap>
            <Checkbox checked={Boolean(draft.accept_external)} onChange={(event) => setDraft({ ...draft, accept_external: event.target.checked })}>允许项目外发</Checkbox>
            <Checkbox checked={Boolean(draft.accept_transcription)} onChange={(event) => setDraft({ ...draft, accept_transcription: event.target.checked })}>允许项目转录</Checkbox>
          </Space>
          <Typography.Paragraph type="secondary">外发和转录是两项独立授权。页面隐藏后这两项勾选会清空，需要重新确认。</Typography.Paragraph>
          <Typography.Text strong>项目素材</Typography.Text>
          <Checkbox.Group value={draft.visible} onChange={(values) => setDraft({ ...draft, visible: values as string[] })}
            options={(project.data.assets.length ? project.data.assets : visible.data?.media.map((item) => item.asset_id) ?? []).map((id) => {
              const media = visible.data?.media.find((item) => item.asset_id === id || item.id === id);
              return { value: id, label: media ? `${media.name || id}` : id };
            })} />
          <Space wrap>
            <Button type="primary" htmlType="submit" loading={action.isPending}>保存项目配置</Button>
            <Typography.Text type="secondary">保存使用当前版本 {project.data.version}，成功后以返回的新版本继续。</Typography.Text>
          </Space>
        </Form>
        <Flex gap={8} wrap align="flex-end">
          <Form.Item label="导入根" layout="vertical" style={{ marginBottom: 0, minWidth: 180 }}>
            <ValueSelect<string> aria-label="导入根" value={rootId} onChange={setRootId} options={[{ value: '', label: '选择 ingest 根' }, ...(roots.data?.roots ?? []).map((item) => ({ value: item.root_id, label: item.name || item.root_id }))]} />
          </Form.Item>
          <Form.Item label="相对路径" layout="vertical" style={{ marginBottom: 0, flex: '1 1 220px' }}>
            <Input aria-label="素材相对路径" value={relativePath} onChange={(event) => setRelativePath(event.target.value)} placeholder="相对于所选根目录" />
          </Form.Item>
          <Button disabled={!rootId || !relativePath.trim()} loading={action.isPending} onClick={importMedia}>导入到项目</Button>
        </Flex>
        <Alert type={confirmed ? 'success' : 'warning'} showIcon
          message={confirmed ? '项目可见集已确认' : '项目可见集尚未确认'}
          description={visible.data ? `scope v${visible.data.scope_version} · ${visible.data.sha256}` : '正在读取项目可见集'}
          action={<Button type="primary" disabled={!draftMatches || confirmed || !visible.data} loading={action.isPending} onClick={confirm}>确认项目可见集</Button>} />
        {!draftMatches && <Typography.Text type="secondary">可见素材勾选和已保存配置不一致。请先保存，再确认摘要。</Typography.Text>}
      </>}
    </Flex>
  </Card>;
}
