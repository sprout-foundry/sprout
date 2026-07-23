import { createContext, useContext, useReducer, useCallback, useEffect, type ReactNode } from 'react';
import { generateUUID } from '../utils/uuid';
import {
  notificationBus,
  type NotificationEvent,
  type NotificationType as BusNotificationType,
} from '../services/notificationBus';
import type { NotificationAction, NotificationData } from '../types/notification';

export type { NotificationAction } from '../types/notification';

// Re-export NotificationType for convenience
export type NotificationType = BusNotificationType;

export type Notification = NotificationData;

interface NotificationState {
  notifications: Notification[];
}

interface AddNotificationPayload {
  type: NotificationType;
  title: string;
  message: string;
  duration?: number;
  id?: string;
  action?: NotificationAction;
}

type NotificationReducerAction =
  | { type: 'ADD_NOTIFICATION'; payload: AddNotificationPayload }
  | { type: 'REMOVE_NOTIFICATION'; payload: string }
  | { type: 'CLEAR_NOTIFICATIONS' }
  | { type: 'MARK_ALL_READ' };

const notificationReducer = (state: NotificationState, action: NotificationReducerAction): NotificationState => {
  switch (action.type) {
    case 'ADD_NOTIFICATION': {
      const newNotification: Notification = {
        ...action.payload,
        id: action.payload.id ?? generateUUID(),
        createdAt: Date.now(),
        read: false,
      };
      return {
        ...state,
        notifications: [...state.notifications, newNotification],
      };
    }

    case 'REMOVE_NOTIFICATION':
      return {
        ...state,
        notifications: state.notifications.filter((n) => n.id !== action.payload),
      };

    case 'CLEAR_NOTIFICATIONS':
      return {
        ...state,
        notifications: [],
      };

    case 'MARK_ALL_READ':
      return {
        ...state,
        notifications: state.notifications.map((n) => ({ ...n, read: true })),
      };

    default:
      return state;
  }
};

interface NotificationContextValue {
  notifications: Notification[];
  addNotification: (
    type: NotificationType,
    title: string,
    message: string,
    duration?: number,
    action?: NotificationAction,
  ) => string;
  removeNotification: (id: string) => void;
  clearNotifications: () => void;
  markAllRead: () => void;
}

const NotificationContext = createContext<NotificationContextValue | null>(null);

export const useNotifications = () => {
  const context = useContext(NotificationContext);
  if (!context) {
    throw new Error('useNotifications must be used within NotificationProvider');
  }
  return context;
};

interface NotificationProviderProps {
  children: ReactNode;
}

const initialState: NotificationState = {
  notifications: [],
};

export function NotificationProvider({ children }: NotificationProviderProps): JSX.Element {
  const [state, dispatch] = useReducer(notificationReducer, initialState);

  const addNotification = useCallback(
    (
      type: NotificationType,
      title: string,
      message: string,
      duration?: number,
      action?: NotificationAction,
    ): string => {
      const id = generateUUID();
      // Clamp duration between 0 and 60000ms (60 seconds)
      const clampedDuration = duration !== undefined ? Math.max(0, Math.min(duration, 60000)) : undefined;
      dispatch({
        type: 'ADD_NOTIFICATION',
        payload: { type, title, message, duration: clampedDuration, id, action },
      });
      return id;
    },
    [],
  );

  const removeNotification = useCallback((id: string) => {
    dispatch({ type: 'REMOVE_NOTIFICATION', payload: id });
  }, []);

  const clearNotifications = useCallback(() => {
    dispatch({ type: 'CLEAR_NOTIFICATIONS' });
  }, []);

  const markAllRead = useCallback(() => {
    dispatch({ type: 'MARK_ALL_READ' });
  }, []);

  // Subscribe to external notificationBus events
  useEffect(() => {
    const unsubscribe = notificationBus.onNotification((event: NotificationEvent) => {
      // Clamp duration between 0 and 60000ms (same as addNotification)
      const clampedDuration = event.duration !== undefined ? Math.max(0, Math.min(event.duration, 60000)) : undefined;

      dispatch({
        type: 'ADD_NOTIFICATION',
        payload: {
          type: event.type,
          title: event.title,
          message: event.message,
          duration: clampedDuration,
          id: event.id,
          action: event.action,
        },
      });
    });

    return unsubscribe;
  }, []);

  const value: NotificationContextValue = {
    notifications: state.notifications,
    addNotification,
    removeNotification,
    clearNotifications,
    markAllRead,
  };

  return <NotificationContext.Provider value={value}>{children}</NotificationContext.Provider>;
}
