import { useEffect, useCallback } from 'react';
import type { RouteState, WorkspaceId, EditingSection, ExportSection, SettingsCategory } from '../navigation';
import { EDITING_SECTIONS, EXPORT_SECTIONS, SETTINGS_CATEGORIES } from '../navigation';

/** 审查快捷键：由 WorkflowPanel（仅当前可见的那个）监听并执行，键盘层只负责守卫与分发。 */
export const REVIEW_SHORTCUT_EVENT = 'vac:review-shortcut';
export type ReviewShortcutAction = 'confirm' | 'approve' | 'next';
const REVIEW_KEYS: Record<string, ReviewShortcutAction> = { c: 'confirm', a: 'approve', n: 'next' };

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

/** 
 * P2-05: 判断当前焦点是否在按钮、滑块、链接、标签页等可交互控件上
 * 用于保护原生控件的键盘行为（如 Space 激活按钮、左右箭头调节滑块数值等）
 */
export function isInteractiveControl(target: EventTarget | null): boolean {
  if (!target || !(target instanceof HTMLElement)) return false;
  const tag = target.tagName.toLowerCase();
  if (tag === 'button' || tag === 'a') return true;
  if (
    target.closest('button') ||
    target.closest('a') ||
    target.closest('.ant-btn') ||
    target.closest('.ant-slider') ||
    target.closest('.ant-tabs-tab') ||
    target.closest('[role="slider"]') ||
    target.closest('[role="button"]') ||
    target.closest('[role="tab"]') ||
    target.closest('[role="combobox"]')
  ) {
    return true;
  }
  return false;
}

/** 字母键不会触发按钮/链接的原生行为，所以只在滑块、页签、下拉等会自己吃键盘的控件上让路 */
function lettersBlocked(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  // 焦点在按钮/链接上时也不触发（例如停在「退回场景」上按 C，不能变成确认场景）
  return Boolean(target.closest('button, a[href], .ant-btn, .ant-slider, [role="slider"], [role="tab"], [role="button"], [role="combobox"], [role="listbox"], [role="menu"]'));
}

/** 
 * 查找当前可见的媒体元素（优先主工作区，其次 Modal/Drawer，最后全局）
 * 支持多种容器，避免硬编码选择器
 */
function findVisibleMedia(): HTMLMediaElement | null {
  const selectors = [
    '.stage-main video',
    '.stage-main audio',
    '.ant-modal-body video',
    '.ant-modal-body audio',
    '.ant-drawer-body video',
    '.ant-drawer-body audio',
    'video',
    'audio'
  ];

  for (const selector of selectors) {
    const elements = document.querySelectorAll<HTMLMediaElement>(selector);
    for (const el of elements) {
      const rect = el.getBoundingClientRect();
      if (rect.width > 0 && rect.height > 0 && !el.hidden) {
        return el;
      }
    }
  }
  return null;
}

/** 触发媒体播放或暂停 */
export function toggleMediaPlayback(): boolean {
  const activeMedia = findVisibleMedia();
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
  const activeMedia = findVisibleMedia();
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
      // P2-06: 帮助弹窗或覆盖层打开时，完全屏蔽背景工作区切换与媒体控制快捷键
      if (isHelpOpen || document.querySelector('.ant-modal-root, .ant-drawer-open, .ant-tour')) {
        return;
      }

      // 如果事件已经被其他原生或组件逻辑阻止，则不重复拦截
      if (e.defaultPrevented) {
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
      if (e.key === '5' && !isInteractiveControl(e.target)) {
        e.preventDefault(); navigate({ workspace: 'tts' }); return;
      }
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

      // 审查快捷键 C / A / N：仅在「编辑 › 内容编辑/审查」生效；焦点在滑块、页签、下拉上时不触发
      if (workspace === 'editing' && (route.editingSection === 'content' || route.editingSection === 'review') && !e.shiftKey) {
        const action = REVIEW_KEYS[e.key.toLowerCase()];
        if (action && !lettersBlocked(e.target)) {
          e.preventDefault();
          window.dispatchEvent(new CustomEvent(REVIEW_SHORTCUT_EVENT, { detail: { action } }));
          return;
        }
      }

      // P2-05: 空格键与媒体快捷键保护交互控件原生行为（按钮空格激活、滑块左右箭头等）
      if (e.code === 'Space') {
        if (isInteractiveControl(e.target)) {
          // 焦点在按钮、滑块、标签页等控件上时，保留原生空格激活行为，不触发媒体播放
          return;
        }
        const handled = toggleMediaPlayback();
        if (handled) {
          e.preventDefault();
        }
        return;
      }

      // J / K / L 简易快进退控制
      if (e.key === 'k' || e.key === 'K') {
        if (!isInteractiveControl(e.target)) {
          const handled = toggleMediaPlayback();
          if (handled) e.preventDefault();
        }
        return;
      }
      if (e.key === 'j' || e.key === 'J') {
        if (!isInteractiveControl(e.target)) {
          const handled = seekMedia(-2);
          if (handled) e.preventDefault();
        }
        return;
      }
      if (e.key === 'l' || e.key === 'L') {
        if (!isInteractiveControl(e.target)) {
          const handled = seekMedia(2);
          if (handled) e.preventDefault();
        }
        return;
      }

      // 方向键快退/快进 (P2-05: 仅在非交互控件如滑块/按钮时触发)
      if (e.key === 'ArrowLeft') {
        if (isInteractiveControl(e.target)) {
          return;
        }
        const handled = seekMedia(-2);
        if (handled) e.preventDefault();
        return;
      }
      if (e.key === 'ArrowRight') {
        if (isInteractiveControl(e.target)) {
          return;
        }
        const handled = seekMedia(2);
        if (handled) e.preventDefault();
        return;
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [navigate, cycleSection, onOpenHelp, isHelpOpen, workspace, route.editingSection]);
}
