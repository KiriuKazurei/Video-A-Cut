import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, Button, Card, Collapse, Descriptions, Flex, Form, Input, List, Space, Switch, Tag, Typography } from 'antd';
import { skillApi } from '../skillApi';
import { skillQueryKeys, type SkillPackage } from '../skillContracts';
import { useSkillWorkspace } from '../skillWorkspace';
import { ProtectedError } from './HumanSessionControl';
import { errorText } from './ui';
import { FLOW_LABEL, protectedQueryRetry } from '../skillView';

export function SkillManagerModule() {
  const workspace = useSkillWorkspace();
  const client = useQueryClient();
  const [path, setPath] = useState('');
  const [selected, setSelected] = useState('');
  const [notice, setNotice] = useState('');
  const skills = useQuery({ queryKey: skillQueryKeys.skills, queryFn: () => skillApi.skills(), retry: protectedQueryRetry });
  const detail = useQuery({
    queryKey: selected ? [...skillQueryKeys.skills, selected] : [...skillQueryKeys.skills, 'none'],
    queryFn: () => skillApi.skill(selected),
    enabled: Boolean(selected),
    retry: protectedQueryRetry
  });
  const ffmpeg = useQuery({
    queryKey: [...skillQueryKeys.skills, 'ffmpeg'],
    queryFn: () => skillApi.ffmpeg(),
    retry: protectedQueryRetry
  });
  const refresh = () => client.invalidateQueries({ queryKey: skillQueryKeys.skills });
  const action = useMutation({
    mutationFn: (fn: () => Promise<unknown>) => fn(),
    onSuccess: async () => { setNotice(''); workspace.markUnlocked(); await refresh(); if (selected) await client.invalidateQueries({ queryKey: [...skillQueryKeys.skills, selected] }); },
    onError: (reason) => setNotice(errorText(reason))
  });
  const validate = useMutation({
    mutationFn: () => skillApi.validateSkill(selected),
    onSuccess: () => workspace.markUnlocked()
  });
  const shown = detail.data;
  const authError = skills.error || detail.error || ffmpeg.error;

  return <Card className="skill-manager-module" title="Skill 与依赖" extra={<Tag>{skills.data ? `${skills.data.length} 个包` : '读取中'}</Tag>}>
    <Flex vertical gap={12}>
      <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
        导入、启停、查看正文和校验依赖都在设置里完成。项目默认 Skill 与禁用列表写在资源页的项目配置中。
      </Typography.Paragraph>
      {authError ? <ProtectedError error={authError} onRetry={() => { void skills.refetch(); void ffmpeg.refetch(); }} /> : null}
      {notice && <Alert type={action.isError ? 'error' : 'info'} showIcon message={notice} />}
      <Form layout="inline" onFinish={() => action.mutate(async () => { const pack = await skillApi.importSkill(path.trim()); setSelected(pack.id); setPath(''); setNotice(`已导入 ${pack.name}`); })}>
        <Form.Item label="本地目录" style={{ flex: '1 1 280px' }}>
          <Input aria-label="Skill 目录" value={path} onChange={(event) => setPath(event.target.value)} placeholder="含 SKILL.md 与 skill.json 的目录" />
        </Form.Item>
        <Form.Item><Button type="primary" htmlType="submit" disabled={!path.trim()} loading={action.isPending}>导入 Skill</Button></Form.Item>
      </Form>
      <List size="small" loading={skills.isFetching} dataSource={skills.data ?? []} locale={{ emptyText: '还没有 Skill 包' }}
        renderItem={(item: SkillPackage) => <List.Item actions={[
          <Switch key="enabled" aria-label={`${item.name} 启停`} checked={item.enabled} loading={action.isPending} onChange={(enabled) => action.mutate(() => skillApi.enableSkill(item.id, enabled))} />,
          <Button key="open" type="link" onClick={() => setSelected(item.id)}>查看</Button>
        ]}>
          <List.Item.Meta title={item.name} description={`${item.id} · ${item.version} · ${item.enabled ? '启用' : '停用'} · ${item.compatible_flows.map((flow) => FLOW_LABEL[flow] ?? flow).join('、') || '未声明流程'}`} />
        </List.Item>} />
      {shown && <Descriptions column={1} title={shown.name} items={[
        { key: 'id', label: '标识', children: <Typography.Text code>{shown.id}</Typography.Text> },
        { key: 'sha', label: '内容摘要', children: <Typography.Text code>{shown.content_sha256}</Typography.Text> },
        { key: 'runtime', label: '运行时', children: shown.runtime },
        { key: 'modes', label: '执行模式', children: shown.execution_modes.join('、') || '—' },
        { key: 'tools', label: '工具', children: shown.tools.join('、') || '—' }
      ]} />}
      {shown && <Collapse items={[{
        key: 'content',
        label: '正文与文本资源',
        children: <Flex vertical gap={8}>
          <pre className="code-block">{shown.content || '（无正文）'}</pre>
          <List size="small" dataSource={Object.entries(shown.resources)} locale={{ emptyText: '没有文本资源' }}
            renderItem={([name, body]) => <List.Item><Typography.Text code>{name}</Typography.Text><pre className="code-block">{body}</pre></List.Item>} />
        </Flex>
      }]} />}
      <Space wrap>
        <Button disabled={!selected} loading={validate.isPending} onClick={() => validate.mutate()}>校验所选 Skill</Button>
        <Button onClick={() => void ffmpeg.refetch()}>重新读取 FFmpeg</Button>
      </Space>
      {validate.isError && <Alert type="error" showIcon message={errorText(validate.error)} />}
      {validate.data && <Descriptions column={1} title="校验结果" items={Object.entries(validate.data).map(([key, value]) => ({
        key, label: key, children: typeof value === 'string' ? value : JSON.stringify(value)
      }))} />}
      <Descriptions column={{ xs: 1, md: 2 }} title="FFmpeg 能力" items={[
        { key: 'available', label: 'ffmpeg', children: ffmpeg.data ? (ffmpeg.data.available ? `可用 ${ffmpeg.data.version}` : '不可用') : '读取中' },
        { key: 'probe', label: 'ffprobe', children: ffmpeg.data?.ffprobe ? '可用' : '不可用' },
        { key: 'x264', label: 'libx264', children: ffmpeg.data?.libx264 ? '可用' : '不可用' },
        { key: 'aac', label: 'aac', children: ffmpeg.data?.aac ? '可用' : '不可用' },
        { key: 'sub', label: '字幕', children: ffmpeg.data?.subtitles ? '可用' : '不可用' },
        { key: 'hw', label: '硬件加速', children: ffmpeg.data?.hardware_acceleration || '未报告' }
      ]} />
    </Flex>
  </Card>;
}
