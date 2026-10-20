import { useEffect, useMemo } from 'react';
import { Button, Tour, type TourProps } from 'antd';

const find = (selector: string) => () => {
  const el = [...document.querySelectorAll<HTMLElement>(selector)].find((node) => node.getClientRects().length > 0);
  return (el ?? null) as HTMLElement;
};

/**
 * 新手引导：antd Tour。用大白话讲清「导入 → 准备 → 生成 → 审查 → 交付」以及每块区域是做什么的。
 * 目标元素不在当前页面（如窄屏没有侧栏）时，该步改为居中说明，不会指向空位。
 */
export function AppTour({ open, onClose }: { open: boolean; onClose: () => void }) {
  const steps = useMemo<NonNullable<TourProps['steps']>>(() => {
    const present = (selector: string) => open && Boolean(find(selector)());
    const side = present('.dock-left') ? '.dock-left' : present('.app-header-right') ? '.app-header-right' : '';
    // 「自动模式」切换只在编辑页的开始卡里；不在当前页（或没有该切换）时，这一步整个省略，不指向空位
    const autoToggle = present('.start-mode');
    return [
      {
        title: '欢迎使用 Video Auto Cut',
        description: '把一段录像剪成带解说和字幕的短视频，只需要五步：导入 → 准备 → 生成 → 审查 → 交付。接下来用 1 分钟带你认识界面，随时可以跳过。',
        target: null
      },
      {
        title: '顶部：工作区',
        description: '「资源管理」放素材；「编辑」里导入、剪辑和审查；「导出」下载成片并验收；「TTS」试听配音；「设置」放高级选项。按键盘 1~5 也能直接切换。',
        target: find('.app-nav-wrap')
      },
      {
        title: '流程阶段：现在做到哪一步',
        description: '这里显示五个阶段各自的进度：绿色是完成，黄色是进行中，带蓝点的是推荐的下一步。点任意阶段会直接跳到它所在的页面。',
        target: find('[data-tour="stepper"]')
      },
      {
        title: '下一步：不知道做什么就点它',
        description: '页面右上方的蓝色按钮会根据当前素材和流程的真实状态，告诉你下一步该做什么；如果暂时不能点，鼠标放上去会写明还缺什么。',
        target: find('.next-step-btn')
      },
      autoToggle
        ? {
            title: '一句话自动模式',
            description: '不想一步步确认？在这里把「分步生成」切到「自动模式」，写一句要求并选择预设与预算，Agent 会自己完成剪辑、解说、配音和导出，不满意时才会来问你。服务商调用统一在设置中管理。',
            target: find('.start-mode')
          }
        : undefined,
      {
        title: '左右两侧：选素材、看详情',
        description: '左边是当前工程导入的资源；中间提供预览、播放操作与备注；右边查看元信息。导入的资源直接供 Agent 使用。',
        target: side ? find(side) : null
      },
      {
        title: '随时查阅：快捷键与新手引导',
        description: '按 ? 键或从“关于”菜单查看快捷键与新手引导。系统连接、日志等技术状态都收在「设置 › 状态监控」，平时不用关心。',
        target: find('.project-menu-bar')
      }
    ].filter((step): step is NonNullable<typeof step> => Boolean(step));
  }, [open]);

  // antd Tour 自身不处理 Esc：补上，保证键盘用户能随时退出
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  return (
    <Tour
      open={open}
      // 目标贴着窗口边缘（顶部页签、侧栏）时，默认 6px 外扩会让遮罩的四块挡板出现负尺寸并在控制台报 <rect> 错误，所以外扩设为 0
      gap={{ offset: 0, radius: 2 }}
      steps={steps}
      onClose={onClose}
      onFinish={onClose}
      scrollIntoViewOptions={{ block: 'nearest' }}
      zIndex={1100}
      actionsRender={(originNode, { current, total }) => (
        <>
          {current < total - 1 ? <Button type="text" size="small" onClick={onClose}>跳过引导</Button> : null}
          {originNode}
        </>
      )}
    />
  );
}
