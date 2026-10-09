export interface RuntimeStatus {
  revision: number;
  status: 'starting' | 'ready' | 'fault' | 'stopping' | 'stopped';
  api_base_url?: string;
  session_id?: string;
  data_directory: string;
  message: string;
  workers: { role: string; status: string }[];
}

interface TauriBridge {
  core: { invoke<T>(command: string, args?: Record<string, unknown>): Promise<T> };
  event: { listen<T>(name: string, callback: (event: { payload: T }) => void): Promise<() => void> };
}
declare global { interface Window { __TAURI__?: TauriBridge } }
export const isDesktop = () => Boolean(window.__TAURI__);
export function desktopCommand<T>(command: string, args?: Record<string, unknown>): Promise<T> {
  if (!window.__TAURI__) return Promise.reject(new Error('此功能需要桌面端'));
  return window.__TAURI__.core.invoke<T>(command, args);
}
export async function runtimeListener(callback: (value: RuntimeStatus) => void): Promise<() => void> {
  return window.__TAURI__!.event.listen<RuntimeStatus>('runtime-status', event => callback(event.payload));
}

export interface UpdateStatus {
  revision: number;
  phase: 'idle' | 'checking' | 'up_to_date' | 'available' | 'downloading' | 'downloaded' | 'awaiting_confirmation' | 'stopping' | 'installing' | 'error';
  current_version: string;
  available_version: string | null;
  notes: string | null;
  downloaded_bytes: number;
  total_bytes: number | null;
  ready_to_install: boolean;
  message: string;
}
export async function updateListener(callback: (value: UpdateStatus) => void): Promise<() => void> {
  return window.__TAURI__!.event.listen<UpdateStatus>('update-status', event => callback(event.payload));
}
