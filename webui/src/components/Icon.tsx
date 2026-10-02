/** 线性 SVG 图标（24 网格、currentColor 描边）。仅作装饰：文字标签始终同时存在。 */
const PATHS: Record<string, string> = {
  import: 'M12 3v12m0 0-4-4m4 4 4-4M4 17v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-2',
  assembly: 'M6 4a2 2 0 1 0 0 4 2 2 0 0 0 0-4Zm0 12a2 2 0 1 0 0 4 2 2 0 0 0 0-4ZM7.7 7.2 20 18M7.7 16.8 20 6',
  prepare: 'M4 6h10M18 6h2M4 12h4M12 12h8M4 18h12M20 18h0M16 4v4M10 10v4M18 16v4',
  edit: 'M3 5h18v14H3zM3 10h18M8 10v9M14 10v9',
  export: 'M12 21V9m0 0 4 4m-4-4-4 4M4 7V5a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v2',
  monitor: 'M3 12h4l3-8 4 16 3-8h4',
  film: 'M4 4h16v16H4zM8 4v16M16 4v16M4 8h4M4 12h4M4 16h4M16 8h4M16 12h4M16 16h4',
  refresh: 'M20 11a8 8 0 0 0-14.9-4M4 4v4h4M4 13a8 8 0 0 0 14.9 4M20 20v-4h-4',
  search: 'M11 4a7 7 0 1 0 0 14 7 7 0 0 0 0-14Zm9 16-4-4',
  audio: 'M4 10v4M8 7v10M12 4v16M16 8v8M20 11v2',
  image: 'M4 5h16v14H4zM4 16l5-5 4 4 3-3 4 4M15 9h.01',
  lock: 'M6 11h12v9H6zM8 11V8a4 4 0 0 1 8 0v3',
  eye: 'M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12Zm10-3a3 3 0 1 0 0 6 3 3 0 0 0 0-6Z',
  check: 'M5 12l4 4 10-10',
  chevron: 'M9 6l6 6-6 6'
};

export type IconName = keyof typeof PATHS;

export function Icon({ name, size = 16, className }: { name: IconName; size?: number; className?: string }) {
  return <svg className={className ? `icon ${className}` : 'icon'} width={size} height={size} viewBox="0 0 24 24"
    fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round"
    aria-hidden="true" focusable="false"><path d={PATHS[name]} /></svg>;
}
