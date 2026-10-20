import { Alert, Button } from 'antd';
import { errorText } from './ui';

/** Local window actions are human actions; errors never offer an unlock flow. */
export function ProtectedError({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  return <Alert type="error" showIcon message={errorText(error)} action={onRetry ? <Button size="small" onClick={onRetry}>重试</Button> : undefined} />;
}
