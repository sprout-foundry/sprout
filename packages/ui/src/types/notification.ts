export type NotificationType = 'info' | 'success' | 'warning' | 'error';

/**
 * Action button shown inline on a notification — e.g. "Configure" on a
 * missing-credential warning. The notification auto-dismisses when the
 * action is invoked unless `keepOpen` is true (rare; usually the user
 * wants the toast gone once they've acted on it).
 */
export interface NotificationAction {
  label: string;
  onClick: () => void;
  keepOpen?: boolean;
}

export interface NotificationData {
  id: string;
  type: NotificationType;
  title: string;
  message: string;
  duration?: number;
  createdAt: number;
  action?: NotificationAction;
}
