export interface VariantStartRequest {
  revision_id: string;
  expected_version: number;
  request_id: string;
  spec_id: 'ass-basic-v1';
}
export interface VariantArtifact {
  key: string; kind: string; path: string; mime: string; size: number; sha256: string;
}
export interface VariantView {
  variant_id: string; task_id: string; asset_id: string; run_id: string; revision_id: string;
  spec_id: string; format: string; input_sha256: string;
  status: string; progress: number; message?: string;
  invalidated: boolean; is_current: boolean; artifacts: VariantArtifact[];
}
export interface VariantList {
  run_id: string; revision_id: string; expected_version: number; can_start: boolean;
  checks: { code: string; status: string; message?: string }[];
  specs: { spec_id: string; sha256: string }[];
  variants: VariantView[];
}
