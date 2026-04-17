package webui

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/configuration"
)

const (
	supportBundleMaxLogBytes = 512 * 1024 // 512 KB per log file
	supportBundleMaxLogFiles = 50
)

// handleAPISupportBundle handles GET /api/support-bundle.
// It builds a zip archive containing:
//   - redacted config snapshot (config.json)
//   - all *.log, *.jsonl files from the ledit config directory
//
// The archive is streamed directly to the client.
func (ws *ReactWebServer) handleAPISupportBundle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("ledit-diagnostics-%s.zip", timestamp)

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))

	zw := zip.NewWriter(w)
	defer zw.Close()

	// 1. Redacted config snapshot
	if err := writeBundleConfig(zw, ws); err != nil {
		// Config errors are non-fatal — continue bundling logs
		writeBundleText(zw, "config-error.txt", err.Error())
	}

	// 2. Log files from the ledit config directory
	configDir, err := configuration.GetConfigDir()
	if err == nil {
		writeBundleLogs(zw, configDir)
	}
}

// writeBundleConfig writes a redacted JSON config snapshot into the zip.
func writeBundleConfig(zw *zip.Writer, ws *ReactWebServer) error {
	if ws.agent == nil {
		return fmt.Errorf("agent not available")
	}
	cm := ws.agent.GetConfigManager()
	if cm == nil {
		return fmt.Errorf("config manager not available")
	}
	cfg := cm.GetConfig()
	redacted := configuration.RedactConfig(cfg)
	data, err := json.MarshalIndent(redacted, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return writeBundleBytes(zw, "config.json", data)
}

// writeBundleLogs walks configDir and adds *.log and *.jsonl files to the zip,
// capping each file at supportBundleMaxLogBytes and the total count at supportBundleMaxLogFiles.
func writeBundleLogs(zw *zip.Writer, configDir string) {
	count := 0
	_ = filepath.Walk(configDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if count >= supportBundleMaxLogFiles {
			return filepath.SkipAll
		}
		ext := strings.ToLower(filepath.Ext(info.Name()))
		if ext != ".log" && ext != ".jsonl" {
			return nil
		}

		rel, relErr := filepath.Rel(configDir, path)
		if relErr != nil {
			rel = info.Name()
		}

		if writeErr := writeBundleLogFile(zw, rel, path); writeErr == nil {
			count++
		}
		return nil
	})
}

// writeBundleLogFile reads up to supportBundleMaxLogBytes from the tail of a file
// and writes it into the zip archive at the given entry name.
func writeBundleLogFile(zw *zip.Writer, entryName, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	var data []byte
	if info.Size() <= supportBundleMaxLogBytes {
		data, err = io.ReadAll(f)
	} else {
		// Read the tail of the file so the most recent entries are included
		offset := info.Size() - supportBundleMaxLogBytes
		_, err = f.Seek(offset, io.SeekStart)
		if err == nil {
			data, err = io.ReadAll(f)
		}
	}
	if err != nil {
		return err
	}

	return writeBundleBytes(zw, "logs/"+entryName, data)
}

// writeBundleBytes adds a file entry to the zip archive.
func writeBundleBytes(zw *zip.Writer, name string, data []byte) error {
	fw, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = fw.Write(data)
	return err
}

// writeBundleText adds a plain-text error entry to the zip archive.
func writeBundleText(zw *zip.Writer, name, content string) {
	_ = writeBundleBytes(zw, name, []byte(content))
}
