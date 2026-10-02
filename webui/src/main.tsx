import '@ant-design/v5-patch-for-react-19';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { App as AntApp, ConfigProvider, theme, type ThemeConfig } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import dayjs from 'dayjs';
import 'dayjs/locale/zh-cn';
import App from './App';
import './styles.css';

dayjs.locale('zh-cn');

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, staleTime: 15_000 } }
});

/**
 * Ant Design 5 暗色算法：Premiere 式统一底色——布局、容器、页头页脚取同一色值，
 * 中间工作区不再用色块区分面板；操作控件保持 antd 默认外观（主色、圆角、尺寸不改）。
 * 次要/三级文字略提亮，保证在玻璃面板上也满足 4.5:1 对比度。
 * 液态玻璃（Spatial UI / visionOS）只作用于展示窗口与侧栏，见 styles.css。
 */
const UNIFIED_BG = '#1c1d21';
const appTheme: ThemeConfig = {
  algorithm: theme.darkAlgorithm,
  token: {
    colorBgBase: UNIFIED_BG,
    colorBgLayout: UNIFIED_BG,
    colorBgContainer: UNIFIED_BG,
    colorTextSecondary: 'rgba(255, 255, 255, 0.68)',
    colorTextTertiary: 'rgba(255, 255, 255, 0.52)',
    colorTextDescription: 'rgba(255, 255, 255, 0.6)'
  },
  components: {
    Layout: { headerBg: 'transparent', bodyBg: 'transparent', footerBg: 'transparent', headerHeight: 56, headerPadding: '0 16px', footerPadding: '6px 16px' },
    Menu: { itemBg: 'transparent', horizontalLineHeight: '54px' },
    Card: { colorBgContainer: 'transparent', headerBg: 'transparent' },
    Table: { colorBgContainer: 'transparent', headerBg: 'rgba(255, 255, 255, 0.04)', rowHoverBg: 'rgba(255, 255, 255, 0.05)' },
    Descriptions: { labelBg: 'rgba(255, 255, 255, 0.04)' },
    Collapse: { headerBg: 'rgba(255, 255, 255, 0.03)', contentBg: 'transparent' },
    List: { colorBgContainer: 'transparent' }
  }
};

// 只关闭按钮两字中文自动插空格，避免“锁定”显示为“锁 定”。
createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ConfigProvider locale={zhCN} theme={appTheme} button={{ autoInsertSpace: false }}>
      <AntApp>
        <QueryClientProvider client={queryClient}>
          <App />
        </QueryClientProvider>
      </AntApp>
    </ConfigProvider>
  </StrictMode>
);
