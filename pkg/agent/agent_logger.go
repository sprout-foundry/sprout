package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// LogEntry represents a structured log entry
type LogEntry struct {
	Timestamp string            `json:"timestamp"`
	Level     string            `json:"level"` // "debug", "info", "warn", "error"
	Message   string            `json:"message"`
	SessionID string            `json:"session_id,omitempty"`
	Iteration int               `json:"iteration,omitempty"`
	Provider  string            `json:"provider,omitempty"`
	Model     string            `json:"model,omitempty"`
	Fields    map[string]string `json:"fields,omitempty"`
}

// AgentLogger wraps the agent and provides context-aware logging
type AgentLogger struct {
	agent    *Agent
	file     *os.File
	mu       sync.Mutex
	jsonMode bool // when true, output JSON lines; otherwise human-readable
}

// LogContext allows chaining fields for multiple log entries
type LogContext struct {
	logger *AgentLogger
	fields map[string]string
}

// NewAgentLogger creates a logger, using the agent's existing debug log file if available
func NewAgentLogger(agent *Agent) *AgentLogger {
	if agent == nil {
		return nil
	}

	return &AgentLogger{
		agent: agent,
		file:  agent.debugLogFile,
	}
}

// SetJSONMode sets whether the logger outputs JSON or human-readable text
func (l *AgentLogger) SetJSONMode(jsonMode bool) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.jsonMode = jsonMode
}

// contextFields extracts sessionID, iteration, provider, model from the agent
func (l *AgentLogger) contextFields() map[string]string {
	if l == nil || l.agent == nil {
		return nil
	}

	fields := make(map[string]string)

	if l.agent.state != nil {
		if sessionID := l.agent.GetSessionID(); sessionID != "" {
			fields["session"] = sessionID
		}
		if iteration := l.agent.GetCurrentIteration(); iteration > 0 {
			fields["iter"] = fmt.Sprintf("%d", iteration)
		}
	}

	if provider := l.agent.GetProvider(); provider != "" {
		fields["provider"] = provider
	}

	if model := l.agent.GetModel(); model != "" {
		fields["model"] = model
	}

	return fields
}

// writeEntry formats and writes the log entry.
// Uses TryLock to prevent self-deadlock if any internal call re-enters
// the logger (e.g., state access triggering another debugLog call).
func (l *AgentLogger) writeEntry(level, message string, extraFields map[string]string) {
	if l == nil {
		return
	}

	// Skip debug messages if debug mode is disabled
	if level == "debug" && l.agent != nil && !l.agent.debug {
		return
	}

	// TryLock prevents self-deadlock on reentrant calls from state access.
	if !l.mu.TryLock() {
		// Reentrant call — write directly to file/stderr without formatting
		// to avoid losing the message while preventing deadlock.
		if l.file != nil {
			timestamp := time.Now().Format("15:04:05.000")
			_, _ = l.file.WriteString(fmt.Sprintf("[%s] [%s] %s\n", timestamp, level, message))
		} else {
			fmt.Fprintf(os.Stderr, "[%s] [%s] %s\n", time.Now().Format("15:04:05.000"), level, message)
		}
		return
	}
	defer l.mu.Unlock()

	timestamp := time.Now().Format("15:04:05.000")

	// Determine output destination
	writer := l.file
	if writer == nil {
		// Fall back to stderr if no file is available
		writer = os.Stderr
	}

	// Collect context fields
	contextFields := l.contextFields()
	if contextFields == nil {
		contextFields = make(map[string]string)
	}

	// Merge extra fields
	for k, v := range extraFields {
		contextFields[k] = v
	}

	// Format output
	if l.jsonMode {
		entry := LogEntry{
			Timestamp: timestamp,
			Level:     level,
			Message:   message,
		}

		if sessionID, ok := contextFields["session"]; ok {
			entry.SessionID = sessionID
		}
		if iterStr, ok := contextFields["iter"]; ok && iterStr != "" {
			fmt.Sscanf(iterStr, "%d", &entry.Iteration)
		}
		if provider, ok := contextFields["provider"]; ok {
			entry.Provider = provider
		}
		if model, ok := contextFields["model"]; ok {
			entry.Model = model
		}

		// Add remaining fields
		entry.Fields = make(map[string]string)
		for k, v := range contextFields {
			if k != "session" && k != "iter" && k != "provider" && k != "model" {
				entry.Fields[k] = v
			}
		}

		if data, err := json.Marshal(entry); err == nil {
			_, _ = writer.Write(append(data, '\n'))
		}
	} else {
		// Human-readable format: [15:04:05.000] [DEBUG] [session=abc123 iter=5 provider=openai model=gpt-4] message
		// Use deterministic field order for consistent log parsing
		fieldOrder := []string{"session", "iter", "provider", "model"}
		var contextParts []string
		for _, k := range fieldOrder {
			if v, ok := contextFields[k]; ok {
				contextParts = append(contextParts, fmt.Sprintf("%s=%s", k, v))
				delete(contextFields, k)
			}
		}
		// Append any extra fields in map order
		for k, v := range contextFields {
			contextParts = append(contextParts, fmt.Sprintf("%s=%s", k, v))
		}

		contextStr := ""
		if len(contextParts) > 0 {
			contextStr = " [" + strings.Join(contextParts, " ") + "]"
		}

		line := fmt.Sprintf("[%s] [%s]%s %s\n", timestamp, level, contextStr, message)
		_, _ = writer.WriteString(line)
	}
}

