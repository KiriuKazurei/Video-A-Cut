import { useEffect, useRef, useState } from 'react';
import { Alert, Button, Card, Flex, Form, Input, Space, Tag, Typography } from 'antd';
import { desktopCommand, isDesktop } from '../desktop';
import { currentMcpURL, mcpConnectionConfig, type McpConfigFormat } from '../mcpConnection';
import { useDesktopRuntimeStatus } from './DesktopRuntime';
import { ValueSelect } from './ui';

export function McpConnectionPanel() {
  const runtime = useDesktopRuntimeStatus();
  const [format, setFormat] = useState<McpConfigFormat>('codex');
  const url = currentMcpURL(runtime);
  const config = mcpConnectionConfig(runtime, format);
  const signature = JSON.stringify([runtime?.session_id, url, format]);
  const latest = useRef(signature); latest.current = signature;
  const [busy, setBusy] = useState(false);
  const [feedback, setFeedback] = useState<{ signature: string; message: string; error: boolean }>();
  useEffect(() => { setBusy(false); }, [signature]);
  const copy = async (text: string, label: string) => {
    if (!text || !isDesktop()) return;
    const started = signature;
    setBusy(true);
    try {
      await desktopCommand('clipboard_write_text', { text });
      setFeedback({ signature: latest.current, message: latest.current === started ? `${label}已复制` : '后台地址已变化，请重新复制。', error: latest.current !== started });
    } catch (reason) {
      if (latest.current === started) setFeedback({ signature: started, message: `复制失败：${String(reason)}`, error: true });
    } finally { if (latest.current === started) setBusy(false); }
  };
  const notice = feedback?.signature === signature ? feedback : undefined;
  return <Card size="small" className="mcp-connection-panel" title="Agent MCP 连接" extra={<Tag>{url ? '后台已就绪' : '暂无连接地址'}</Tag>}>
    <Flex vertical gap={12}>
      <Typography.Paragraph style={{ marginBottom: 0 }}>外部 Agent 使用 Streamable HTTP 连接，仅访问当前打开工程的资源。无需额外 Agent Token，人工定稿和日志查看继续通过 VAC 窗口操作。</Typography.Paragraph>
      {!isDesktop() && <Alert type="info" showIcon message="请在 VAC 桌面端获取连接地址；浏览器仅用于开发预览。" />}
      {isDesktop() && !url && <Alert type="info" showIcon role="status" message="后台就绪后会显示当前 MCP 地址，请勿使用旧端口。" />}
      <Form.Item label="MCP 地址" style={{ marginBottom: 0 }}><Input aria-label="MCP 地址" readOnly value={url} placeholder="等待桌面后台服务" /></Form.Item>
      <Form.Item label="连接配置格式" style={{ marginBottom: 0 }}><ValueSelect<McpConfigFormat> aria-label="连接配置格式" value={format} onChange={setFormat}
        options={[{ value: 'codex', label: 'Codex TOML' }, { value: 'json', label: 'mcpServers JSON' }]} /></Form.Item>
      <Input.TextArea aria-label="MCP 连接配置" readOnly value={config} autoSize={{ minRows: 3, maxRows: 10 }} />
      <Space wrap><Button disabled={!url || busy} onClick={() => void copy(url, 'MCP 地址')}>复制地址</Button><Button type="primary" disabled={!config || busy} onClick={() => void copy(config, '连接配置')}>复制连接配置</Button></Space>
      <Typography.Text type="secondary">地址随后台启动和重启更新。Codex TOML 可合入客户端的 config.toml；JSON 用于支持 mcpServers 的客户端。请保留已有连接配置。</Typography.Text>
      {notice && <Alert showIcon type={notice.error ? 'error' : 'success'} role={notice.error ? 'alert' : 'status'} message={notice.message} />}
    </Flex>
  </Card>;
}
