// Components
export { default as ContextMenu } from './components/ContextMenu';
export type { ContextMenuProps } from './components/ContextMenu';

export { default as NotificationStack } from './components/NotificationStack';
export { default as NotificationItem } from './components/NotificationItem';
export type { NotificationItemProps } from './components/NotificationItem';

export { default as StatusBar } from './components/StatusBar';
export type { StatusBarProps } from './components/StatusBar';

// Contexts
export { NotificationProvider, useNotifications } from './contexts/NotificationContext';
export type { NotificationType } from './services/notificationBus';
export type { Notification } from './contexts/NotificationContext';

// Services
export { notificationBus } from './services/notificationBus';
export type { NotificationEvent } from './services/notificationBus';

// Utils
export { generateUUID } from './utils/uuid';
export { detectLineEnding } from './utils/lineEndingDetect';
export type { LineEnding, LineEndingDetectionResult } from './utils/lineEndingDetect';