// Debug writes a debug-level log with context
func (l *AgentLogger) Debug(format string, args ...interface{}) {
	if l == nil {
		return
	}
	message := fmt.Sprintf(format, args...)
	l.writeEntry("debug", message, nil)
}

// Info writes an info-level log with context
func (l *AgentLogger) Info(format string, args ...interface{}) {
	if l == nil {
		return
	}
	message := fmt.Sprintf(format, args...)
	l.writeEntry("info", message, nil)
}

// Warn writes a warn-level log with context
func (l *AgentLogger) Warn(format string, args ...interface{}) {
	if l == nil {
		return
	}
	message := fmt.Sprintf(format, args...)
	l.writeEntry("warn", message, nil)
}

// Error writes an error-level log with context
func (l *AgentLogger) Error(format string, args ...interface{}) {
	if l == nil {
		return
	}
	message := fmt.Sprintf(format, args...)
	l.writeEntry("error", message, nil)
}

// WithFields returns a context that adds extra fields to all subsequent logs
func (l *AgentLogger) WithFields(fields map[string]string) *LogContext {
	if l == nil {
		return nil
	}
	return &LogContext{
		logger: l,
		fields: fields,
	}
}

// Debug writes a debug-level log with context fields from the LogContext
func (lc *LogContext) Debug(format string, args ...interface{}) {
	if lc == nil || lc.logger == nil {
		return
	}
	message := fmt.Sprintf(format, args...)
	lc.logger.writeEntry("debug", message, lc.fields)
}

// Info writes an info-level log with context fields from the LogContext
func (lc *LogContext) Info(format string, args ...interface{}) {
	if lc == nil || lc.logger == nil {
		return
	}
	message := fmt.Sprintf(format, args...)
	lc.logger.writeEntry("info", message, lc.fields)
}

// Warn writes a warn-level log with context fields from the LogContext
func (lc *LogContext) Warn(format string, args ...interface{}) {
	if lc == nil || lc.logger == nil {
		return
	}
	message := fmt.Sprintf(format, args...)
	lc.logger.writeEntry("warn", message, lc.fields)
}

// Error writes an error-level log with context fields from the LogContext
func (lc *LogContext) Error(format string, args ...interface{}) {
	if lc == nil || lc.logger == nil {
		return
	}
	message := fmt.Sprintf(format, args...)
	lc.logger.writeEntry("error", message, lc.fields)
}
