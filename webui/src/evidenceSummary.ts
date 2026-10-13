/**
 * R1 / S4e：把预览、成片包、人工验收聚合成普通用户可读的「检查摘要」。
 * 不引入新后端；哈希/修订号只进 techDetails，默认视图不展示。
 */


/** 把预览前置检查里的英文/技术句换成白话；未知句则给通用提示。 */
export function plainPreviewBlocker(raw?: string): string | undefined {
  if (!raw || !raw.trim()) return undefined;
  const t = raw.trim();
  const lower = t.toLowerCase();
  if (/no active worker|worker capable|preset mp4/i.test(t)) {
    return '暂时不能生成预览：渲染服务未就绪，请稍后再试或到「状态监控」查看';
  }
  if (/governance|agent_visible|locked|not visible/i.test(lower)) {
    return '素材治理限制导致无法生成预览，请在资源页确认可见且未锁定';
  }
  if (/export_revision|output_revision|no revision/i.test(lower)) {
    return '还没有导出版本，请先完成内容生成与导出';
  }
  if (/workflow/i.test(lower) && /status|ready/i.test(lower)) {
    return '当前流程状态还不支持生成预览';
  }
  // 已是中文则原样；夹杂英文技术词时收束为通用句
  if (/[\u4e00-\u9fff]/.test(t) && !/sha256|preset|worker|revision_id/i.test(t)) return t;
  return '暂时不能生成预览，请稍后再试';
}

export type DigestItemStatus = 'ready' | 'missing' | 'busy' | 'blocked' | 'na';

export interface DigestItem {
  id: 'preview' | 'package' | 'acceptance';
  label: string;
  status: DigestItemStatus;
  /** 一行白话说明 */
  detail: string;
}

export interface EvidenceDigestInput {
  workflowStatus?: string;
  workflowStage?: string;
  revisionId?: string;
  runId?: string;
  /** 当前修订下的人工验收结果（passed / failed / 其它） */
  checkResults: ReadonlyArray<{ item: string; label: string; result?: string }>;
  preview?: {
    hasPlayable: boolean;
    inProgress: boolean;
    canStart?: boolean;
    blockedHint?: string;
    previewId?: string;
    inputSha256?: string;
    mp4Sha256?: string;
  };
  delivery?: {
    fileCount: number;
    hasPlayable: boolean;
  };
  /** 流程已开放 ZIP（ready_for_acceptance） */
  packageDownloadReady?: boolean;
}

export interface EvidenceDigest {
  headline: string;
  nextAction: string;
  items: DigestItem[];
  counts: { passed: number; failed: number; pending: number; total: number };
  allPassed: boolean;
  techDetails: Array<{ label: string; value: string }>;
}

const STATUS_WEIGHT: Record<DigestItemStatus, number> = {
  blocked: 0, missing: 1, busy: 2, na: 3, ready: 4
};

