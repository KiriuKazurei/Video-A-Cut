import { useEffect, useRef } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Alert,
  Button,
  Card,
  Collapse,
  Descriptions,
  Empty,
  Flex,
  List,
  Progress,
  Space,
  Tag,
  Tooltip,
  Typography
} from 'antd';
import {
  CheckCircleOutlined,
  DownloadOutlined,
  PlayCircleOutlined,
  ReloadOutlined,
  SyncOutlined,
  VideoCameraOutlined
} from '@ant-design/icons';
import type { Asset } from '../types';
import type { RunSelection } from './WorkflowPanel';
import { useWorkflowData } from './WorkflowPanel';
import type { PreviewSource } from './Monitor';
import { previewApi, previewFileURL, previewQueryKey } from '../api';
import type { PreviewCheck, PreviewStartRequest, PreviewView } from '../previewContracts';
import { createPreviewSource, formatSeconds } from '../previewContracts';

export { createPreviewSource };

export interface PreviewPanelProps {
  asset?: Asset;
  selection: RunSelection;
  onSelectPreview?: (preview: PreviewSource) => void;
  activePreviewSrc?: string;
}

const CHECK_LABELS: Record<string, string> = {
  workflow_status: '工作流状态',
  export_revision: '导出版本',
  governance: '治理权限',
  worker_capability: 'Worker 就绪'
};

/**
 * 成片 MP4 预览面板（Phase D）：
 * 挂载于交付页，读取导出版本并受控生成、查询、取消与联动监视器播放。
 */
