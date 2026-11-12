import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, App, Button, Card, Flex, Input, Space, Typography } from 'antd';
import { desktopCommand, isDesktop } from '../desktop';
import { shotCardsKey, shotFramesKey, shotcraftApi } from '../shotcraftApi';
import { errorText } from './ui';

const directoryKey = ['shotcraft-directory'] as const;

export function ShotcraftDirectorySettings() {
  const client = useQueryClient();
  const { message } = App.useApp();
  const [draft, setDraft] = useState<string | null>(null);
  const [picking, setPicking] = useState(false);
  const [pickerError, setPickerError] = useState('');
  const settings = useQuery({ queryKey: directoryKey, queryFn: ({ signal }) => shotcraftApi.directory(signal) });
  const current = settings.data?.directory ?? '';
  const value = draft ?? current;
  const save = useMutation({
    mutationFn: shotcraftApi.setDirectory,
    onSuccess: async result => {
      client.setQueryData(directoryKey, result);
      setDraft(null);
      await Promise.all([
        client.invalidateQueries({ queryKey: shotCardsKey }),
        client.resetQueries({ queryKey: shotFramesKey }),
      ]);
      void message.success('模板库目录已保存');
    },
  });

  const choose = async () => {
    setPicking(true);
    setPickerError('');
    try {
      const [directory] = await desktopCommand<string[]>('project_dialog', {
        mode: 'shotcraft-directory',
        defaultPath: value || null,
      });
      if (directory) setDraft(directory);
    } catch (error) {
      setPickerError(errorText(error));
    } finally {
      setPicking(false);
    }
  };

  return <Card size="small">
    <Flex vertical gap={12}>
      <Typography.Text type="secondary">
        切换目录后，模板库会读取所选目录中的模板，新导入也会保存到该目录。原库中的模板保留，切回即可使用。
      </Typography.Text>
      <Typography.Text>当前库：{current || (settings.data ? '应用默认库' : '读取中')}</Typography.Text>
      <label htmlFor="shotcraft-directory">模板库目录</label>
      <Input id="shotcraft-directory" value={value} placeholder="留空使用应用默认库"
        disabled={!settings.data || save.isPending || picking} onChange={event => setDraft(event.target.value)} />
      <Space wrap>
        {isDesktop() && <Button loading={picking} disabled={!settings.data || save.isPending} onClick={() => void choose()}>选择目录</Button>}
        <Button type="primary" loading={save.isPending} disabled={!settings.data || value === current || picking}
          onClick={() => save.mutate(value)}>保存目录</Button>
        <Button disabled={!current || save.isPending || picking} onClick={() => save.mutate('')}>恢复默认库</Button>
      </Space>
      {settings.isError && <Alert type="error" showIcon message={errorText(settings.error)} action={<Button onClick={() => void settings.refetch()}>重试</Button>} />}
      {(pickerError || save.isError) && <Alert type="error" showIcon message={pickerError || errorText(save.error)} />}
    </Flex>
  </Card>;
}
