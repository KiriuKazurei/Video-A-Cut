import { apiURL } from './runtimeUrl.ts';
export type * from '../../contracts/delivery-variant/v1/contracts';
export const variantQueryKey = (asset: string, run: string, revision: string) =>
  ['deliveryVariants', asset, run, revision] as const;
export const variantFileURL = (id: string, key: string) =>
  apiURL(`/api/delivery-variants/${encodeURIComponent(id)}/files/${encodeURIComponent(key)}`);
export const variantZIPURL = (id: string) => apiURL(`/api/delivery-variants/${encodeURIComponent(id)}/delivery.zip`);
