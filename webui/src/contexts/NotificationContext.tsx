import { createContext, useContext, useReducer, useCallback, useEffect, type ReactNode } from 'react';
import type { FC } from 'react';
import { generateUUID } from '../utils/uuid';
import { notificationBus } from '../services/notificationBus';

export type NotificationType = 'info' | 'success' | 'warning' | 'error';

export interface Notification {
  id: string;
  type: NotificationType;
  title: string;
  message: string;
  duration?: number;
  createdAt: number;
}

interface NotificationState {
  notifications: Notification[];
}

interface AddNotificationPayload {
  type: NotificationType;
  title: string;
  message: string;
  duration?: number;
  id?: string;
}

type NotificationAction =
  | { type: 'ADD_NOTIFICATION'; payload: AddNotificationPayload }
  | { type: 'REMOVE_NOTIFICATION'; payload: string }
  | { type: 'CLEAR_NOTIFICATIONS' };

const notificationReducer = (state: NotificationState, action: NotificationAction): NotificationState => {
  switch (action.type) {
    case 'ADD_NOTIFICATION': {
      const newNotification: Notification = {
        ...action.payload,
        id: action.payload.id ?? generateUUID(),
        createdAt: Date.now(),
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

    default:
      return state;
  }
};

interface NotificationContextValue {
  notifications: Notification[];
  addNotification: (type: NotificationType, title: string, message: string, duration?: number) => string;
  removeNotification: (id: string) => void;
  clearNotifications: () => void;
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

export const NotificationProvider: FC<NotificationProviderProps> = ({ children }) => {
  const [state, dispatch] = useReducer(notificationReducer, initialState);

  const addNotification = useCallback(
    (type: NotificationType, title: string, message: string, duration?: number): string => {
      const id = generateUUID();
      // Clamp duration between 0 and 60000ms (60 seconds)
      const clampedDuration = duration !== undefined ? Math.max(0, Math.min(duration, 60000)) : undefined;
      dispatch({
        type: 'ADD_NOTIFICATION',
        payload: { type, title, message, duration: clampedDuration, id },
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

  // Subscribe to external notificationBus events
  useEffect(() => {
    const unsubscribe = notificationBus.onNotification((event) => {
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
  };

  return <NotificationContext.Provider value={value}>{children}</NotificationContext.Provider>;
};
