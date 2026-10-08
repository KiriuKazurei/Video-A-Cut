import { Modal, Typography, Table, Tag, Divider, theme } from 'antd';
import { QuestionCircleOutlined } from '@ant-design/icons';

export interface KeyboardShortcutsModalProps {
  open: boolean;
  onClose: () => void;
}

interface ShortcutItem {
  key: string;
  keys: string[];
  action: string;
  category: string;
}

const SHORTCUTS: ShortcutItem[] = [
  { key: '1', keys: ['1'], action: '切换到「资源管理」工作区', category: '工作区导航' },
  { key: '2', keys: ['2'], action: '切换到「编辑」工作区', category: '工作区导航' },
  { key: '3', keys: ['3'], action: '切换到「导出」工作区', category: '工作区导航' },
  { key: '4', keys: ['4'], action: '切换到「设置」工作区', category: '工作区导航' },
  { key: '5', keys: ['5'], action: '切换到「TTS」工作区', category: '工作区导航' },
  { key: '[', keys: ['['], action: '切换到上一个二级分区 / 分类', category: '二级导航' },
  { key: ']', keys: [']'], action: '切换到下一个二级分区 / 分类', category: '二级导航' },
  { key: 'space', keys: ['Space'], action: '播放 / 暂停当前监视器媒体', category: '播放控制' },
  { key: 'k', keys: ['K'], action: '播放 / 暂停媒体', category: '播放控制' },
  { key: 'j', keys: ['J'], action: '快退 2 秒', category: '播放控制' },
  { key: 'l', keys: ['L'], action: '快进 2 秒', category: '播放控制' },
  { key: 'left', keys: ['←'], action: '快退 2 秒', category: '播放控制' },
  { key: 'right', keys: ['→'], action: '快进 2 秒', category: '播放控制' },
  { key: 'help', keys: ['?'], action: '打开 / 关闭快捷键帮助', category: '通用' },
  { key: 'esc', keys: ['Esc'], action: '关闭当前弹窗 / 抽屉', category: '通用' }
];

export function KeyboardShortcutsModal({ open, onClose }: KeyboardShortcutsModalProps) {
  const { token } = theme.useToken();

  const columns = [
    {
      title: '按键',
      dataIndex: 'keys',
      key: 'keys',
      width: 140,
      render: (keys: string[]) => (
        <span style={{ display: 'inline-flex', gap: 4 }}>
          {keys.map((k) => (
            <Tag
              key={k}
              style={{
                fontFamily: 'monospace',
                fontSize: 12,
                fontWeight: 600,
                padding: '2px 8px',
                borderRadius: 4,
                background: token.colorFillSecondary,
                borderColor: token.colorBorderSecondary,
                color: token.colorText // 修复：使用主文本色，确保暗色背景下对比度达标（原 colorTextLightSolid 对比度不足）
              }}
            >
              {k}
            </Tag>
          ))}
        </span>
      )
    },
    {
      title: '功能说明',
      dataIndex: 'action',
      key: 'action',
      render: (text: string) => <Typography.Text>{text}</Typography.Text>
    },
    {
      title: '分类',
      dataIndex: 'category',
      key: 'category',
      width: 110,
      render: (cat: string) => <Tag color="blue">{cat}</Tag>
    }
  ];

  return (
    <Modal
      title={
        <span style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <QuestionCircleOutlined style={{ color: token.colorPrimary }} />
          <span>键盘快捷键速查</span>
        </span>
      }
      open={open}
      onCancel={onClose}
      footer={null}
      width={560}
      destroyOnClose
    >
      <Typography.Paragraph type="secondary" style={{ fontSize: 13, marginTop: 4 }}>
        在非文本输入焦点时生效。按键后直接跳转或控制当前监视器播放。
      </Typography.Paragraph>

      <Table
        dataSource={SHORTCUTS}
        columns={columns}
        pagination={false}
        size="small"
        bordered
        aria-label="键盘快捷键列表"
      />

      <Divider style={{ margin: '16px 0 8px' }} />
      <Typography.Text type="secondary" style={{ fontSize: 12, display: 'block', textAlign: 'center' }}>
        输入框与下拉搜索聚焦期间将自动停用单字符快捷键，防止打字误触。
      </Typography.Text>
    </Modal>
  );
}
