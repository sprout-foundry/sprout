import { useState, useEffect, useCallback } from 'react';
import { supportsInstances } from '../config/mode';
import type { ApiService, SproutInstance } from '../services/api';

const INSTANCE_PID_STORAGE_KEY = 'sprout:webui:instancePid';

export interface UseInstancesParams {
  apiService: ApiService;
  isConnected: boolean;
}

export interface UseInstancesReturn {
  instances: SproutInstance[];
  selectedInstancePID: number;
  isSwitchingInstance: boolean;
  onInstanceChange: (pid: number) => Promise<void>;
}

export const useInstances = ({ apiService, isConnected }: UseInstancesParams): UseInstancesReturn => {
  const [instances, setInstances] = useState<SproutInstance[]>([]);
  const [selectedInstancePID, setSelectedInstancePID] = useState<number>(0);
  const [isSwitchingInstance, setIsSwitchingInstance] = useState(false);

  const handleInstanceChange = useCallback(
    async (pid: number) => {
      if (!supportsInstances) return;
      if (!Number.isFinite(pid) || pid <= 0 || pid === selectedInstancePID) {
        return;
      }

      setIsSwitchingInstance(true);
      try {
        const targetInstance = instances.find((instance) => instance.pid === pid);
        if (!targetInstance || !targetInstance.port) {
          throw new Error('Selected instance is unavailable');
        }

        const INSTANCE_SWITCH_RESET_KEY = 'sprout:webui:instanceSwitchReset';
        window.localStorage.setItem(INSTANCE_PID_STORAGE_KEY, String(pid));
        window.sessionStorage.setItem(INSTANCE_SWITCH_RESET_KEY, '1');
        const nextURL = new URL(window.location.href);
        nextURL.port = String(targetInstance.port);
        window.location.assign(nextURL.toString());
      } catch (error) {
        console.error('Failed to switch instance:', error);
        setIsSwitchingInstance(false);
      }
    },
    [instances, selectedInstancePID],
  );

  useEffect(() => {
    if (!supportsInstances || !isConnected) {
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

  return {
    instances,
    selectedInstancePID,
    isSwitchingInstance,
    onInstanceChange: handleInstanceChange,
  };
};
