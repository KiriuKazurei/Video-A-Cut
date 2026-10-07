export type * from '../../contracts/delivery-variant/v1/contracts';
export const variantQueryKey = (asset: string, run: string, revision: string) =>
  ['deliveryVariants', asset, run, revision] as const;
export const variantFileURL = (id: string, key: string) =>
  `/api/delivery-variants/${encodeURIComponent(id)}/files/${encodeURIComponent(key)}`;
export const variantZIPURL = (id: string) => `/api/delivery-variants/${encodeURIComponent(id)}/delivery.zip`;
