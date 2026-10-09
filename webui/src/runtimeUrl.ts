let apiBase = '';

export function setApiBase(value: string): void {
  if (value) {
    const url = new URL(value);
    if (url.protocol !== 'http:' || url.hostname !== '127.0.0.1' || !url.port || url.pathname !== '/' || url.search || url.hash || url.username || url.password) {
      throw new Error('桌面服务地址无效');
    }
    apiBase = url.origin;
  } else apiBase = '';
}

/** Includes SSE, media and downloads. Browser development retains relative URLs. */
export function apiURL(path: string): string {
  if (!path.startsWith('/api/')) throw new Error('不支持的业务路径');
  return `${apiBase}${path}`;
}
