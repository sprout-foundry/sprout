import { useState, useEffect, useCallback } from 'react';
import { ApiService, LeditInstance } from '../services/api';
import { INSTANCE_PID_STORAGE_KEY, INSTANCE_SWITCH_RESET_KEY } from '../constants/app';

export interface UseInstanceManagerOptions {
  isConnected: boolean;
  apiService: ApiService;
}

export interface UseInstanceManagerReturn {
  instances: LeditInstance[];
  selectedInstancePID: number;
  isSwitchingInstance: boolean;
  handleInstanceChange: (pid: number) => Promise<void>;
}

export function useInstanceManager({
  isConnected,
  apiService,
}: UseInstanceManagerOptions): UseInstanceManagerReturn {
  const [instances, setInstances] = useState<LeditInstance[]>([]);
  const [selectedInstancePID, setSelectedInstancePID] = useState<number>(0);
  const [isSwitchingInstance, setIsSwitchingInstance] = useState(false);

  // Poll instances every 2s while connected
  useEffect(() => {
    if (!isConnected) {
      return;
    }

    let cancelled = false;
    let timer: NodeJS.Timeout | null = null;

    const loadInstances = async () => {
      try {
        const data = await apiService.getInstances();
        if (cancelled) {
          return;
        }
        setInstances(data.instances || []);
        const currentPort = Number(window.location.port || 0);
        const currentInstance =
          (data.instances || []).find((instance) => instance.port === currentPort) ||
          (data.instances || []).find((instance) => instance.is_current) ||
          (data.instances || []).find((instance) => instance.pid === data.active_host_pid);
        const nextPID = currentInstance?.pid || 0;
        if (nextPID > 0) {
          setSelectedInstancePID(nextPID);
          window.localStorage.setItem(INSTANCE_PID_STORAGE_KEY, String(nextPID));
        }
      } catch (error) {
        if (!cancelled) {
          console.error('Failed to fetch instances:', error);
        }
      }
      if (!cancelled) {
        timer = setTimeout(loadInstances, 2000);
      }
    };

    loadInstances();
    return () => {
      cancelled = true;
      if (timer) {
        clearTimeout(timer);
      }
    };
  }, [apiService, isConnected]);

  const handleInstanceChange = useCallback(async (pid: number) => {
    if (!Number.isFinite(pid) || pid <= 0 || pid === selectedInstancePID) {
      return;
    }

    setIsSwitchingInstance(true);
    try {
      const targetInstance = instances.find((instance) => instance.pid === pid);
      if (!targetInstance || !targetInstance.port) {
        throw new Error('Selected instance is unavailable');
      }

      window.localStorage.setItem(INSTANCE_PID_STORAGE_KEY, String(pid));
      window.sessionStorage.setItem(INSTANCE_SWITCH_RESET_KEY, '1');
      const nextURL = new URL(window.location.href);
      nextURL.port = String(targetInstance.port);
      window.location.assign(nextURL.toString());
    } catch (error) {
      console.error('Failed to switch instance:', error);
      setIsSwitchingInstance(false);
    }
  }, [instances, selectedInstancePID]);

  return {
    instances,
    selectedInstancePID,
    isSwitchingInstance,
    handleInstanceChange,
  };
}
