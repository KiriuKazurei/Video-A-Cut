/**
 * 可见素材确认（纯函数）。
 * 入库和 Agent 可见是两层：只有人确认过当前可见集，才能进入脚本或开始新的生成。
 * 已经在跑的流程按确认前的授权继续，不在这里被拦住。
 */

export interface ScriptEntryInput {
  /** 当前可见集指纹已经被人确认。 */
  confirmed: boolean;
  /** 这一条素材本身在 Agent 可见集里。 */
  assetVisible: boolean;
  /** 该素材已有内容流程（进行中的 run 视为已确认）。 */
  hasWorkflow: boolean;
}

/** 未确认且没有进行中的流程时，不能进入脚本。去掉 confirmed 判断，单测会变红。 */
export function enterScriptAllowed(input: ScriptEntryInput): boolean {
  if (input.hasWorkflow) return true;
  return input.confirmed && input.assetVisible;
}

/** 开始一次新的生成（手动或自动）必须先有可见集确认。 */
export function startGenerationAllowed(confirmed: boolean): boolean {
  return confirmed;
}
