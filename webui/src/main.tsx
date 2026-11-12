import '@ant-design/v5-patch-for-react-19';
import { lazy, StrictMode, Suspense, useEffect } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { App as AntApp, ConfigProvider } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import dayjs from 'dayjs';
import 'dayjs/locale/zh-cn';
import { ProjectHome } from './components/ProjectHome';
import { DesktopRuntime } from './components/DesktopRuntime';
import { appTheme } from './theme';
import { ProjectWorkspaceProvider, useProjectWorkspace } from './projectWorkspace';
import './styles.css';
import './project.css';

dayjs.locale('zh-cn');

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, staleTime: 15_000 } }
});

const loadApp = () => import('./App');
const App = lazy(loadApp);

function ProjectApp() {
  const { ready, session, pendingPath, openingPath } = useProjectWorkspace();
  useEffect(() => {
    if (ready || pendingPath) void loadApp().catch(() => {});
  }, [ready, pendingPath]);

  const home = <ProjectHome onOpenTemplates={() => { window.location.hash = '/templates'; }} />;
  if (!ready || (window.__TAURI__ && (!session || (!session.document && (pendingPath || openingPath))))) return home;
  return <Suspense fallback={home}><App key={session?.id ?? 'startup'} /></Suspense>;
}

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
