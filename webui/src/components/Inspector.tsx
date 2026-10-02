import type { UseMutationResult } from '@tanstack/react-query';
import type { Asset, GovernancePatch } from '../types';
import { ApiError } from '../api';
import type { PageId } from '../navigation';
import { kindLabel } from './MediaBin';
import { Icon } from './Icon';

function message(error: unknown): string {
  if (error instanceof ApiError) return `${error.message}（${error.code}）`;
  return error instanceof Error ? error.message : '发生未知错误';
}

function stamp(value: string | undefined): string {
  if (!value) return '—';
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? value : date.toLocaleString('zh-CN');
}

type Patch = UseMutationResult<Asset, Error, { id: string; values: GovernancePatch }>;

/** 属性面板（Premiere Properties）：当前资产的治理状态与 PATCH 操作（API-03）。 */
export function Inspector({ asset, patch, onNavigate }: { asset?: Asset; patch: Patch; onNavigate: (page: PageId) => void }) {
  const next: { page: PageId; text: string } | null = !asset ? null
    : asset.input_kind === 'raw_recording' ? { page: 'assembly', text: '原始录像：到「粗剪」导入、切分并生成短片' }
    : asset.status === 'exported' ? { page: 'export', text: '已导出：到「导出」预览、下载与验收' }
    : { page: 'prepare', text: 'EDL 包：到「准备」预检并启动内容流程' };
  return (
    <section className="inspector-panel" aria-labelledby="detail-title">
      <div className="dock-heading"><h2 id="detail-title">属性 · 资产治理</h2></div>
      {!asset && <p className="state">在项目面板选择一项资产以查看状态和治理操作。</p>}
      {asset && <>
        <div className="detail-title"><strong>{asset.asset_id}</strong><span className="pill">{asset.status}</span></div>
        <dl className="detail-grid detail-grid-stack">
          <div><dt>输入类型</dt><dd>{kindLabel(asset)}</dd></div>
          <div><dt>Agent 可见</dt><dd>{asset.agent_visible ? '是' : '否'}</dd></div>
          <div><dt>锁定</dt><dd>{asset.locked ? '是' : '否'}</dd></div>
          <div><dt>人工批准</dt><dd>{asset.human_approved ? '是' : '否'}</dd></div>
          <div><dt>允许的角色</dt><dd>{asset.allowed_agents?.join('、') || '未指定'}</dd></div>
          <div><dt>更新时间</dt><dd>{stamp(asset.updated_at)}</dd></div>
          <div><dt>产物记录</dt><dd>{Object.keys(asset.artifacts ?? {}).length} 项</dd></div>
          {asset.ingest_run_id && <div><dt>导入运行</dt><dd className="mono">{asset.ingest_run_id}</dd></div>}
        </dl>
        <div className="inspector-actions">
          <button type="button" disabled={patch.isPending} onClick={() => patch.mutate({ id: asset.asset_id,
            values: { agent_visible: !asset.agent_visible } })}>
            <Icon name="eye" />{asset.agent_visible ? '设为 Agent 不可见' : '设为 Agent 可见'}</button>
          <button className="secondary-button" type="button" disabled={patch.isPending}
            onClick={() => patch.mutate({ id: asset.asset_id, values: { locked: !asset.locked } })}>
            <Icon name="lock" />{asset.locked ? '解锁' : '锁定'}</button>
          <button className="secondary-button" type="button" disabled={patch.isPending}
            onClick={() => patch.mutate({ id: asset.asset_id, values: { human_approved: !asset.human_approved } })}>
            <Icon name="check" />{asset.human_approved ? '撤销批准' : '人工批准'}</button>
        </div>
        {patch.isError && <p className="inline-error" role="alert">更新失败：{message(patch.error)}</p>}
        <p className="hint">只发送修改的治理字段；人工批准资产不等于批准某句解说。修改权限可能影响活动任务。</p>
        {next && <button type="button" className="next-step" onClick={() => onNavigate(next.page)}>
          <span><small>建议下一步</small>{next.text}</span><Icon name="chevron" /></button>}
      </>}
    </section>
  );
}
