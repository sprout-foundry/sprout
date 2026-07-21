//go:build !js

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/automate"
	"github.com/sprout-foundry/sprout/pkg/utils/pidalive"
	"github.com/sprout-foundry/sprout/pkg/webui"
)

const (
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
	// StartedAt is the wall-clock time the desired host process was
	// created. Used with processStartedBefore to guard against PID reuse:
	// if the original host died and the OS recycled the PID, the new
	// process will have a later start time.
	StartedAt time.Time `json:"started_at,omitempty"`
}

type webUISupervisor struct {
	webServer      *webui.ReactWebServer
	port           int
	announceStart  func(port int)
	announceAttach func(port int)

	mu               sync.Mutex
	attachedAnnounce bool
	startAnnounce    bool
	// attached is true once this supervisor has determined another healthy
	// process owns the Web UI host role (and consequently shut down its own
	// server). The startup loop in agent_modes.go checks this to avoid
	// timing out when attach-to-existing is the correct outcome.
	attached bool
}

func newWebUISupervisor(ws *webui.ReactWebServer, port int, announceStart func(port int), announceAttach func(port int)) *webUISupervisor {
	if port <= 0 {
		port = webui.DaemonPort
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

// HasAttached reports whether the supervisor has determined that another
// healthy process owns the Web UI host role. Callers that are waiting for
// a local web server to start should break out of their wait loop when
// this returns true — there is no local server coming.
func (s *webUISupervisor) HasAttached() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attached
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
	if desired > 0 && pidalive.IsAlive(desired) {
		if desired != pid {
			if s.webServer.IsRunning() {
				if shutdownErr := s.webServer.Shutdown(); shutdownErr != nil {
					log.Printf("[debug] failed to shutdown web server: %v", shutdownErr)
				}
			}
			s.mu.Lock()
			s.attached = true
			if !s.attachedAnnounce && s.announceAttach != nil {
				s.attachedAnnounce = true
				s.announceAttach(s.port)
			}
			s.mu.Unlock()
			return
		}
	} else if desired > 0 {
		if err := clearDesiredWebUIHostPID(); err != nil {
			log.Printf("[debug] failed to clear desired WebUI host PID: %v", err)
		}
	}

	// Another healthy process currently owns the web UI host role.
	if alive && record.PID != pid {
		if s.webServer.IsRunning() {
			if shutdownErr := s.webServer.Shutdown(); shutdownErr != nil {
				log.Printf("[debug] failed to shutdown web server: %v", shutdownErr)
			}
		}
		s.mu.Lock()
		s.attached = true
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
			log.Printf("[webui-supervisor] failed to start web server on port %d: %v", s.port, err)
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
	if err := saveWebUIHostRecord(webUIHostRecord{
		PID:       pid,
		Port:      s.port,
		StartedAt: recordProcessStartTime(pid),
		UpdatedAt: now,
	}); err != nil {
		log.Printf("[debug] failed to save WebUI host record: %v", err)
	}
}

func (s *webUISupervisor) cleanupHostRecordIfOwned() {
	record, err := loadWebUIHostRecord()
	if err != nil {
		return
	}
	if record.PID != os.Getpid() {
		return
	}
	if err := os.Remove(webUIHostFile()); err != nil && !os.IsNotExist(err) {
		log.Printf("[debug] failed to remove WebUI host file: %v", err)
	}
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
		return webUIHostRecord{}, fmt.Errorf("failed to read webUI host record file: %w", err)
	}
	var record webUIHostRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return webUIHostRecord{}, fmt.Errorf("failed to unmarshal webUI host record: %w", err)
	}
	return record, nil
}

func saveWebUIHostRecord(record webUIHostRecord) error {
	if err := os.MkdirAll(getConfigDir(), 0755); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal webUI host record: %w", err)
	}

	tmp := webUIHostFile() + ".tmp"
	if err := os.WriteFile(tmp, encoded, 0644); err != nil {
		return fmt.Errorf("failed to write webUI host record temp file: %w", err)
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
	// Staleness guard: the desired-host record is refreshed every heartbeat
	// (hostHeartbeatInterval). If it's older than hostStaleAfter the host
	// likely died without cleaning up — don't trust the PID.
	if time.Since(record.UpdatedAt) > hostStaleAfter {
		return 0
	}
	// PID-reuse guard: verify the process at this PID is the same one that
	// was recorded. If the host died and the OS recycled the PID, refuse to
	// treat the unrelated process as the desired host.
	if !automate.VerifyProcessStartedBefore(record.PID, record.StartedAt) {
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
		StartedAt: recordProcessStartTime(pid),
	}
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal desired webUI host PID record: %w", err)
	}
	tmp := desiredWebUIHostFile() + ".tmp"
	if err := os.WriteFile(tmp, encoded, 0644); err != nil {
		return fmt.Errorf("failed to write desired webUI host PID temp file: %w", err)
	}
	return os.Rename(tmp, desiredWebUIHostFile())
}

func clearDesiredWebUIHostPID() error {
	if err := os.Remove(desiredWebUIHostFile()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove desired webUI host PID file: %w", err)
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
	if !pidalive.IsAlive(record.PID) {
		return false
	}
	// PID-reuse guard: if StartedAt is recorded, verify the process at this
	// PID is the same one that created the host record. Without this check,
	// a recycled PID causes the supervisor to "attach" to an unrelated process.
	if !record.StartedAt.IsZero() && !automate.VerifyProcessStartedBefore(record.PID, record.StartedAt) {
		return false
	}
	return true
}
