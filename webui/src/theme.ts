import { theme, type ThemeConfig } from 'antd';

/**
 * Ant Design 5 暗色算法：Premiere 式统一底色——布局、容器、页头页脚取同一纯色，
 * 中间工作区不再用色块区分面板；操作控件保持 antd 默认外观（主色、圆角、尺寸不改）。
 * 次要/三级文字略提亮，保证 4.5:1 对比度。
 */
export const UNIFIED_BG = '#1c1d21';

const BASE_TOKEN: ThemeConfig['token'] = {
  colorBgBase: UNIFIED_BG,
  colorBgLayout: UNIFIED_BG,
  colorBgContainer: UNIFIED_BG,
  colorTextSecondary: 'rgba(255, 255, 255, 0.68)',
  colorTextTertiary: 'rgba(255, 255, 255, 0.52)',
  colorTextDescription: 'rgba(255, 255, 255, 0.6)'
};

/**
 * 全局主题。液态玻璃（Spatial UI / visionOS）只作用于顶部页签与监视器/预览窗口（见 styles.css）。
 * 侧栏底色 siderBg / lightSiderBg = colorBgContainer，纯色、不透明。
 */
export const appTheme: ThemeConfig = {
  algorithm: theme.darkAlgorithm,
  token: BASE_TOKEN,
  components: {
    Layout: { headerBg: UNIFIED_BG, bodyBg: UNIFIED_BG, footerBg: UNIFIED_BG, siderBg: UNIFIED_BG, lightSiderBg: UNIFIED_BG, headerHeight: 56, headerPadding: '0 16px', footerPadding: '6px 16px' },
    // 页签胶囊：当前页白字落在蓝色玻璃高亮上（对比度 > 7:1）；去掉水平菜单的下划线指示。
    Menu: { itemBg: 'transparent', horizontalLineHeight: '34px', horizontalItemSelectedColor: '#ffffff', horizontalItemHoverColor: '#ffffff', itemHoverColor: '#ffffff', activeBarHeight: 0, horizontalItemBorderRadius: 999, itemPaddingInline: 14 },
    // 以下组件级覆写只用于中间工作区（卡片/表格融入统一底色）；侧栏用 siderTheme，不继承这些覆写。
    Card: { colorBgContainer: 'transparent', headerBg: 'transparent' },
    Table: { colorBgContainer: 'transparent', headerBg: 'rgba(255, 255, 255, 0.04)', rowHoverBg: 'rgba(255, 255, 255, 0.05)' },
    Descriptions: { labelBg: 'rgba(255, 255, 255, 0.04)' },
    Collapse: { headerBg: 'rgba(255, 255, 255, 0.03)', contentBg: 'transparent' },
    List: { colorBgContainer: 'transparent' }
  }
};

/**
 * 左右侧栏主题：不继承上面的组件级覆写（inherit: false），只用 antd 暗色算法 + 同一组基础色值，
 * 按钮、输入框、列表、卡片、描述表、标签、提示框全部是 antd 5 默认暗色外观。
 */
export const siderTheme: ThemeConfig = {
  inherit: false,
  algorithm: theme.darkAlgorithm,
  token: BASE_TOKEN
};
