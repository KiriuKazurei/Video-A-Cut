import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, App, Button, Card, Empty, Flex, Input, Select, Space, Spin, Tag, Typography } from 'antd';
import { CopyOutlined, DeleteOutlined, DownloadOutlined, UploadOutlined } from '@ant-design/icons';
import { desktopCommand, isDesktop } from '../desktop';
import { shotCardsKey, shotFramesKey, shotcraftApi, useShotCards, type ShotCard } from '../shotcraftApi';
import { errorText } from '../components/ui';

const example = {
  version: 1,
  id: 'user-title',
  summary: '从画面中央淡入的标题',
  category: '开场',
  duration_frames: 90,
  source: 'import {AbsoluteFill, interpolate, useCurrentFrame} from "remotion";\nexport default function Title(){\n  const opacity = interpolate(useCurrentFrame(), [0, 24], [0, 1], {extrapolateRight:"clamp"});\n  return <AbsoluteFill style={{alignItems:"center",justifyContent:"center",background:"#252525",color:"#E6E6E6",fontSize:96,opacity}}>标题示例</AbsoluteFill>;\n}',
};

function downloadJSON(id: string, entry: unknown) {
  const url = URL.createObjectURL(new Blob([JSON.stringify(entry, null, 2)], { type: 'application/json' }));
  const link = document.createElement('a');
  link.href = url;
  link.download = id + '.json';
  link.click();
  window.setTimeout(() => URL.revokeObjectURL(url), 1000);
}

function ShotDetails({ card, active, busy, onDelete, onDownload }: {
  card: ShotCard; active: boolean; busy: boolean; onDelete: () => void; onDownload: () => void;
}) {
  const { message } = App.useApp();
  const [variant, setVariant] = useState(card.styles[0].id);
  const [position, setPosition] = useState(50);
  const selection = variant + ':' + position;
  const [requested, setRequested] = useState('');
  useEffect(() => {
    if (!active) return;
    const timer = window.setTimeout(() => setRequested(selection), 160);
    return () => window.clearTimeout(timer);
  }, [active, selection]);
  const [image, setImage] = useState<{ source: Blob; url: string }>();
  const reference = '{#' + card.id + (card.styles.length > 1 ? '/' + variant : '') + '}';
  const frame = useQuery({
    queryKey: [...shotFramesKey, card.id, variant, position],
    queryFn: ({ signal }) => shotcraftApi.preview(card.id, variant, position, signal),
    enabled: active && requested === selection,
    staleTime: 60_000,
    retry: false,
  });
  useEffect(() => {
    if (!frame.data) return;
    const url = URL.createObjectURL(frame.data);
    setImage({ source: frame.data, url });
    return () => URL.revokeObjectURL(url);
  }, [frame.data]);

  const copy = async () => {
    try {
      if (isDesktop()) await desktopCommand('clipboard_write_text', { text: reference });
      else await navigator.clipboard.writeText(reference);
      void message.success('已复制镜头卡引用');
    } catch (error) { void message.error(errorText(error, '复制失败')); }
  };

  return <Card size="small" title={card.id} extra={<Tag>{card.user ? '用户导入' : '内置'}</Tag>}>
    <Flex vertical gap={12}>
      <Typography.Paragraph>{card.summary}</Typography.Paragraph>
      {card.use && <Typography.Text type="secondary">{card.use}</Typography.Text>}
      <Space wrap><Tag>{card.category}</Tag>{card.duration && <Typography.Text type="secondary">{card.duration}</Typography.Text>}</Space>
      <div className="shotcraft-frame" aria-busy={frame.isFetching}>
        {image && image.source === frame.data ? <img src={image.url} decoding="async" alt={card.id + ' 效果预览'} /> : frame.isFetching ? <Spin />
          : <Typography.Text type="secondary">{frame.isError ? '当前选项没有可显示的预览' : '正在准备预览…'}</Typography.Text>}
      </div>
      {frame.isError && <Alert type="warning" showIcon message={errorText(frame.error)} />}
      <Space wrap>
        {card.styles.length > 1 && <Select aria-label="模板变体" value={variant} onChange={setVariant}
          options={card.styles.map(style => ({ value: style.id, label: style.id }))} />}
        <Select aria-label="预览位置" value={position} onChange={setPosition}
          options={[{ value: 0, label: '开头' }, { value: 25, label: '前段' }, { value: 50, label: '中段' }, { value: 75, label: '后段' }, { value: 100, label: '结尾' }]} />
        {frame.isError && <Button onClick={() => void frame.refetch()}>重试预览</Button>}
      </Space>
      {card.styles.length > 1 && <Typography.Text type="secondary">{card.styles.find(style => style.id === variant)?.description}</Typography.Text>}
      <Space wrap>
        <Typography.Text code>{reference}</Typography.Text>
        <Button icon={<CopyOutlined aria-hidden="true" />} onClick={() => void copy()}>复制脚本引用</Button>
        {card.user && <>
          <Button icon={<DownloadOutlined aria-hidden="true" />} disabled={busy} onClick={onDownload}>导出 JSON</Button>
          <Button danger icon={<DeleteOutlined aria-hidden="true" />} disabled={busy} onClick={onDelete}>移除模板</Button>
        </>}
      </Space>
    </Flex>
  </Card>;
}

