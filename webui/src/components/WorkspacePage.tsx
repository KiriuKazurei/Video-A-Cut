import { useEffect, useLayoutEffect, useRef, type PropsWithChildren } from 'react';
// Workspace pages use string pageId for compatibility

interface WorkspacePageProps {
  pageId: string;
  active: boolean;
}

/**
 * 工作区页面容器：包装单个页面，负责媒体生命周期管理。
 *
 * 当页面变为非活动状态时，暂停该页面内所有 audio/video 元素的播放，
 * 防止隐藏页面的媒体继续播放。页面重新激活时保留播放进度、音量、
 * 文件选择和表单状态，等待用户明确播放操作。
 */
export function WorkspacePage({ pageId, active, children }: PropsWithChildren<WorkspacePageProps>) {
  const rootRef = useRef<HTMLDivElement>(null);

  // 页面变为非活动时在 layout effect 中暂停该节点内全部 audio/video
  useLayoutEffect(() => {
    if (!active && rootRef.current) {
      const media = rootRef.current.querySelectorAll<HTMLMediaElement>('audio, video');
      media.forEach((element) => {
        if (!element.paused) {
          element.pause();
        }
      });
    }
  }, [active]);

  // 原生捕获防护：确保即便通过直接 DOM 调用 play() 也能在非活动状态立即拦截暂停
  useEffect(() => {
    const el = rootRef.current;
    if (!el) return;
    const onPlay = (e: Event) => {
      if (!active) {
        const target = e.target;
        if (target instanceof HTMLMediaElement) {
          target.pause();
        }
      }
    };
    el.addEventListener('play', onPlay, true);
    return () => {
      el.removeEventListener('play', onPlay, true);
    };
  }, [active]);

  // 防护：非活动页面的媒体如果因异步挂载或程序调用收到 play 事件，立即暂停
  const handlePlayCapture = (event: React.SyntheticEvent<HTMLElement>) => {
    if (!active && (event.target instanceof HTMLMediaElement)) {
      event.target.pause();
    }
  };

  return (
    <div
      ref={rootRef}
      className="page"
      data-page-view={pageId}
      hidden={!active}
      onPlayCapture={handlePlayCapture}
    >
      {children}
    </div>
  );
}
