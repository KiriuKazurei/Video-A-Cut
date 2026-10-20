/** Stop players before changing the active project. No identity session exists. */
export function pauseMediaPlayers(): void {
  if (typeof document === 'undefined') return;
  document.querySelectorAll('video,audio').forEach((node) => {
    if (node instanceof HTMLMediaElement) { node.pause(); node.removeAttribute('src'); node.load(); }
  });
}
