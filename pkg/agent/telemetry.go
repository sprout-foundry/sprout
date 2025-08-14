package agent

import (
	"encoding/json"
	"os"
	"time"
)

type telemetryEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Policy    string    `json:"policy"`
	Variant   string    `json:"variant"`
	Intent    string    `json:"intent"`
	Iteration int       `json:"iteration"`
	Action    string    `json:"action"`
	Status    string    `json:"status"`
}

func logTelemetry(cfgPath string, ev telemetryEvent) {
	f, err := os.OpenFile(cfgPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(ev)
	_, _ = f.Write(append(b, '\n'))
}
