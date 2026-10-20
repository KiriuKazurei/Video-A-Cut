import '@ant-design/v5-patch-for-react-19';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { App as AntApp, ConfigProvider } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import dayjs from 'dayjs';
import 'dayjs/locale/zh-cn';
import App from './App';
import { DesktopRuntime } from './components/DesktopRuntime';
import { appTheme } from './theme';
import { ProjectWorkspaceProvider, useProjectWorkspace } from './projectWorkspace';
import './styles.css';
import './project.css';

dayjs.locale('zh-cn');

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, staleTime: 15_000 } }
});

function ProjectApp() { const { session } = useProjectWorkspace(); return <App key={session?.id ?? 'startup'} />; }

// 只关闭按钮两字中文自动插空格，避免“锁定”显示为“锁 定”。
createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ConfigProvider locale={zhCN} theme={appTheme} button={{ autoInsertSpace: false }}>
      <AntApp>
        <QueryClientProvider client={queryClient}>
          <DesktopRuntime><ProjectWorkspaceProvider><ProjectApp /></ProjectWorkspaceProvider></DesktopRuntime>
        </QueryClientProvider>
      </AntApp>
    </ConfigProvider>
  </StrictMode>
);