export function ShotcraftWorkspace({ active }: { active: boolean }) {
  const { modal, message } = App.useApp();
  const client = useQueryClient();
  const cards = useShotCards(active);
  const file = useRef<HTMLInputElement>(null);
  const [search, setSearch] = useState('');
  const [source, setSource] = useState('all');
  const [selected, setSelected] = useState('');
  const change = useMutation({
    mutationFn: async (action: { importFile: File } | { removeID: string }) => {
      if ('removeID' in action) return shotcraftApi.remove(action.removeID);
      if (action.importFile.size > 1024 * 1024) throw new Error('JSON 文件不得超过 1 MiB');
      const card = await shotcraftApi.import((await action.importFile.text()).replace(/^\uFEFF/, ''));
      setSearch('');
      setSource('user');
      setSelected(card.id);
    },
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: shotCardsKey });
      void message.success('模板库已更新');
    },
  });
  const query = search.trim().toLocaleLowerCase();
  const visible = (cards.data ?? []).filter(card =>
    (source === 'all' || (source === 'user') === Boolean(card.user)) &&
    [card.id, card.summary, card.category, card.use, ...card.styles.flatMap(style => [style.id, style.description])]
      .join(' ').toLocaleLowerCase().includes(query));
  const detail = visible.find(card => card.id === selected) ?? visible[0];

  const remove = (card: ShotCard) => modal.confirm({
    title: '移除 ' + card.id + '？',
    content: '移除后，脚本再次生成时需要重新导入该模板。',
    okText: '移除模板',
    cancelText: '取消',
    okButtonProps: { danger: true },
    onOk: async () => {
      await change.mutateAsync({ removeID: card.id }).catch(() => {});
      client.removeQueries({ queryKey: [...shotFramesKey, card.id] });
    },
  });

  return <section className="shotcraft-library" aria-label="Shotcraft 模板库">
    <Flex align="center" justify="space-between" gap={12} wrap>
      <Typography.Title level={3}>Shotcraft 模板库</Typography.Title>
      <Space wrap>
        <Button loading={cards.isFetching} onClick={() => {
          void client.resetQueries({ queryKey: shotFramesKey });
          void cards.refetch();
        }}>刷新目录</Button>
        <Button icon={<DownloadOutlined aria-hidden="true" />} onClick={() => downloadJSON(example.id, example)}>下载导入示例</Button>
        <Button type="primary" icon={<UploadOutlined aria-hidden="true" />} loading={change.isPending} onClick={() => file.current?.click()}>导入模板</Button>
        <input ref={file} hidden type="file" accept=".json,application/json" onChange={event => {
          const chosen = event.currentTarget.files?.[0];
          event.currentTarget.value = '';
          if (chosen) change.mutate({ importFile: chosen });
        }} />
      </Space>
    </Flex>
    <Typography.Text type="secondary">导入的镜头卡可在本机的所有项目中引用。</Typography.Text>
    <Space wrap>
      <Input.Search aria-label="搜索镜头卡" placeholder="搜索卡号、分类或效果" allowClear value={search} onChange={event => setSearch(event.target.value)} />
      <Select aria-label="模板来源" value={source} onChange={setSource} options={[
        { value: 'all', label: '全部模板' }, { value: 'builtin', label: '内置模板' }, { value: 'user', label: '用户导入' },
      ]} />
    </Space>
    {change.isError && <Alert type="error" showIcon message={errorText(change.error)} closable onClose={change.reset} />}
    {cards.isError && <Alert type="error" showIcon message={errorText(cards.error)} action={<Button onClick={() => void cards.refetch()}>重试</Button>} />}
    {cards.isLoading ? <Spin /> : !cards.isError && !detail
      ? <Empty description={source === 'user' && !query ? '还没有导入模板' : '没有匹配的模板'} />
      : detail && <div className="shotcraft-browser">
        <ul className="shotcraft-list" aria-label="镜头卡">
          {visible.map(card => <li key={card.id}>
            <Button block type={detail.id === card.id ? 'primary' : 'text'} aria-pressed={detail.id === card.id} onClick={() => setSelected(card.id)}>
              <span>{card.id}</span><span>{card.category}</span>
            </Button>
          </li>)}
        </ul>
        <ShotDetails key={detail.id} card={detail} active={active} busy={change.isPending} onDelete={() => remove(detail)}
          onDownload={() => void shotcraftApi.download(detail.id).then(data => downloadJSON(detail.id, data)).catch(error => message.error(errorText(error)))} />
      </div>}
  </section>;
}
