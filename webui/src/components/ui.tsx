import type { ReactNode } from 'react';
import { Select, type SelectProps } from 'antd';
import { ApiError } from '../api';

/** 统一的错误文案：服务端错误码附在括号里，便于对照接口文档。 */
export function errorText(error: unknown, fallback = '发生未知错误'): string {
  if (error instanceof ApiError) return `${error.message}（${error.code}）`;
  return error instanceof Error ? error.message : fallback;
}

export function stamp(value: string | undefined): string {
  if (!value) return '—';
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? value : date.toLocaleString('zh-CN');
}

export interface ValueOption<V extends string | number> { value: V; label: ReactNode; disabled?: boolean }

/**
 * antd Select 的薄封装：选项节点带 data-value，方便浏览器回归脚本按值选择；
 * 列表较短，关闭虚拟滚动以便所有选项都渲染出来。
 */
export function ValueSelect<V extends string | number>({ options, ...props }: Omit<SelectProps<V>, 'options'> & { options: ValueOption<V>[] }) {
  return <Select<V> virtual={false} {...props} options={options}
    optionRender={(option) => <span data-value={String(option.value)}>{option.label}</span>} />;
}
