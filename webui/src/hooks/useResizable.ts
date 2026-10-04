import { useCallback, useEffect, useRef, useState } from 'react';

interface UseResizableOptions {
  /** 初始大小 (px) */
  initial: number;
  /** 最小尺寸 (px) */
  min: number;
  /** 最大尺寸 (px) */
  max: number;
  /** 方向：'horizontal' (左右拖拽调整宽度) | 'vertical' (上下拖拽调整高度) */
  direction: 'horizontal' | 'vertical';
  /** 反向拖拽 (例如右侧栏从左往右拉是变小，需要 reverse) */
  reverse?: boolean;
  /** 本地缓存存储 key */
  storageKey?: string;
  /** 双击重置默认值 */
  defaultSize?: number;
}

export function useResizable({
  initial,
  min,
  max,
  direction,
  reverse = false,
  storageKey,
  defaultSize = initial
}: UseResizableOptions) {
  const [size, setSize] = useState<number>(() => {
    if (storageKey && typeof window !== 'undefined') {
      const saved = window.localStorage.getItem(storageKey);
      if (saved) {
        const parsed = Number.parseInt(saved, 10);
        if (!Number.isNaN(parsed) && parsed >= min && parsed <= max) {
          return parsed;
        }
      }
    }
    return initial;
  });

  const [isDragging, setIsDragging] = useState(false);
  const dragStartRef = useRef<{ startPos: number; startSize: number } | null>(null);
  const lastSavedRef = useRef<number | null>(null); // 跟踪上次保存的值，避免重复写入

  const startDrag = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setIsDragging(true);
    const startPos = direction === 'horizontal' ? e.clientX : e.clientY;
    dragStartRef.current = { startPos, startSize: size };
  }, [direction, size]);

  const resetToDefault = useCallback(() => {
    setSize(defaultSize);
    if (storageKey) {
      window.localStorage.setItem(storageKey, String(defaultSize));
      lastSavedRef.current = defaultSize;
    }
  }, [defaultSize, storageKey]);

  useEffect(() => {
    if (!isDragging) return;

    const onMouseMove = (e: MouseEvent) => {
      if (!dragStartRef.current) return;
      const currentPos = direction === 'horizontal' ? e.clientX : e.clientY;
      const delta = currentPos - dragStartRef.current.startPos;
      const factor = reverse ? -1 : 1;
      const nextSize = Math.max(min, Math.min(max, dragStartRef.current.startSize + delta * factor));
      setSize(nextSize);
    };

    const onMouseUp = () => {
      setIsDragging(false);
      dragStartRef.current = null;
    };

    document.addEventListener('mousemove', onMouseMove);
    document.addEventListener('mouseup', onMouseUp);
    document.body.style.userSelect = 'none';
    document.body.style.cursor = direction === 'horizontal' ? 'col-resize' : 'row-resize';

    return () => {
      document.removeEventListener('mousemove', onMouseMove);
      document.removeEventListener('mouseup', onMouseUp);
      document.body.style.userSelect = '';
      document.body.style.cursor = '';
    };
  }, [isDragging, direction, reverse, min, max]);

  // 大小确定后持久化（仅在值实际变化时写入，避免重复）
  useEffect(() => {
    if (!isDragging && storageKey && typeof window !== 'undefined') {
      const roundedSize = Math.round(size);
      if (lastSavedRef.current !== roundedSize) {
        window.localStorage.setItem(storageKey, String(roundedSize));
        lastSavedRef.current = roundedSize;
      }
    }
  }, [size, isDragging, storageKey]);

  return {
    size: Math.round(size),
    setSize,
    isDragging,
    startDrag,
    resetToDefault
  };
}
