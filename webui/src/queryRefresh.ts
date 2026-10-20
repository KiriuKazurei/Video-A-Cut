import type { QueryClient } from '@tanstack/react-query';

/**
 * 集中定义的刷新领域（查询键前缀）。
 * 覆盖：assets、deliveryFiles、audit、asset-task-index、processing-profiles、
 * prepared、workflows、workflow、review、acceptance、ingest。
 */
export const REFRESH_DOMAINS = [
 'project-workspace', 'resource-text',
 'vu-projects','vu-project','vu-visible','vu-skills','vu-runs','vu-run','vu-context',
  'assets',
  'deliveryFiles',
  'audit',
  'asset-task-index',
  'processing-profiles',
  'prepared',
  'workflows',
  'workflow',
  'review',
  'acceptance',
  'ingest',
  'mp4Previews',
  'deliveryVariants',
  'tts-templates', 'tts-previews', 'tts-preview', 'tts-capabilities',
  'auto', 'auto-notice', 'visible-set', 'scripts', 'script-workbench', 'script-media', 'script-revisions', 'script-delivery'
] as const;

export type RefreshDomain = (typeof REFRESH_DOMAINS)[number];

export const REFRESH_SCOPES: readonly (readonly [RefreshDomain])[] = REFRESH_DOMAINS.map(
  (domain) => [domain] as const
);

/**
 * 刷新完整快照：
 * 统一手动刷新（App 顶部“刷新快照”按钮）与 SSE 首次连接/重连成功时的查询失效范围。
 * 范围包括：assets、deliveryFiles、audit、asset-task-index、processing-profiles、
 * prepared、workflows、workflow、review、acceptance、ingest，以及当前 watchedTaskId 的任务查询。
 * 有观察者的查询立即重新请求，未使用的缓存标为过期（stale），
 * 并且保留已有数据直至新快照返回。
 */
export function refreshSnapshot(
  queryClient: QueryClient,
  watchedTaskId?: string | null
): Promise<void[]> {
  const promises: Promise<void>[] = REFRESH_SCOPES.map((scope) =>
    queryClient.invalidateQueries({ queryKey: scope })
  );
  if (watchedTaskId) {
    promises.push(queryClient.invalidateQueries({ queryKey: ['task', watchedTaskId] }));
  }
  return Promise.all(promises);
}

/**
 * 交付文件查询失效：
 * 若提供具体 assetId，则仅失效该资产的交付文件 ['deliveryFiles', assetId]；
 * 若无法安全定位资产（为空或未提供），则使整个 ['deliveryFiles'] 前缀失效。
 */
export function invalidateDeliveryFiles(
  queryClient: QueryClient,
  assetId?: string | null
): Promise<void> {
  const trimmed = typeof assetId === 'string' ? assetId.trim() : '';
  if (trimmed) {
    return queryClient.invalidateQueries({ queryKey: ['deliveryFiles', trimmed] });
  }
  return queryClient.invalidateQueries({ queryKey: ['deliveryFiles'] });
}

/**
 * 从事件 JSON 中提取定位信息。
 * 契约规范：事件 JSON 只读取 asset_id、task_id、status 等失效定位信息，
 * 不把事件对象写入查询缓存，以 REST 返回的文件清单为准。
 */
export interface EventPayloadMeta {
  asset_id?: string;
  task_id?: string;
  status?: string;
  run_id?: string;
  revision_id?: string;
}

export function extractEventMeta(data?: unknown): EventPayloadMeta {
  if (!data) return {};
  if (typeof data === 'string') {
    try {
      const parsed = JSON.parse(data);
      if (parsed && typeof parsed === 'object') {
        return extractFromObject(parsed as Record<string, unknown>);
      }
    } catch {
      return {};
    }
  } else if (typeof data === 'object') {
    return extractFromObject(data as Record<string, unknown>);
  }
  return {};
}

function extractFromObject(obj: Record<string, unknown>): EventPayloadMeta {
  const meta: EventPayloadMeta = {};
  if (typeof obj.asset_id === 'string' && obj.asset_id.trim()) {
    meta.asset_id = obj.asset_id.trim();
  }
  if (typeof obj.task_id === 'string' && obj.task_id.trim()) {
    meta.task_id = obj.task_id.trim();
  }
  if (typeof obj.status === 'string' && obj.status.trim()) {
    meta.status = obj.status.trim();
  }
  if (typeof obj.run_id === 'string' && obj.run_id.trim()) {
    meta.run_id = obj.run_id.trim();
  }
  if (typeof obj.revision_id === 'string' && obj.revision_id.trim()) {
    meta.revision_id = obj.revision_id.trim();
  }
  return meta;
}