export function PreviewPanel({
  asset,
  selection,
  onSelectPreview,
  activePreviewSrc
}: PreviewPanelProps) {
  const queryClient = useQueryClient();
  const { current, runs } = useWorkflowData(asset?.asset_id ?? '', selection);
  const runId = current?.run_id ?? selection.runId;
  const revisionId = current?.current_revision_id ?? '';
  const contextKey = JSON.stringify([asset?.asset_id ?? '', runId, revisionId]);

  const query = useQuery({
    queryKey: previewQueryKey(asset?.asset_id ?? '', runId, revisionId),
    queryFn: () => previewApi.list(runId),
    enabled: Boolean(asset?.asset_id && runId)
  });
  const canStart = Boolean(runId && query.data?.can_start && !query.isError);

  // 初次生成生成唯一 request_id；传输重试复用；明确重试则生成新 request_id
  const requestIdRef = useRef<{ context: string; request: PreviewStartRequest } | null>(null);

  const startMutation = useMutation({
    mutationFn: ({ targetRun, request }: {
      targetRun: string;
      context: string;
      queryKey: ReturnType<typeof previewQueryKey>;
      request: PreviewStartRequest;
    }) => previewApi.start(targetRun, request),
    onSuccess: (_data, operation) => {
      void queryClient.invalidateQueries({
        queryKey: operation.queryKey
      });
    }
  });

  const cancelMutation = useMutation({
    mutationFn: ({ previewId }: {
      previewId: string;
      context: string;
      queryKey: ReturnType<typeof previewQueryKey>;
    }) => previewApi.cancel(previewId),
    onSuccess: (_data, operation) => {
      void queryClient.invalidateQueries({
        queryKey: operation.queryKey
      });
    }
  });
  const isStarting = startMutation.variables?.context === contextKey && startMutation.isPending;
  const hasStartError = startMutation.variables?.context === contextKey && startMutation.isError;
  const isCanceling = cancelMutation.variables?.context === contextKey && cancelMutation.isPending;
  const hasCancelError = cancelMutation.variables?.context === contextKey && cancelMutation.isError;

  const previews = query.data?.previews ?? [];
  const latestPreview = previews[0];

  const inProgressPreview = previews.find(
    (p) => p.status === 'queued' || p.status === 'claimed' || p.status === 'running'
  );

  const latestSucceededPreview = previews.find(
    (p) =>
      p.status === 'succeeded' &&
      !p.invalidated &&
      p.is_current && p.asset_id === asset?.asset_id && p.run_id === runId && p.revision_id === revisionId
  );

  // 自动联动：若最新预览处于 succeeded 且未失效且为当前导出 revision，送入 Monitor 播放
  const autoLinkedIdRef = useRef<string | null>(null);
  const prevContextKeyRef = useRef<string | null>(null);

  useEffect(() => {
    if (prevContextKeyRef.current !== contextKey) {
      if (prevContextKeyRef.current !== null) {
        startMutation.reset();
        cancelMutation.reset();
        requestIdRef.current = null;
      }
      prevContextKeyRef.current = contextKey;
      autoLinkedIdRef.current = null;
    }
  }, [contextKey, startMutation.reset, cancelMutation.reset]);

  useEffect(() => {
    if (!latestSucceededPreview || !onSelectPreview) return;
    const targetSrc = previewFileURL(latestSucceededPreview.preview_id, 'mp4');
    if (autoLinkedIdRef.current !== latestSucceededPreview.preview_id) {
      if (!activePreviewSrc || activePreviewSrc !== targetSrc) {
        onSelectPreview(createPreviewSource(latestSucceededPreview));
      }
      autoLinkedIdRef.current = latestSucceededPreview.preview_id;
    }
  }, [latestSucceededPreview, activePreviewSrc, onSelectPreview]);

  const handleStart = (explicitRetry = false) => {
    if (!canStart || isStarting || !runId || !query.data) return;
    const context = JSON.stringify([asset?.asset_id, runId, query.data.revision_id]);
    // A failed transport repeats its frozen body; a new action gets a new ID.
    if (explicitRetry || requestIdRef.current?.context !== context || !hasStartError) {
      requestIdRef.current = { context, request: {
        revision_id: query.data.revision_id,
        expected_version: query.data.expected_version,
        preset_id: 'mp4-720p-v1',
        request_id: crypto.randomUUID()
      } };
    }
    startMutation.mutate({
      targetRun: runId,
      context: contextKey,
      queryKey: previewQueryKey(asset?.asset_id ?? '', runId, revisionId),
      request: requestIdRef.current!.request
    });
  };
  const handleCancel = (previewId: string) => {
    if (isCanceling) return;
    cancelMutation.mutate({
      previewId,
      context: contextKey,
      queryKey: previewQueryKey(asset?.asset_id ?? '', runId, revisionId)
    });
  };

  const isBusy = isStarting || Boolean(inProgressPreview);

  return (
    <Card
      size="small"
      loading={query.isLoading}
      className="preview-panel glass glass-window"
      aria-labelledby="preview-panel-title"
      title={
        <Flex gap={8} align="center" wrap style={{ minWidth: 0 }}>
          <VideoCameraOutlined style={{ color: '#1677ff' }} />
          <Typography.Text strong id="preview-panel-title">
            合成成片 MP4 预览
          </Typography.Text>
        </Flex>
      }
      extra={
        query.data?.revision_id ? (
          <Tag color="cyan" style={{ marginInlineEnd: 0 }}>
            导出版本: {query.data.revision_id}
          </Tag>
        ) : undefined
      }
    >
      <Flex vertical gap={12} className="preview-panel-content">
        {!asset && (
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description="请先在左侧项目面板选择资产。"
          />
        )}

        {asset && runId && query.isError && (
          <Alert
            role="alert" type="error" showIcon message="读取预览失败"
            description={query.error instanceof Error ? query.error.message : '无法读取预览记录，请重试。'}
            action={
              <Button icon={<ReloadOutlined />} onClick={() => void query.refetch()} loading={query.isFetching}>
                重试读取
              </Button>
            }
          />
        )}
        {hasStartError && (
          <Alert
            role="alert" type="error" showIcon message="请求发起失败"
            description={startMutation.error instanceof Error ? startMutation.error.message : '网络或服务异常，请重试。'}
            action={
              <Button icon={<ReloadOutlined />} onClick={() => handleStart(false)} disabled={!canStart || isStarting}>
                重试发起
              </Button>
            }
          />
        )}
        {hasCancelError && (
          <Alert
            role="alert" type="error" showIcon message="取消预览失败"
            description={cancelMutation.error instanceof Error ? cancelMutation.error.message : '取消请求未完成，请重试。'}
            action={
              <Button danger icon={<ReloadOutlined />} onClick={() => {
                if (cancelMutation.variables) handleCancel(cancelMutation.variables.previewId);
              }} loading={isCanceling}>
                重试取消
              </Button>
            }
          />
        )}

        {asset && (!runId || (!query.isPending && !query.isError && !query.data?.revision_id)) && (
          <Alert
            type="info"
            showIcon
            message="前置条件未满足：暂无可导出的成片版本"
            description="当前工作流尚未成功完成导出（无 output_revision）。请先在「编辑」或「交付」流程中完成导出任务，不可变导出版就绪后即可生成 MP4 预览。"
          />
        )}

        {asset && query.data?.revision_id && (
          <>
            <Flex justify="space-between" align="center" wrap gap={8}>
              <Space wrap>
                <Button
                  type="primary"
                  icon={isBusy ? <SyncOutlined spin /> : <VideoCameraOutlined />}
                  onClick={() => handleStart(false)}
                  loading={isStarting}
                  disabled={!canStart || isBusy}
                >
                  {isBusy ? '渲染进行中…' : '生成 MP4 预览'}
                </Button>
                {latestPreview && (latestPreview.status === 'failed' || latestPreview.status === 'cancelled') && (
                  <Button
                    icon={<ReloadOutlined />}
                    onClick={() => handleStart(true)}
                    loading={isStarting}
                    disabled={!canStart || isStarting}
                  >
                    重试生成
                  </Button>
                )}
              </Space>

              {query.data.checks && query.data.checks.length > 0 && (
                <Flex wrap gap={6} className="preview-checks">
                  {query.data.checks.map((chk: PreviewCheck) => (
                    <Tooltip key={chk.code} title={chk.message}>
                      <Tag
                        color={chk.status === 'passed' ? 'success' : chk.status === 'blocked' ? 'error' : 'default'}
                        style={{ marginInlineEnd: 0, cursor: 'default' }}
                      >
                        {chk.status === 'passed' ? '✓ ' : chk.status === 'blocked' ? '✕ ' : '○ '}
                        {CHECK_LABELS[chk.code] ?? chk.code}
                      </Tag>
                    </Tooltip>
                  ))}
                </Flex>
              )}
            </Flex>

            {!query.data.can_start && query.data.checks?.some((c) => c.status === 'blocked') && (
              <Alert
                type="warning"
                showIcon
                message="预览生成受阻"
                description={query.data.checks
                  .filter((c) => c.status === 'blocked')
                  .map((c) => c.message)
                  .join('；')}
              />
            )}


            {inProgressPreview && (
              <Card
                size="small"
                className="preview-progress-card"
                style={{
                  background: 'rgba(22, 119, 255, 0.04)',
                  borderColor: 'rgba(22, 119, 255, 0.25)'
                }}
              >
                <Flex vertical gap={6}>
                  <Flex justify="space-between" align="center" wrap gap={8}>
                    <Space wrap>
                      <SyncOutlined spin style={{ color: '#1677ff' }} />
                      <Typography.Text strong>
                        {inProgressPreview.status === 'queued'
                          ? '预览任务排队中…'
                          : inProgressPreview.status === 'claimed'
                            ? 'Worker 已领取，准备环境中…'
                            : '成片渲染合成中…'}
                      </Typography.Text>
                      <Tag color="processing">{inProgressPreview.status}</Tag>
                    </Space>
                    <Button
                      size="small"
                      danger
                      onClick={() => handleCancel(inProgressPreview.preview_id)}
                      disabled={isCanceling}
                      loading={isCanceling && cancelMutation.variables?.previewId === inProgressPreview.preview_id}
                    >
                      取消任务
                    </Button>
                  </Flex>
                  <Progress
                    percent={Math.round((inProgressPreview.progress ?? 0) * 100)}
                    status="active"
                    strokeColor={{ from: '#108ee9', to: '#87d068' }}
                  />
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    {inProgressPreview.message ||
                      '渲染内核正在基于不可变导出版裁剪对齐并烧录中文字幕…'}
                  </Typography.Text>
                </Flex>
              </Card>
            )}

            {!inProgressPreview && latestPreview?.status === 'failed' && (
              <Alert
                type="error"
                showIcon
                message="成片预览生成失败"
                description={
                  <Flex vertical gap={8} style={{ marginTop: 4 }}>
                    <Typography.Text>
                      {latestPreview.message || '渲染内核未能成功合成成片 MP4，请检查日志或重试。'}
                    </Typography.Text>
                    <div>
                      <Button
                        size="small"
                        type="primary"
                        danger
                        icon={<ReloadOutlined />}
                        onClick={() => handleStart(true)}
                        loading={isStarting}
                        disabled={!canStart || isStarting}
                      >
                        重试生成
                      </Button>
                    </div>
                  </Flex>
                }
              />
            )}

            {!inProgressPreview && latestPreview?.status === 'cancelled' && (
              <Alert
                type="warning"
                showIcon
                message="成片预览已取消"
                description={
                  <Flex justify="space-between" align="center" wrap gap={8} style={{ marginTop: 4 }}>
                    <Typography.Text type="secondary">
                      {latestPreview.message || '预览任务已取消，未生成最终成片文件。'}
                    </Typography.Text>
                    <Button
                      size="small"
                      onClick={() => handleStart(true)}
                      loading={isStarting}
                      disabled={!canStart || isStarting}
                    >
                      重新生成
                    </Button>
                  </Flex>
                }
              />
            )}

            {latestSucceededPreview && (
              <Card
                size="small"
                className="preview-media-card"
                style={{
                  background: 'rgba(82, 196, 26, 0.04)',
                  borderColor: 'rgba(82, 196, 26, 0.3)'
                }}
              >
                <Flex vertical gap={8}>
                  <Flex justify="space-between" align="center" wrap gap={8}>
                    <Space wrap>
                      <CheckCircleOutlined style={{ color: '#52c41a', fontSize: 16 }} />
                      <Typography.Text strong>成片 MP4 预览已就绪</Typography.Text>
                      <Tag color="success">已完成</Tag>
                      {latestSucceededPreview.invalidated && (
                        <Tag color="warning">已失效（上游有新变动）</Tag>
                      )}
                      {latestSucceededPreview.is_current && (
                        <Tag color="blue">当前导出版本</Tag>
                      )}
                    </Space>
                    <Space wrap>
                      <Button
                        type="primary"
                        icon={<PlayCircleOutlined />}
                        onClick={() => onSelectPreview?.(createPreviewSource(latestSucceededPreview))}
                      >
                        在监视器播放
                      </Button>
                      <Button
                        icon={<DownloadOutlined />}
                        href={previewFileURL(latestSucceededPreview.preview_id, 'mp4', true)}
                        download
                      >
                        下载 MP4
                      </Button>
                    </Space>
                  </Flex>

                  {latestSucceededPreview.media && (
                    <Descriptions
                      size="small"
                      column={{ xs: 1, sm: 2, md: 4 }}
                      bordered
                      style={{ marginTop: 4 }}
                    >
                      <Descriptions.Item label="画面尺寸">
                        {latestSucceededPreview.media.width} × {latestSucceededPreview.media.height}
                      </Descriptions.Item>
                      <Descriptions.Item label="帧率/总帧数">
                        {latestSucceededPreview.media.fps} fps ({latestSucceededPreview.media.frame_count} 帧)
                      </Descriptions.Item>
                      <Descriptions.Item label="成片时长">
                        {formatSeconds(latestSucceededPreview.media.duration_seconds)}
                      </Descriptions.Item>
                      <Descriptions.Item label="编码规格">
                        {latestSucceededPreview.media.video_codec} / {latestSucceededPreview.media.audio_codec}
                      </Descriptions.Item>
                    </Descriptions>
                  )}
                </Flex>
              </Card>
            )}

            {previews.length > 0 && (
              <Collapse
                size="small"
                items={[
                  {
                    key: 'history',
                    label: `历史预览记录 (${previews.length} 条)`,
                    children: (
                      <List
                        size="small"
                        dataSource={previews}
                        renderItem={(p) => {
                          const isPlaying =
                            activePreviewSrc === previewFileURL(p.preview_id, 'mp4');
                          return (
                            <List.Item
                              key={p.preview_id}
                              actions={[
                                ...(p.status === 'succeeded'
                                  ? [
                                      <Button
                                        key="play"
                                        size="small"
                                        type={isPlaying ? 'primary' : 'default'}
                                        icon={<PlayCircleOutlined />}
                                        disabled={p.invalidated || !p.is_current || p.run_id !== runId || p.revision_id !== revisionId}
                                        onClick={() =>
                                          onSelectPreview?.(createPreviewSource(p))
                                        }
                                      >
                                        {isPlaying ? '播放中' : '播放'}
                                      </Button>,
                                      <Typography.Link
                                        key="download"
                                        href={previewFileURL(p.preview_id, 'mp4', true)}
                                        download
                                      >
                                        下载
                                      </Typography.Link>
                                    ]
                                  : []),
                                ...(p.status === 'queued' ||
                                p.status === 'claimed' ||
                                p.status === 'running'
                                  ? [
                                      <Button
                                        key="cancel"
                                        size="small"
                                        danger
                                        onClick={() =>
                                          handleCancel(p.preview_id)
                                        }
                                        disabled={isCanceling}
                                        loading={isCanceling && cancelMutation.variables?.previewId === p.preview_id}
                                      >
                                        取消
                                      </Button>
                                    ]
                                  : [])
                              ]}
                            >
                              <List.Item.Meta
                                title={
                                  <Space wrap>
                                    <Typography.Text code>
                                      {p.preview_id.slice(0, 8)}
                                    </Typography.Text>
                                    <Tag>{p.revision_id}</Tag>
                                    <Tag
                                      color={
                                        p.status === 'succeeded'
                                          ? 'success'
                                          : p.status === 'failed'
                                            ? 'error'
                                            : p.status === 'cancelled'
                                              ? 'default'
                                              : 'processing'
                                      }
                                    >
                                      {p.status}
                                    </Tag>
                                    {p.invalidated && <Tag color="warning">已失效</Tag>}
                                    {p.is_current && <Tag color="blue">当前导出</Tag>}
                                  </Space>
                                }
                                description={
                                  <span style={{ overflowWrap: 'anywhere' }}>
                                    {p.media
                                      ? `${p.media.width}×${p.media.height} · ${p.media.fps}fps · ${formatSeconds(p.media.duration_seconds)}`
                                      : p.message || p.status}
                                  </span>
                                }
                              />
                            </List.Item>
                          );
                        }}
                      />
                    )
                  }
                ]}
              />
            )}
          </>
        )}
      </Flex>
    </Card>
  );
}