export function buildEvidenceDigest(input: EvidenceDigestInput): EvidenceDigest {
  const total = input.checkResults.length;
  const passed = input.checkResults.filter(c => c.result === 'passed').length;
  const failed = input.checkResults.filter(c => c.result === 'failed').length;
  const pending = total - passed - failed;
  const readyForAccept = input.workflowStatus === 'ready_for_acceptance';
  const exportedish = readyForAccept || input.workflowStatus === 'succeeded';

  const previewItem = (): DigestItem => {
    const p = input.preview;
    if (!input.runId) {
      return { id: 'preview', label: '预览成片', status: 'na', detail: '还没有流程，暂无可预览内容' };
    }
    if (p?.inProgress) {
      return { id: 'preview', label: '预览成片', status: 'busy', detail: '正在生成可播放预览，稍候即可在「交付」里观看' };
    }
    if (p?.hasPlayable) {
      return { id: 'preview', label: '预览成片', status: 'ready', detail: '已有可播放预览，可在「交付」页观看或下载' };
    }
    if (p?.blockedHint) {
      return { id: 'preview', label: '预览成片', status: 'blocked', detail: plainPreviewBlocker(p.blockedHint) ?? p.blockedHint };
    }
    if (exportedish && p?.canStart !== false) {
      return { id: 'preview', label: '预览成片', status: 'missing', detail: '尚未生成预览：到「交付」页点「生成预览」即可' };
    }
    if (!exportedish) {
      return { id: 'preview', label: '预览成片', status: 'na', detail: '等内容生成并开放验收后，即可制作预览' };
    }
    return { id: 'preview', label: '预览成片', status: 'missing', detail: '暂无预览，请到「交付」页查看原因或重试' };
  };

  const packageItem = (): DigestItem => {
    const files = input.delivery?.fileCount ?? 0;
    const playable = Boolean(input.delivery?.hasPlayable);
    if (input.packageDownloadReady) {
      if (files > 0) {
        return {
          id: 'package',
          label: '成片包',
          status: 'ready',
          detail: playable
            ? `已开放下载，登记了 ${files} 个文件（含可预览媒体）`
            : `已开放下载，登记了 ${files} 个文件`
        };
      }
      return { id: 'package', label: '成片包', status: 'ready', detail: '已开放成片包下载（流程 ZIP）' };
    }
    if (files > 0) {
      return {
        id: 'package',
        label: '成片包',
        status: playable ? 'ready' : 'missing',
        detail: playable
          ? `已有 ${files} 个登记文件，可预览或下载单文件`
          : `已有 ${files} 个登记文件；整包 ZIP 将在开放验收后提供`
      };
    }
    if (!input.runId) {
      return { id: 'package', label: '成片包', status: 'na', detail: '选择素材并完成生成后会出现成片文件' };
    }
    return { id: 'package', label: '成片包', status: 'missing', detail: '还没有成片文件：请先完成内容生成，或在导入页登记已有成片包' };
  };

  const acceptanceItem = (): DigestItem => {
    if (!readyForAccept && input.workflowStatus !== 'succeeded') {
      return {
        id: 'acceptance',
        label: '人工验收',
        status: 'na',
        detail: '流程尚未开放验收，完成生成后即可勾选通过'
      };
    }
    if (failed > 0) {
      return {
        id: 'acceptance',
        label: '人工验收',
        status: 'blocked',
        detail: `${failed} 项未通过，请改完问题后重新确认；${passed}/${total} 已通过`
      };
    }
    if (pending === 0 && total > 0) {
      return {
        id: 'acceptance',
        label: '人工验收',
        status: 'ready',
        detail: `六项检查均已通过（${passed}/${total}）`
      };
    }
    if (!readyForAccept) {
      return {
        id: 'acceptance',
        label: '人工验收',
        status: 'na',
        detail: `历史进度 ${passed}/${total} 已通过；当前流程未处于可验收状态`
      };
    }
    return {
      id: 'acceptance',
      label: '人工验收',
      status: pending === total ? 'missing' : 'busy',
      detail: pending === total
        ? `共 ${total} 项待确认：先看预览，没问题再逐项点「通过」`
        : `还剩 ${pending} 项待确认（已通过 ${passed}，未通过 ${failed}）`
    };
  };

  const items = [previewItem(), packageItem(), acceptanceItem()];

  const nextAction = (() => {
    if (!input.runId) return '请先在左侧选择素材，并完成内容生成。';
    if (!readyForAccept && input.workflowStatus !== 'succeeded') {
      return '请先完成生成与审查；开放验收后，再回来勾选通过。';
    }
    if (failed > 0) return '先处理未通过的项（可写备注说明问题），修好后再重新勾选。';
    if (items.find(i => i.id === 'preview')?.status === 'busy') {
      return '预览还在生成，稍候在「交付」页观看，确认无误后再勾选验收项。';
    }
    if (items.find(i => i.id === 'preview')?.status === 'missing' || items.find(i => i.id === 'preview')?.status === 'blocked') {
      return '建议先到「交付」页生成或查看预览，确认画面与声音正常，再回来勾选通过。';
    }
    if (pending > 0) {
      return `还有 ${pending} 项未确认：看过预览后，对没问题的项点「通过」；有问题点「不通过」并填写备注。`;
    }
    return '验收项都已通过。需要存档时可下载成片包；技术细节见下方「详细信息」。';
  })();

  const headline = (() => {
    if (!input.runId) return '还没有可检查的成片';
    if (failed > 0) return '有未通过的检查项';
    if (readyForAccept && pending === 0 && total > 0) return '检查已完成，可以收工';
    if (readyForAccept) return '可以开始验收';
    if (input.workflowStatus === 'succeeded') return '流程已完成';
    return '验收尚未开放';
  })();

  const techDetails: Array<{ label: string; value: string }> = [];
  if (input.runId) techDetails.push({ label: '流程编号', value: input.runId });
  if (input.revisionId) techDetails.push({ label: '导出版本', value: input.revisionId });
  if (input.workflowStatus) {
    techDetails.push({
      label: '流程状态',
      value: input.workflowStage
        ? `${input.workflowStatus} / ${input.workflowStage}`
        : input.workflowStatus
    });
  }
  if (input.preview?.previewId) techDetails.push({ label: '预览编号', value: input.preview.previewId });
  if (input.preview?.inputSha256) techDetails.push({ label: '预览输入指纹', value: input.preview.inputSha256 });
  if (input.preview?.mp4Sha256) techDetails.push({ label: '预览成片指纹', value: input.preview.mp4Sha256 });

  return {
    headline,
    nextAction,
    items,
    counts: { passed, failed, pending, total },
    allPassed: total > 0 && pending === 0 && failed === 0,
    techDetails
  };
}

export function digestItemTagColor(status: DigestItemStatus): string {
  if (status === 'ready') return 'success';
  if (status === 'blocked') return 'error';
  if (status === 'busy') return 'processing';
  if (status === 'missing') return 'warning';
  return 'default';
}

export function digestItemStatusLabel(status: DigestItemStatus): string {
  switch (status) {
    case 'ready': return '就绪';
    case 'missing': return '待准备';
    case 'busy': return '进行中';
    case 'blocked': return '受阻';
    default: return '暂不可用';
  }
}

/** 测试/排序辅助：数字越小越需要用户关注 */
export function digestPriority(status: DigestItemStatus): number {
  return STATUS_WEIGHT[status];
}
