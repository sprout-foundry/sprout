package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/webui"
)

const (
	defaultWebUIPort      = 54421
	hostHeartbeatInterval = 1 * time.Second
	hostStaleAfter        = 4 * time.Second
)

type webUIHostRecord struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	StartedAt time.Time `json:"started_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type desiredWebUIHostRecord struct {
	PID       int       `json:"pid"`
	UpdatedAt time.Time `json:"updated_at"`
}

type webUISupervisor struct {
	webServer      *webui.ReactWebServer
	port           int
	announceStart  func(port int)
	announceAttach func(port int)

	mu               sync.Mutex
	attachedAnnounce bool
	startAnnounce    bool
}

func newWebUISupervisor(ws *webui.ReactWebServer, port int, announceStart func(port int), announceAttach func(port int)) *webUISupervisor {
	if port <= 0 {
		port = defaultWebUIPort
	}
	return &webUISupervisor{
		webServer:        ws,
		port:             port,
		announceStart:    announceStart,
		announceAttach:   announceAttach,
		startAnnounce:    false,
		attachedAnnounce: false,
	}
}

func (s *webUISupervisor) Run(ctx context.Context) {
	ticker := time.NewTicker(hostHeartbeatInterval)
	defer ticker.Stop()

	// Reconcile immediately so UI host starts quickly when no leader exists.
	s.reconcile(ctx)

	for {
		select {
		case <-ctx.Done():
			s.cleanupHostRecordIfOwned()
			return
		case <-ticker.C:
			s.reconcile(ctx)
		}
	}
}

func (s *webUISupervisor) reconcile(ctx context.Context) {
	record, _ := loadWebUIHostRecord()
	alive := isHostRecordAlive(record)
	pid := os.Getpid()
	desired := loadDesiredWebUIHostPID()

	// If a specific PID is selected and still alive, enforce it as leader.
	if desired > 0 && isProcessAlive(desired) {
		if desired != pid {
			if s.webServer.IsRunning() {
				_ = s.webServer.Shutdown()
			}
			s.mu.Lock()
			if !s.attachedAnnounce && s.announceAttach != nil {
				s.attachedAnnounce = true
				s.announceAttach(s.port)
			}
			s.mu.Unlock()
			return
		}
	} else if desired > 0 {
		_ = clearDesiredWebUIHostPID()
	}

	// Another healthy process currently owns the web UI host role.
	if alive && record.PID != pid {
		if s.webServer.IsRunning() {
			_ = s.webServer.Shutdown()
		}
		s.mu.Lock()
		if !s.attachedAnnounce && s.announceAttach != nil {
			s.attachedAnnounce = true
			s.announceAttach(record.Port)
		}
		s.mu.Unlock()
		return
	}

	// Either we already own leadership or there is no healthy host.
	if !s.webServer.IsRunning() {
		if err := s.webServer.Start(ctx); err != nil {
			return
		}
		s.mu.Lock()
		if !s.startAnnounce && s.announceStart != nil {
			s.startAnnounce = true
			s.announceStart(s.port)
		}
		s.mu.Unlock()
	}

	now := time.Now()
	_ = saveWebUIHostRecord(webUIHostRecord{
		PID:       pid,
		Port:      s.port,
		StartedAt: now,
		UpdatedAt: now,
	})
}

func (s *webUISupervisor) cleanupHostRecordIfOwned() {
	record, err := loadWebUIHostRecord()
	if err != nil {
		return
	}
	if record.PID != os.Getpid() {
		return
	}
	_ = os.Remove(webUIHostFile())
}

func webUIHostFile() string {
	return filepath.Join(getConfigDir(), "webui_host.json")
}

func desiredWebUIHostFile() string {
	return filepath.Join(getConfigDir(), "webui_desired_host.json")
}

func loadWebUIHostRecord() (webUIHostRecord, error) {
	data, err := os.ReadFile(webUIHostFile())
	if err != nil {
		return webUIHostRecord{}, err
	}
	var record webUIHostRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return webUIHostRecord{}, err
	}
	return record, nil
}

func saveWebUIHostRecord(record webUIHostRecord) error {
	if err := os.MkdirAll(getConfigDir(), 0755); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}

	tmp := webUIHostFile() + ".tmp"
	if err := os.WriteFile(tmp, encoded, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, webUIHostFile())
}

func loadDesiredWebUIHostPID() int {
	data, err := os.ReadFile(desiredWebUIHostFile())
	if err != nil {
		return 0
	}
	var record desiredWebUIHostRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return 0
	}
	if record.PID <= 0 {
		return 0
	}
	return record.PID
}

func saveDesiredWebUIHostPID(pid int) error {
	if err := os.MkdirAll(getConfigDir(), 0755); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}
	record := desiredWebUIHostRecord{
		PID:       pid,
		UpdatedAt: time.Now(),
	}
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	tmp := desiredWebUIHostFile() + ".tmp"
	if err := os.WriteFile(tmp, encoded, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, desiredWebUIHostFile())
}

func clearDesiredWebUIHostPID() error {
	if err := os.Remove(desiredWebUIHostFile()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func isHostRecordAlive(record webUIHostRecord) bool {
	if record.PID <= 0 || record.Port <= 0 {
		return false
	}
	if time.Since(record.UpdatedAt) > hostStaleAfter {
		return false
	}
	return isProcessAlive(record.PID)
}

func isProcessAlive(pid int) bool {
	return isPIDAlive(pid)
}