/** 支持的所有 SSE 事件名单 */
export const SUPPORTED_SSE_EVENTS = [
 'workspace.changed','workspace.saved','resource.changed',
 'project.changed','skill.changed','skill.run.changed',
  'asset_created',
  'asset_updated',
  'task_created',
  'task_updated',
  'workflow.changed',
  'review.changed',
  'acceptance.changed',
  'profile.changed',
  'capability.changed',
  'ingest.changed',
  'audit.changed',
  'preview_updated',
  'delivery_variant.changed',
  'tts.template.changed', 'tts.preview.changed',
  'auto.changed', 'auto.reviewed', 'script.changed', 'script.rendered'
] as const;

export type SupportedSSEEvent = (typeof SUPPORTED_SSE_EVENTS)[number];

/**
 * 统一的事件监听处理入口：
 * 保持原有查询失效行为，增加交付文件领域，并避免 capability.changed 分散监听。
 * 文件失效规则：
 * - asset_created / asset_updated、workflow.changed、ingest.changed 刷新对应资产文件；
 * - task_updated 在 status=succeeded 时刷新对应资产文件；
 * - 无法定位资产时使整个 deliveryFiles 前缀失效。
 */
export function handleSSEEvent(
  queryClient: QueryClient,
  eventName: string,
  data?: unknown,
  watchedTaskId?: string | null
): Promise<void[]> {
  const meta = extractEventMeta(data);
  const promises: Promise<void>[] = [];
  if (['workspace.changed','workspace.saved','resource.changed'].includes(eventName)) {
    return Promise.all(['project-workspace','assets','visible-set','vu-project','vu-visible','resource-text'].map(key => queryClient.invalidateQueries({queryKey:[key]})));
  }
  if (['project.changed','skill.changed','skill.run.changed'].includes(eventName)) {
    return Promise.all(['vu-projects','vu-project','vu-visible','vu-skills','vu-runs','vu-run','vu-context','audit']
      .map(key => queryClient.invalidateQueries({ queryKey: [key] })));
  }
  if (['script.changed', 'script.rendered', 'asset_created', 'asset_updated'].includes(eventName)) {
    promises.push(...['scripts', 'script-workbench', 'script-media', 'script-revisions', 'script-delivery', 'visible-set', 'auto'].map(k => queryClient.invalidateQueries({ queryKey: [k] })));
    if (eventName === 'script.changed' || eventName === 'script.rendered') { promises.push(queryClient.invalidateQueries({ queryKey: ['audit'] }), queryClient.invalidateQueries({ queryKey: ['resource-text'] })); return Promise.all(promises); }
  }
  // 自动模式：只让 auto 视图与对应流程失效，事件负载不当作权威状态。
  if (eventName === 'auto.changed' || eventName === 'auto.reviewed') {
    return Promise.all(['auto', 'workflows', 'workflow', 'review', 'mp4Previews'].map(k => queryClient.invalidateQueries({ queryKey: [k] }))) as Promise<void[]>;
  }
  if (eventName === 'tts.template.changed') return Promise.all([queryClient.invalidateQueries({queryKey:['tts-templates']}),queryClient.invalidateQueries({queryKey:['audit']})]);
  if (eventName === 'tts.preview.changed') return Promise.all([queryClient.invalidateQueries({queryKey:['tts-previews']}),queryClient.invalidateQueries({queryKey:['tts-preview']}),queryClient.invalidateQueries({queryKey:['audit']})]);
  if (['delivery_variant.changed', 'capability.changed', 'workflow.changed', 'asset_created', 'asset_updated', 'task_updated', 'task_created'].includes(eventName)) {
    promises.push(queryClient.invalidateQueries({ queryKey: ['deliveryVariants'] }));
  }

  switch (eventName) {
    case 'asset_created':
    case 'asset_updated':
      promises.push(
        invalidateDeliveryFiles(queryClient, meta.asset_id),
        queryClient.invalidateQueries({ queryKey: ['assets'] }),
        queryClient.invalidateQueries({ queryKey: ['audit'] })
      );
      break;

    case 'task_created':
      if (watchedTaskId && (!meta.task_id || meta.task_id === watchedTaskId)) {
        promises.push(queryClient.invalidateQueries({ queryKey: ['task', watchedTaskId] }));
      }
      promises.push(
        queryClient.invalidateQueries({ queryKey: ['audit'] }),
        queryClient.invalidateQueries({ queryKey: ['ingest', 'run'] }),
        queryClient.invalidateQueries({ queryKey: ['asset-task-index'] })
      );
      break;

    case 'task_updated':
      if (meta.status === 'succeeded') {
        promises.push(invalidateDeliveryFiles(queryClient, meta.asset_id));
      }
      if (watchedTaskId && (!meta.task_id || meta.task_id === watchedTaskId)) {
        promises.push(queryClient.invalidateQueries({ queryKey: ['task', watchedTaskId] }));
      }
      promises.push(
        queryClient.invalidateQueries({ queryKey: ['audit'] }),
        queryClient.invalidateQueries({ queryKey: ['ingest', 'run'] }),
        queryClient.invalidateQueries({ queryKey: ['asset-task-index'] })
      );
      break;

    case 'workflow.changed':
      promises.push(
        invalidateDeliveryFiles(queryClient, meta.asset_id),
        queryClient.invalidateQueries({ queryKey: ['workflows'] }),
        queryClient.invalidateQueries({ queryKey: ['processing-profiles'] }),
        queryClient.invalidateQueries({ queryKey: ['prepared'] }),
        queryClient.invalidateQueries({ queryKey: ['workflow'] }),
        queryClient.invalidateQueries({ queryKey: ['review'] }),
        queryClient.invalidateQueries({ queryKey: ['acceptance'] }),
        queryClient.invalidateQueries({ queryKey: ['audit'] }),
        queryClient.invalidateQueries({ queryKey: ['mp4Previews'] })
      );
      break;

    case 'ingest.changed':
      promises.push(
        invalidateDeliveryFiles(queryClient, meta.asset_id),
        queryClient.invalidateQueries({ queryKey: ['ingest'] }),
        queryClient.invalidateQueries({ queryKey: ['assets'] }),
        queryClient.invalidateQueries({ queryKey: ['audit'] })
      );
      break;

    case 'capability.changed':
      promises.push(
        queryClient.invalidateQueries({ queryKey: ['ingest'] }),
        queryClient.invalidateQueries({ queryKey: ['assets'] }),
        queryClient.invalidateQueries({ queryKey: ['workflows'] }),
        queryClient.invalidateQueries({ queryKey: ['processing-profiles'] }),
        queryClient.invalidateQueries({ queryKey: ['prepared'] }),
        queryClient.invalidateQueries({ queryKey: ['workflow'] }),
        queryClient.invalidateQueries({ queryKey: ['review'] }),
        queryClient.invalidateQueries({ queryKey: ['acceptance'] })
      );
      break;

    case 'review.changed':
    case 'acceptance.changed':
    case 'profile.changed':
      promises.push(
        queryClient.invalidateQueries({ queryKey: ['workflows'] }),
        queryClient.invalidateQueries({ queryKey: ['processing-profiles'] }),
        queryClient.invalidateQueries({ queryKey: ['prepared'] }),
        queryClient.invalidateQueries({ queryKey: ['workflow'] }),
        queryClient.invalidateQueries({ queryKey: ['review'] }),
        queryClient.invalidateQueries({ queryKey: ['acceptance'] }),
        queryClient.invalidateQueries({ queryKey: ['audit'] })
      );
      break;

    case 'audit.changed':
      promises.push(queryClient.invalidateQueries({ queryKey: ['audit'] }));
      break;

    case 'preview_updated':
      if (meta.asset_id && meta.run_id && meta.revision_id) {
        promises.push(
          queryClient.invalidateQueries({
            queryKey: ['mp4Previews', meta.asset_id, meta.run_id, meta.revision_id]
          })
        );
      } else if (meta.asset_id && meta.run_id) {
        promises.push(
          queryClient.invalidateQueries({
            queryKey: ['mp4Previews', meta.asset_id, meta.run_id]
          })
        );
      } else if (meta.asset_id) {
        promises.push(
          queryClient.invalidateQueries({
            queryKey: ['mp4Previews', meta.asset_id]
          })
        );
      } else {
        promises.push(queryClient.invalidateQueries({ queryKey: ['mp4Previews'] }));
      }
      if (watchedTaskId && (!meta.task_id || meta.task_id === watchedTaskId)) {
        promises.push(queryClient.invalidateQueries({ queryKey: ['task', watchedTaskId] }));
      }
      promises.push(queryClient.invalidateQueries({ queryKey: ['audit'] }));
      break;

    default:
      break;
  }

  return Promise.all(promises);
}
