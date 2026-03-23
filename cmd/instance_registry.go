package cmd

import (
	"context"
	"os"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
)

const (
	instanceHeartbeatInterval = 2 * time.Second
	instanceStaleAfter        = 12 * time.Second
)

type instanceTracker struct {
	id string
}

func startInstanceTracker(ctx context.Context, port int, chatAgent *agent.Agent) *instanceTracker {
	workingDir, err := os.Getwd()
	if err != nil {
		workingDir = "."
	}

	instanceID := "instance_" + time.Now().Format("20060102_150405.000000000") + "_" + itoa(os.Getpid())
	startedAt := time.Now()
	tracker := &instanceTracker{id: instanceID}

	go func() {
		ticker := time.NewTicker(instanceHeartbeatInterval)
		defer ticker.Stop()

		writeHeartbeat := func() {
			instances, err := loadInstances()
			if err != nil {
				instances = make(map[string]InstanceInfo)
			}
			cleanStaleInstances(instances, time.Now().Add(-instanceStaleAfter))

			sessionID := ""
			if chatAgent != nil {
				sessionID = chatAgent.GetSessionID()
			}

			now := time.Now()
			instances[instanceID] = InstanceInfo{
				ID:         instanceID,
				Port:       port,
				PID:        os.Getpid(),
				StartTime:  startedAt,
				WorkingDir: workingDir,
				LastPing:   now,
				SessionID:  sessionID,
			}
			_ = saveInstances(instances)
		}

		removeHeartbeat := func() {
			instances, err := loadInstances()
			if err != nil {
				return
			}
			delete(instances, instanceID)
			_ = saveInstances(instances)
		}

		writeHeartbeat()

		for {
			select {
			case <-ctx.Done():
				removeHeartbeat()
				return
			case <-ticker.C:
				writeHeartbeat()
			}
		}
	}()

	return tracker
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	buf := [20]byte{}
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + (v % 10))
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
