import { Alert, Card, Collapse, Flex, List, Tag, Typography } from 'antd';
import {
  buildEvidenceDigest,
  digestItemStatusLabel,
  digestItemTagColor,
  type EvidenceDigest,
  type EvidenceDigestInput
} from '../evidenceSummary';

export type { EvidenceDigest, EvidenceDigestInput };

/** 将输入聚合成摘要；无流程时也给出空态文案。 */
export function useEvidenceDigest(input: EvidenceDigestInput): EvidenceDigest {
  return buildEvidenceDigest(input);
}

/**
 * 交付 / 人工验收共用的白话「检查摘要」：预览、成片包、验收进度 + 下一步。
 * 哈希与版本号仅在「详细信息」折叠内展示。
 */
export function EvidenceSummaryCard({
  digest,
  compact = false,
  extraDetails
}: {
  digest: EvidenceDigest;
  /** 交付页侧栏用更短卡片 */
  compact?: boolean;
  /** 追加到详细信息里的行（如 manifest） */
  extraDetails?: Array<{ label: string; value: string }>;
}) {
  const alertType = digest.counts.failed > 0
    ? 'warning'
    : digest.allPassed
      ? 'success'
      : 'info';

  const details = [...digest.techDetails, ...(extraDetails ?? [])];

  return (
    <Card
      size="small"
      className={`evidence-summary-card${compact ? ' is-compact' : ''}`}
      aria-labelledby="evidence-summary-title"
      title={<span id="evidence-summary-title">检查摘要</span>}
      extra={
        <Flex gap={6} wrap>
          <Tag bordered={false}>{digest.counts.passed}/{digest.counts.total} 已通过</Tag>
          {digest.counts.failed > 0 && <Tag color="error" bordered={false}>{digest.counts.failed} 未通过</Tag>}
          {digest.counts.pending > 0 && <Tag bordered={false}>{digest.counts.pending} 待确认</Tag>}
        </Flex>
      }
    >
      <Flex vertical gap={compact ? 8 : 12}>
        <Alert
          type={alertType}
          showIcon
          className="evidence-summary-next"
          message={digest.headline}
          description={digest.nextAction}
        />
        <List
          size="small"
          className="evidence-summary-list"
          dataSource={digest.items}
          renderItem={(item) => (
            <List.Item key={item.id} className={`evidence-summary-row is-${item.status}`}>
              <Flex gap={8} align="flex-start" style={{ width: '100%' }} wrap>
                <Tag color={digestItemTagColor(item.status)} style={{ marginInlineEnd: 0 }}>
                  {digestItemStatusLabel(item.status)}
                </Tag>
                <div style={{ flex: '1 1 160px', minWidth: 0 }}>
                  <Typography.Text strong>{item.label}</Typography.Text>
                  <div>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {item.detail}
                    </Typography.Text>
                  </div>
                </div>
              </Flex>
            </List.Item>
          )}
        />
        {details.length > 0 && (
          <Collapse
            size="small"
            ghost
            items={[{
              key: 'tech',
              label: '详细信息（版本与指纹，一般可忽略）',
              children: (
                <Flex vertical gap={6}>
                  {details.map((row) => (
                    <Typography.Text key={row.label} type="secondary" style={{ fontSize: 12, overflowWrap: 'anywhere' }}>
                      {row.label}{' '}
                      <Typography.Text code copyable={{ text: row.value }} style={{ fontSize: 12 }}>
                        {row.value.length > 48 ? `${row.value.slice(0, 20)}…${row.value.slice(-12)}` : row.value}
                      </Typography.Text>
                    </Typography.Text>
                  ))}
                </Flex>
              )
            }]}
          />
        )}
      </Flex>
    </Card>
  );
}
