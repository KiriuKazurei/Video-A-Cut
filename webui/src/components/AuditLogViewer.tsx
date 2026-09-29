import { useCallback, useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { auditEndpointInUse, fetchAuditLogs } from '../api';
import type { AuditFilters, AuditLog } from '../types';
import {
  AUDIT_ACTOR_KINDS,
  AUDIT_ACTOR_KIND_LABELS,
  AUDIT_DEFAULT_LIMIT,
  AUDIT_PAGE_LIMITS,
  AUDIT_ACTION_ALIASES,
  DEFAULT_ACTION_ALIASES,
  auditActionCounts,
  auditActionLabel,
  auditActorKind,
  auditTargetHint,
  describeError,
  expandActionAliases,
  filterAuditLogs,
  stamp,
  type AuditActionCount
} from './governanceModel';
import './governance.css';

/* ------------------------------------------------------------------ *
 * 审计日志面板
 *
 * 服务端契约（docs/二阶段开发文档.md「已有 HTTP 契约」）：
 *   GET /api/audit?limit=N → AuditLog[]，limit 是唯一参数
 *   - 默认页 200、显式上限 1000（control-plane/internal/api/audit.go）
 *   - 服务端 newest first，不做任何过滤；时间范围与操作类型过滤归 UI
 *   - actor 前缀（"system" / "human:webui" / "agent:<id>"）是人机动作的区分依据
 *
 * 因此这个面板必须诚实地讲一件事：过滤只作用于已加载的这一页，
 * 不是全表筛选。面板标题旁边一直显示「已加载 N 条」。
 *
 * 操作类型过滤用别名词表：如 CREATE_TASK / TASK_REQUEUE / SYSTEM_EVENT，
 * 而服务端写的是 task.create 等真实 action。选项同时显示别名的中文意图和真实
 * action 值，筛的是什么始终可核对。
 * ------------------------------------------------------------------ */

export default function AuditLogViewer() {
  const [limit, setLimit] = useState<number>(AUDIT_DEFAULT_LIMIT);
  const [aliases, setAliases] = useState<string[]>([...DEFAULT_ACTION_ALIASES]);
  const [actorKinds, setActorKinds] = useState<AuditFilters['actorKinds']>([]);
  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');
  const [keyword, setKeyword] = useState('');
  const [expandedId, setExpandedId] = useState<number | null>(null);

  const auditQuery = useQuery({
    queryKey: ['audit-panel', limit],
    queryFn: () => fetchAuditLogs(limit),
    staleTime: 15_000
  });

  const logs: AuditLog[] = useMemo(() => auditQuery.data ?? [], [auditQuery.data]);

  // 别名词表里只有本页真实出现过的 action 才会进过滤条件，
  // 否则界面会显示「0 条命中」而不说原因。
  const activeActions = useMemo(() => expandActionAliases(aliases, logs), [aliases, logs]);

  const filters = useMemo<AuditFilters>(
    () => ({ from, to, actions: activeActions, actorKinds, keyword }),
    [from, to, activeActions, actorKinds, keyword]
  );

  const visible = useMemo(() => filterAuditLogs(logs, filters), [logs, filters]);
  const actionCounts = useMemo(() => auditActionCounts(logs), [logs]);
  const actorCounts = useMemo(() => countActors(logs), [logs]);

  const endpoint = auditEndpointInUse();

  const toggleAlias = useCallback((alias: string) => {
    setAliases((previous) => previous.includes(alias)
      ? previous.filter((item) => item !== alias)
      : [...previous, alias]);
  }, []);

  const toggleActorKind = useCallback((kind: AuditFilters['actorKinds'][number]) => {
    setActorKinds((previous) => previous.includes(kind)
      ? previous.filter((item) => item !== kind)
      : [...previous, kind]);
  }, []);

  const resetFilters = useCallback(() => {
    setAliases([...DEFAULT_ACTION_ALIASES]);
    setActorKinds([]);
    setFrom('');
    setTo('');
    setKeyword('');
  }, []);

  // 过滤变化后，已展开的详情行可能已不在结果里；收起它比留一个悬空详情更诚实。
  useEffect(() => {
    if (expandedId !== null && !visible.some((row) => row.id === expandedId)) setExpandedId(null);
  }, [visible, expandedId]);

  return (
    <section className="panel audit-viewer" aria-labelledby="audit-viewer-title">
      <div className="panel-heading">
        <div>
          <p className="eyebrow">AUDIT STREAM</p>
          <h2 id="audit-viewer-title">审计日志面板</h2>
        </div>
        <span className="count">
          {auditQuery.isPending ? '读取中' : `已加载 ${logs.length} 条 · 命中 ${visible.length} 条`}
        </span>
      </div>

      <p className="hint">
        服务端 <code>GET /api/audit</code> 只支持 <code>limit</code> 分页， newest first，
        不提供时间范围或操作类型过滤（control-plane/internal/api/audit.go）。
        <strong>下面的过滤只作用于本页已加载的 {logs.length} 条，不是全表筛选</strong>；
        要看更早的记录请把单页条数调大。当前读取的端点：
        <code>{endpoint ?? '探测中'}</code>
      </p>

      <div className="audit-filters">
        <div>
          <label className="field-label" htmlFor="audit-limit">单页加载条数</label>
          <select id="audit-limit" value={limit}
            onChange={(event) => setLimit(Number(event.target.value))}>
            {AUDIT_PAGE_LIMITS.map((value) => (
              <option key={value} value={value}>{value}</option>
            ))}
          </select>
        </div>
        <div>
          <label className="field-label" htmlFor="audit-from">起始时间</label>
          <input id="audit-from" type="datetime-local" value={from}
            onChange={(event) => setFrom(event.target.value)} />
        </div>
        <div>
          <label className="field-label" htmlFor="audit-to">结束时间</label>
          <input id="audit-to" type="datetime-local" value={to}
            onChange={(event) => setTo(event.target.value)} />
        </div>
        <div>
          <label className="field-label" htmlFor="audit-keyword">关键字</label>
          <input id="audit-keyword" type="search" value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
            placeholder="actor / action / target / detail / id" />
        </div>
      </div>

      <fieldset className="audit-filter-group">
        <legend className="field-label">操作类型</legend>
        <div className="checkbox-row">
          {Object.entries(AUDIT_ACTION_ALIASES).map(([alias, mapped]) => (
            <label key={alias} className="checkbox-item">
              <input type="checkbox" checked={aliases.includes(alias)}
                onChange={() => toggleAlias(alias)} />
              <span>
                {alias}
                <small> → {mapped.join(' / ')}</small>
              </span>
            </label>
          ))}
        </div>
        <p className="hint">
          复选框显示的是别名的意图，箭头后是服务端实际写出的 action 值。
          当前生效的 action 过滤：
          <code>{activeActions.length > 0 ? activeActions.join('、') : '不限'}</code>
        </p>
      </fieldset>

      <fieldset className="audit-filter-group">
        <legend className="field-label">操作来源（actor 前缀）</legend>
        <div className="checkbox-row">
          {AUDIT_ACTOR_KINDS.map((kind) => (
            <label key={kind} className="checkbox-item">
              <input type="checkbox" checked={actorKinds.includes(kind)}
                onChange={() => toggleActorKind(kind)} />
              <span>
                {AUDIT_ACTOR_KIND_LABELS[kind]}
                <small> {actorCounts[kind] ?? 0} 条</small>
              </span>
            </label>
          ))}
        </div>
      </fieldset>

      <div className="draft-form-actions">
        <button className="secondary-button" type="button" onClick={resetFilters}>重置过滤</button>
        <button className="secondary-button" type="button"
          onClick={() => void auditQuery.refetch()}>重新读取</button>
      </div>

      {auditQuery.isPending && <p className="state">正在读取审计记录…</p>}
      {auditQuery.isError && (
        <div className="state state-error" role="alert">
          审计读取失败：{describeError(auditQuery.error)}
          <button type="button" onClick={() => void auditQuery.refetch()}>重试</button>
        </div>
      )}
      {auditQuery.isSuccess && logs.length === 0 && (
        <p className="state">暂无审计记录。资产入库、治理变更和任务创建都会记录在这里。</p>
      )}
      {auditQuery.isSuccess && logs.length > 0 && visible.length === 0 && (
        <p className="state">
          已加载 {logs.length} 条，但没有命中当前过滤。可尝试重置过滤，或调大单页条数。
        </p>
      )}

      {visible.length > 0 && (
        <>
          {actionCounts.length > 0 && (
            <div className="detail-block">
              <h4>动作分布（当前页）</h4>
              <ul className="action-count-list">
                {actionCounts.map((item) => (
                  <li key={item.value}>
                    <code>{item.value}</code><span>{item.label}</span><strong>{item.count}</strong>
                  </li>
                ))}
              </ul>
            </div>
          )}

          <ol className="audit-list audit-detail-list">
            {visible.map((row) => {
              const kind = auditActorKind(row.actor);
              const isOpen = expandedId === row.id;
              return (
                <li key={row.id} className={isOpen ? 'is-open' : undefined}>
                  <button type="button" className="audit-summary"
                    aria-expanded={isOpen}
                    aria-controls={`audit-detail-${row.id}`}
                    onClick={() => setExpandedId(isOpen ? null : row.id)}>
                    <time dateTime={row.created_at}>{stamp(row.created_at)}</time>
                    <strong>{auditActionLabel(row.action)}</strong>
                    <span className="audit-action-value">{row.action}</span>
                    <span className="audit-actor">{row.actor}</span>
                    <span className="audit-kind">{AUDIT_ACTOR_KIND_LABELS[kind]}</span>
                  </button>
                  <div className="audit-row-target">{row.target}</div>
                  {isOpen && (
                    <div className="audit-detail" id={`audit-detail-${row.id}`}>
                      <dl className="detail-grid">
                        <div><dt>日志 ID</dt><dd>{row.id}</dd></div>
                        <div><dt>操作来源</dt><dd>{row.actor}（{AUDIT_ACTOR_KIND_LABELS[kind]}）</dd></div>
                        <div><dt>操作类型</dt><dd>{auditActionLabel(row.action)}<code>{row.action}</code></dd></div>
                        <div><dt>目标</dt><dd>{row.target}</dd></div>
                        <div><dt>落库时间</dt><dd>{stamp(row.created_at)}<small>{row.created_at}</small></dd></div>
                        <div><dt>说明</dt><dd>{auditTargetHint(row.action)}</dd></div>
                      </dl>
                      <div className="audit-detail-detail">
                        <span className="field-label">detail 原文</span>
                        <pre>{row.detail && row.detail.trim() !== '' ? row.detail : '（服务端未记录 detail）'}</pre>
                      </div>
                    </div>
                  )}
                </li>
              );
            })}
          </ol>
        </>
      )}
    </section>
  );
}

/** 当前页里各 actor 分类的条数，让「操作来源」过滤器每个选项都有可核对的计数。 */
function countActors(logs: readonly AuditLog[]): Record<string, number> {
  const counts: Record<string, number> = {};
  for (const log of logs) {
    const kind = auditActorKind(log.actor);
    counts[kind] = (counts[kind] ?? 0) + 1;
  }
  return counts;
}

export type { AuditActionCount };
