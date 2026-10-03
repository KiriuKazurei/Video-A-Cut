import { useCallback, useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Alert, Button, Card, Checkbox, Col, DatePicker, Descriptions, Empty, Flex, Form, Input, Row, Space, Table, Tag, Typography } from 'antd';
import dayjs from 'dayjs';
import { ValueSelect } from './ui';
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

  const toDate = (value: string) => value ? dayjs(value) : null;
  const fromDate = (value: dayjs.Dayjs | null) => value ? value.format('YYYY-MM-DDTHH:mm') : '';

  return (
    <Card size="small" className="audit-viewer" aria-labelledby="audit-viewer-title" title={<span id="audit-viewer-title">审计日志面板</span>}
      extra={<Tag>{auditQuery.isPending ? '读取中' : `已加载 ${logs.length} 条 · 命中 ${visible.length} 条`}</Tag>}>
      <Flex vertical gap={12}>
        <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
          服务端 <Typography.Text code>GET /api/audit</Typography.Text> 只支持 <Typography.Text code>limit</Typography.Text> 分页，newest first，
          不提供时间范围或操作类型过滤（control-plane/internal/api/audit.go）。
          <Typography.Text strong>下面的过滤只作用于本页已加载的 {logs.length} 条，不是全表筛选</Typography.Text>；
          要看更早的记录请把单页条数调大。当前读取的端点：<Typography.Text code>{endpoint ?? '探测中'}</Typography.Text>
        </Typography.Paragraph>

        <Form layout="vertical" className="audit-filters">
          <Row gutter={12}>
            <Col xs={12} md={4}><Form.Item label="单页加载条数" htmlFor="audit-limit" style={{ marginBottom: 8 }}>
              <ValueSelect<number> id="audit-limit" value={limit} onChange={setLimit} options={AUDIT_PAGE_LIMITS.map((value) => ({ value, label: String(value) }))} />
            </Form.Item></Col>
            <Col xs={12} md={6}><Form.Item label="起始时间" htmlFor="audit-from" style={{ marginBottom: 8 }}>
              <DatePicker id="audit-from" showTime={{ format: 'HH:mm' }} format="YYYY-MM-DD HH:mm" style={{ width: '100%' }} value={toDate(from)} onChange={(value) => setFrom(fromDate(value))} />
            </Form.Item></Col>
            <Col xs={12} md={6}><Form.Item label="结束时间" htmlFor="audit-to" style={{ marginBottom: 8 }}>
              <DatePicker id="audit-to" showTime={{ format: 'HH:mm' }} format="YYYY-MM-DD HH:mm" style={{ width: '100%' }} value={toDate(to)} onChange={(value) => setTo(fromDate(value))} />
            </Form.Item></Col>
            <Col xs={12} md={8}><Form.Item label="关键字" htmlFor="audit-keyword" style={{ marginBottom: 8 }}>
              <Input id="audit-keyword" type="search" allowClear value={keyword} onChange={(event) => setKeyword(event.target.value)} placeholder="actor / action / target / detail / id" />
            </Form.Item></Col>
          </Row>
          <Form.Item label="操作类型" style={{ marginBottom: 8 }} extra={<>复选框显示的是别名的意图，箭头后是服务端实际写出的 action 值。当前生效的 action 过滤：<Typography.Text code>{activeActions.length > 0 ? activeActions.join('、') : '不限'}</Typography.Text></>}>
            <Checkbox.Group value={aliases} onChange={(values) => setAliases(values as string[])}
              options={Object.entries(AUDIT_ACTION_ALIASES).map(([alias, mapped]) => ({ value: alias, label: <>{alias}<Typography.Text type="secondary"> → {mapped.join(' / ')}</Typography.Text></> }))} />
          </Form.Item>
          <Form.Item label="操作来源（actor 前缀）" style={{ marginBottom: 8 }}>
            <Checkbox.Group value={actorKinds} onChange={(values) => setActorKinds(values as AuditFilters['actorKinds'])}
              options={AUDIT_ACTOR_KINDS.map((kind) => ({ value: kind, label: <>{AUDIT_ACTOR_KIND_LABELS[kind]}<Typography.Text type="secondary"> {actorCounts[kind] ?? 0} 条</Typography.Text></> }))} />
          </Form.Item>
          <Space wrap>
            <Button onClick={resetFilters}>重置过滤</Button>
            <Button onClick={() => void auditQuery.refetch()}>重新读取</Button>
          </Space>
        </Form>

        {auditQuery.isPending && <Typography.Text type="secondary">正在读取审计记录…</Typography.Text>}
        {auditQuery.isError && <Alert type="error" showIcon role="alert" message={`审计读取失败：${describeError(auditQuery.error)}`}
          action={<Button size="small" onClick={() => void auditQuery.refetch()}>重试</Button>} />}
        {auditQuery.isSuccess && logs.length === 0 && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无审计记录。资产入库、治理变更和任务创建都会记录在这里。" />}
        {auditQuery.isSuccess && logs.length > 0 && visible.length === 0 && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={`已加载 ${logs.length} 条，但没有命中当前过滤。可尝试重置过滤，或调大单页条数。`} />}

        {visible.length > 0 && <>
          {actionCounts.length > 0 && <div>
            <Typography.Text type="secondary">动作分布（当前页）</Typography.Text>
            <Flex wrap gap={6} className="action-count-list" style={{ marginTop: 6 }}>
              {actionCounts.map((item) => <Tag key={item.value} title={item.value}>{item.label} <Typography.Text strong>{item.count}</Typography.Text></Tag>)}
            </Flex>
          </div>}
          <Table<AuditLog> size="small" className="audit-detail-list" rowKey="id" dataSource={visible} scroll={{ x: 760 }}
            pagination={{ pageSize: 20, showSizeChanger: false, hideOnSinglePage: true }}
            expandable={{
              expandedRowKeys: expandedId === null ? [] : [expandedId],
              onExpand: (open, row) => setExpandedId(open ? row.id : null),
              expandedRowRender: (row) => {
                const kind = auditActorKind(row.actor);
                return <div className="audit-detail" id={`audit-detail-${row.id}`}>
                  <Descriptions size="small" column={{ xs: 1, md: 2 }} items={[
                    { key: 'id', label: '日志 ID', children: row.id },
                    { key: 'actor', label: '操作来源', children: `${row.actor}（${AUDIT_ACTOR_KIND_LABELS[kind]}）` },
                    { key: 'action', label: '操作类型', children: <>{auditActionLabel(row.action)} <Typography.Text code>{row.action}</Typography.Text></> },
                    { key: 'target', label: '目标', children: <span style={{ overflowWrap: 'anywhere' }}>{row.target}</span> },
                    { key: 'time', label: '落库时间', children: <>{stamp(row.created_at)} <Typography.Text type="secondary">{row.created_at}</Typography.Text></> },
                    { key: 'hint', label: '说明', children: auditTargetHint(row.action) }
                  ]} />
                  <Typography.Text type="secondary">detail 原文</Typography.Text>
                  <pre className="code-block">{row.detail && row.detail.trim() !== '' ? row.detail : '（服务端未记录 detail）'}</pre>
                </div>;
              }
            }}
            columns={[
              { key: 'time', title: '时间', width: 170, render: (_, row) => <time dateTime={row.created_at}>{stamp(row.created_at)}</time> },
              { key: 'action', title: '操作', render: (_, row) => <><Typography.Text strong>{auditActionLabel(row.action)}</Typography.Text><br /><Typography.Text type="secondary" code>{row.action}</Typography.Text></> },
              { key: 'actor', title: '来源', render: (_, row) => <><span>{row.actor}</span><br /><Tag bordered={false}>{AUDIT_ACTOR_KIND_LABELS[auditActorKind(row.actor)]}</Tag></> },
              { key: 'target', title: '目标', render: (_, row) => <span style={{ overflowWrap: 'anywhere' }}>{row.target}</span> }
            ]} />
        </>}
      </Flex>
    </Card>
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
