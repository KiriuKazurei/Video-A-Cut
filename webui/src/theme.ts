import { theme, type ThemeConfig } from 'antd';

/**
 * 中性灰深色试验。配色只集中在这里和 styles.css 的 --vac-*。
 * 回退这一版时还原本文件与 :root 里的 --vac-bg / --vac-panel / --vac-control /
 * --vac-border / --vac-text / --vac-text-2 / --vac-accent 即可。
 *
 * 窗口 #1E1E1E，面板 #252525，控件 #2F2F2F，边框 #3A3A3A，
 * 主文字 #E6E6E6，次文字 #9A9A9A，强调色只有 #4C8DFF。
 * 无渐变、无发光、无阴影。控件仍走 Ant Design 5，圆角 2–4px。
 */
export const WINDOW_BG = '#1E1E1E';
export const PANEL_BG = '#252525';
export const CONTROL_BG = '#2F2F2F';
export const BORDER = '#3A3A3A';
export const TEXT = '#E6E6E6';
export const TEXT_SECONDARY = '#9A9A9A';
export const ACCENT = '#4C8DFF';

/** 窗口底色。旧名保留，供仍引用 UNIFIED_BG 的地方使用。 */
export const UNIFIED_BG = WINDOW_BG;

const BASE_TOKEN: ThemeConfig['token'] = {
  colorPrimary: ACCENT,
  colorInfo: ACCENT,
  colorLink: ACCENT,
  colorBgBase: WINDOW_BG,
  colorBgLayout: WINDOW_BG,
  colorBgContainer: PANEL_BG,
  colorBgElevated: CONTROL_BG,
  colorBorder: BORDER,
  colorBorderSecondary: BORDER,
  colorSplit: BORDER,
  colorText: TEXT,
  colorTextBase: TEXT,
  colorTextSecondary: TEXT_SECONDARY,
  colorTextTertiary: TEXT_SECONDARY,
  colorTextDescription: TEXT_SECONDARY,
  colorTextQuaternary: TEXT_SECONDARY,
  boxShadow: 'none',
  boxShadowSecondary: 'none',
  boxShadowTertiary: 'none',
  // 专业剪辑工具：小圆角（2–4px）、更紧凑的控件与字号。
  borderRadius: 3,
  borderRadiusLG: 4,
  borderRadiusSM: 2,
  borderRadiusXS: 2,
  fontSize: 13,
  fontSizeSM: 12,
  controlHeight: 28,
  controlHeightSM: 24,
  controlHeightLG: 34,
  motionDurationFast: '0.15s',
  motionDurationMid: '0.2s',
  motionDurationSlow: '0.25s'
};

/**
 * 全局主题。顶栏分段滑块和监视器外框都是实色，不用液态玻璃（见 styles.css）。
 * 侧栏底色是面板色，纯色、不透明。
 */
export const appTheme: ThemeConfig = {
  algorithm: theme.darkAlgorithm,
  token: BASE_TOKEN,
  components: {
    Layout: {
      headerBg: WINDOW_BG, bodyBg: WINDOW_BG, footerBg: WINDOW_BG,
      siderBg: PANEL_BG, lightSiderBg: PANEL_BG,
      headerHeight: 48, headerPadding: '0 16px', footerPadding: '8px 16px'
    },
    Menu: {
      itemBg: 'transparent', horizontalLineHeight: '28px',
      horizontalItemSelectedColor: TEXT, horizontalItemHoverColor: TEXT,
      itemHoverColor: TEXT, itemSelectedColor: TEXT, itemColor: TEXT_SECONDARY,
      activeBarHeight: 0, horizontalItemBorderRadius: 3, itemBorderRadius: 3, itemPaddingInline: 12
    },
    Card: {
      colorBgContainer: PANEL_BG, headerBg: 'transparent', headerFontSize: 14,
      headerHeight: 46, headerHeightSM: 42,
      headerPadding: 20, headerPaddingSM: 18,
      bodyPadding: 20, bodyPaddingSM: 16
    },
    Table: {
      colorBgContainer: 'transparent', headerBg: CONTROL_BG, rowHoverBg: CONTROL_BG,
      borderColor: BORDER,
      cellPaddingBlockSM: 12, cellPaddingInlineSM: 14
    },
    Descriptions: { labelBg: CONTROL_BG },
    Collapse: { headerBg: CONTROL_BG, contentBg: 'transparent' },
    List: {
      colorBgContainer: 'transparent',
      itemPadding: '18px 2px',
      itemPaddingSM: '14px 4px',
      itemPaddingLG: '20px 8px'
    },
    Button: { defaultBg: CONTROL_BG, defaultBorderColor: BORDER, defaultColor: TEXT, primaryShadow: 'none', defaultShadow: 'none' },
    Input: { colorBgContainer: CONTROL_BG, activeBorderColor: ACCENT, hoverBorderColor: ACCENT },
    InputNumber: { colorBgContainer: CONTROL_BG, activeBorderColor: ACCENT, hoverBorderColor: ACCENT },
    Select: { selectorBg: CONTROL_BG, optionSelectedBg: CONTROL_BG },
    Segmented: { itemSelectedBg: CONTROL_BG, itemSelectedColor: TEXT, itemColor: TEXT_SECONDARY, trackBg: PANEL_BG }
  }
};

/**
 * 左右侧栏主题：不继承上面的组件级覆写（inherit: false），只用 antd 暗色算法 + 同一组色值。
 * 按钮、输入框、列表、卡片、描述表、标签、提示框全部是 antd 5 默认暗色外观。
 */
export const siderTheme: ThemeConfig = {
  inherit: false,
  algorithm: theme.darkAlgorithm,
  token: BASE_TOKEN
};
