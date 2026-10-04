import { useEffect, useCallback } from 'react';
import type { RouteState, WorkspaceId, EditingSection, ExportSection, SettingsCategory } from '../navigation';
import { EDITING_SECTIONS, EXPORT_SECTIONS, SETTINGS_CATEGORIES } from '../navigation';

interface UseKeyboardShortcutsOptions {
  route: RouteState;
  navigate: (state: Partial<RouteState>) => void;
  onOpenHelp: () => void;
  isHelpOpen?: boolean;
}

/** 判断当前焦点是否在文本输入、下拉选框或可编辑容器内 */
export function isTypingContext(target: EventTarget | null): boolean {
  if (!target || !(target instanceof HTMLElement)) return false;
  const tag = target.tagName.toLowerCase();
  if (tag === 'input' || tag === 'textarea' || tag === 'select') return true;
  if (target.isContentEditable) return true;
  if (
    target.closest('input') ||
    target.closest('textarea') ||
    target.closest('.ant-select') ||
    target.closest('.ant-input') ||
    target.closest('[contenteditable="true"]')
  ) {
    return true;
  }
  return false;
}

/** 触发媒体播放或暂停 */
export function toggleMediaPlayback(): boolean {
  const mediaElements = Array.from(document.querySelectorAll<HTMLMediaElement>('.stage-main video, .stage-main audio'));
  const activeMedia = mediaElements.find((el) => {
    // 优先寻找正在播放或可见的媒体元素
    const rect = el.getBoundingClientRect();
    return rect.width > 0 && rect.height > 0;
  });

  if (!activeMedia) return false;

  if (activeMedia.paused) {
    void activeMedia.play().catch(() => {});
  } else {
    activeMedia.pause();
  }
  return true;
}

/** 步进/快进媒体时间 */
export function seekMedia(deltaSeconds: number): boolean {
  const mediaElements = Array.from(document.querySelectorAll<HTMLMediaElement>('.stage-main video, .stage-main audio'));
  const activeMedia = mediaElements.find((el) => {
    const rect = el.getBoundingClientRect();
    return rect.width > 0 && rect.height > 0;
  });

  if (!activeMedia) return false;

  const targetTime = Math.max(0, Math.min(activeMedia.duration || 0, activeMedia.currentTime + deltaSeconds));
  activeMedia.currentTime = targetTime;
  return true;
}

export function useKeyboardShortcuts({
  route,
  navigate,
  onOpenHelp,
  isHelpOpen
}: UseKeyboardShortcutsOptions) {
  const { workspace } = route;

  const cycleSection = useCallback((forward: boolean) => {
    if (workspace === 'editing') {
      const current = route.editingSection ?? 'import';
      const idx = EDITING_SECTIONS.findIndex((s) => s.id === current);
      const nextIdx = forward
        ? (idx + 1) % EDITING_SECTIONS.length
        : (idx - 1 + EDITING_SECTIONS.length) % EDITING_SECTIONS.length;
      navigate({ editingSection: EDITING_SECTIONS[nextIdx].id });
    } else if (workspace === 'export') {
      const current = route.exportSection ?? 'delivery';
      const idx = EXPORT_SECTIONS.findIndex((s) => s.id === current);
      const nextIdx = forward
        ? (idx + 1) % EXPORT_SECTIONS.length
        : (idx - 1 + EXPORT_SECTIONS.length) % EXPORT_SECTIONS.length;
      navigate({ exportSection: EXPORT_SECTIONS[nextIdx].id });
    } else if (workspace === 'settings') {
      const current = route.settingsCategory ?? 'profiles';
      const idx = SETTINGS_CATEGORIES.findIndex((s) => s.id === current);
      const nextIdx = forward
        ? (idx + 1) % SETTINGS_CATEGORIES.length
        : (idx - 1 + SETTINGS_CATEGORIES.length) % SETTINGS_CATEGORIES.length;
      navigate({ settingsCategory: SETTINGS_CATEGORIES[nextIdx].id });
    }
  }, [workspace, route.editingSection, route.exportSection, route.settingsCategory, navigate]);

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // 帮助弹框打开时按 Escape 关闭
      if (isHelpOpen && e.key === 'Escape') {
        return;
      }

      // 如果有修饰键（Ctrl, Alt, Meta），除特定组合外默认不拦截
      if (e.ctrlKey || e.metaKey || e.altKey) {
        return;
      }

      // 输入框聚焦状态防护
      if (isTypingContext(e.target)) {
        return;
      }

      // 帮助面板开关：? 或 Shift + /
      if (e.key === '?' || (e.shiftKey && e.key === '/')) {
        e.preventDefault();
        onOpenHelp();
        return;
      }

      // 1 ~ 4 切换主工作区
      if (e.key === '1') {
        e.preventDefault();
        navigate({ workspace: 'resources' });
        return;
      }
      if (e.key === '2') {
        e.preventDefault();
        navigate({ workspace: 'editing' });
        return;
      }
      if (e.key === '3') {
        e.preventDefault();
        navigate({ workspace: 'export' });
        return;
      }
      if (e.key === '4') {
        e.preventDefault();
        navigate({ workspace: 'settings' });
        return;
      }

      // [ 与 ] 切换二级分区
      if (e.key === '[') {
        e.preventDefault();
        cycleSection(false);
        return;
      }
      if (e.key === ']') {
        e.preventDefault();
        cycleSection(true);
        return;
      }

      // 空格键：播放/暂停
      if (e.code === 'Space') {
        const handled = toggleMediaPlayback();
        if (handled) {
          e.preventDefault();
        }
        return;
      }

      // J / K / L 简易快进退控制
      if (e.key === 'k' || e.key === 'K') {
        const handled = toggleMediaPlayback();
        if (handled) e.preventDefault();
        return;
      }
      if (e.key === 'j' || e.key === 'J') {
        const handled = seekMedia(-2);
        if (handled) e.preventDefault();
        return;
      }
      if (e.key === 'l' || e.key === 'L') {
        const handled = seekMedia(2);
        if (handled) e.preventDefault();
        return;
      }

      // 方向键快退/快进 (无修饰键且在媒体上下文)
      if (e.key === 'ArrowLeft') {
        const handled = seekMedia(-2);
        if (handled) e.preventDefault();
        return;
      }
      if (e.key === 'ArrowRight') {
        const handled = seekMedia(2);
        if (handled) e.preventDefault();
        return;
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [navigate, cycleSection, onOpenHelp, isHelpOpen]);
}
