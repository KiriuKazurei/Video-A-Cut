import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, Button, Card, Flex, List, Space, Tag, Typography } from 'antd';
import { skillApi } from '../skillApi';
import { skillPreviewSource } from '../skillEditing';
import { skillQueryKeys } from '../skillContracts';
import { useSkillWorkspace } from '../skillWorkspace';
import { Monitor, type PreviewSource } from './Monitor';
import { ProtectedError } from './HumanSessionControl';
import { errorText } from './ui';
import { isVersionConflict, newIdempotencyKey, protectedQueryRetry, runDetailKey, sameRevisionDelivery, versionConflictMessage } from '../skillView';

/** Same-revision MP4 / Premiere delivery and labelled historical previews. */
export function SkillDeliveryModule() {
  const workspace = useSkillWorkspace();
  const client = useQueryClient();
  const [preview, setPreview] = useState<PreviewSource>(null);
  const [notice, setNotice] = useState('');
  const [exportKey, setExportKey] = useState(() => newIdempotencyKey('export'));
  const enabled = Boolean(workspace.projectId && workspace.runId);
  const run = useQuery({
    queryKey: enabled ? skillQueryKeys.run(workspace.projectId, workspace.runId) : ['vu-run', 'delivery-idle'],
    queryFn: ({ signal }) => skillApi.run(workspace.runId, signal),
    enabled,
    retry: protectedQueryRetry
  });
  const workbench = useQuery({
    queryKey: enabled ? runDetailKey(workspace.projectId, workspace.runId, 'workbench') : ['vu-run', 'delivery-workbench-idle'],
    queryFn: ({ signal }) => skillApi.workbench(workspace.runId, signal),
    enabled,
    retry: protectedQueryRetry
  });
  useEffect(() => { setPreview(null); }, [workspace.epoch, workspace.runId]);
  const deliver = useMutation({
    mutationFn: () => skillApi.exportProject(workspace.projectId, {
      run_id: workspace.runId,
      expected_version: run.data!.version,
      idempotency_key: exportKey
    }),
    onSuccess: async () => {
      setNotice('已按当前定稿请求同版 MP4 与 Premiere 包。');
      setExportKey(newIdempotencyKey('export'));
      workspace.markUnlocked();
      await client.invalidateQueries({ queryKey: ['vu-run', workspace.projectId, workspace.runId] });
    }
  });
  const pair = sameRevisionDelivery(run.data?.artifacts ?? [], run.data?.plan?.version);
  const error = run.error || workbench.error || deliver.error;

  return <Card size="small" className="skill-delivery-module" title="Skill 运行交付">
    <Flex vertical gap={12}>
      {!enabled && <Typography.Text type="secondary">在编辑页选择项目中的 Skill 运行后，这里显示同版成片和历史预览。</Typography.Text>}
      {error ? <ProtectedError error={error} onRetry={() => { void run.refetch(); void workbench.refetch(); }} /> : null}
      {notice && <Alert type="info" showIcon message={notice} />}
      {deliver.isError && isVersionConflict(deliver.error) && <Alert type="warning" showIcon message={versionConflictMessage()} />}
      {run.data && <Alert type={pair.aligned ? 'success' : 'warning'} showIcon
        message={pair.aligned ? `计划 v${run.data.plan?.version} 的 MP4 与 Premiere 包为同一修订` : '当前计划修订还没有成对的 MP4 与 Premiere 包'}
        description="历史成片只能浏览，不能直接当作新的修改基准。正式导出要求当前定稿和内容摘要一致。" />}
      <Monitor label="Skill 成片" preview={preview} compact
        emptyTitle="未选择 Skill 预览"
        emptyHint="选择当前成片或带有历史标记的备份。历史预览不会授权新的修改。" />
      <List size="small" dataSource={workbench.data?.previews ?? []} locale={{ emptyText: '没有可播放的 Skill 产物' }}
        renderItem={(item) => {
          const source = skillPreviewSource(workspace.runId, item);
          return <List.Item actions={[source ? <Button key="play" type="link" onClick={() => setPreview({
            kind: 'video', src: source.src, title: item.artifact.name,
            meta: item.historical ? '历史预览' : item.is_current ? '当前成片' : '预览',
            note: item.historical ? '历史预览，只读，不能据此发起新修改或作为正式交付。' : '当前工程的只读预览。'
          })}>播放</Button> : <Typography.Text key="missing" type="secondary">不可播</Typography.Text>]}>
            <Space wrap>
              <Typography.Text>{item.artifact.name}</Typography.Text>
              <Tag>{item.artifact.kind}</Tag>
              {item.historical && <Tag color="warning">历史预览</Tag>}
              {item.is_current && <Tag color="processing">当前</Tag>}
              {item.exportable && !item.historical && <Tag color="success">可导出</Tag>}
              {item.artifact.plan_version != null && <Typography.Text type="secondary">计划 v{item.artifact.plan_version}</Typography.Text>}
            </Space>
          </List.Item>;
        }} />
      <Space wrap>
        {pair.mp4 && <Typography.Text>MP4 {pair.mp4.name}</Typography.Text>}
        {pair.premiere && <Typography.Text>Premiere {pair.premiere.name}</Typography.Text>}
        {pair.aligned && pair.mp4 && <a href={skillApi.artifactURL(workspace.runId, pair.mp4.id)}>下载当前 MP4</a>}
        {pair.aligned && pair.premiere && <a href={skillApi.artifactURL(workspace.runId, pair.premiere.id)}>下载当前 Premiere 包</a>}
      </Space>
      <Button type="primary" disabled={!enabled || !run.data?.plan?.finalized_by} loading={deliver.isPending} onClick={() => deliver.mutate()}>
        导出当前定稿
      </Button>
      {run.data?.plan && !run.data.plan.finalized_by && <Typography.Text type="secondary">计划尚未人工定稿，不能把产物当作正式同版交付。</Typography.Text>}
      {deliver.isError && !isVersionConflict(deliver.error) && <Typography.Text type="danger">{errorText(deliver.error)}</Typography.Text>}
    </Flex>
  </Card>;
}
