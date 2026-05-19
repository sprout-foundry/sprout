//go:build browser

package webcontent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

type browserSession struct {
	id        string
	incognito *rod.Browser
	page      *rod.Page
	mu        sync.Mutex
	createdAt time.Time
	lastUsed  time.Time
}

// openIncognitoPage creates an incognito browser context and opens a new page.
// The caller MUST defer close on both the incognito browser and the page.
func (r *rodRenderer) openIncognitoPage(ctx context.Context) (*rod.Browser, *rod.Page, error) {
	browser, err := r.connect(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("browser connect: %w", err)
	}

	incognito, err := browser.Incognito()
	if err != nil {
		return nil, nil, fmt.Errorf("incognito context: %w", err)
	}

	page, err := incognito.Page(proto.TargetCreateTarget{})
	if err != nil {
		_ = incognito.Close()
		return nil, nil, fmt.Errorf("open page: %w", err)
	}

	return incognito, page, nil
}

func newBrowserSessionID() string {
	return fmt.Sprintf("browser_%d", time.Now().UnixNano())
}

func (r *rodRenderer) acquireSession(ctx context.Context, requestedID string) (*browserSession, error) {
	sessionID := strings.TrimSpace(requestedID)
	if sessionID == "" {
		sessionID = newBrowserSessionID()
	}

	r.mu.Lock()
	if r.sessions == nil {
		r.sessions = make(map[string]*browserSession)
	}
	if existing, ok := r.sessions[sessionID]; ok {
		r.mu.Unlock()
		existing.mu.Lock()
		existing.lastUsed = time.Now()
		return existing, nil
	}
	r.mu.Unlock()

	incognito, page, err := r.openIncognitoPage(ctx)
	if err != nil {
		return nil, fmt.Errorf("open page: %w", err)
	}
	session := &browserSession{
		id:        sessionID,
		incognito: incognito,
		page:      page,
		createdAt: time.Now(),
		lastUsed:  time.Now(),
	}

	r.mu.Lock()
	if r.sessions == nil {
		r.sessions = make(map[string]*browserSession)
	}
	if existing, ok := r.sessions[sessionID]; ok {
		r.mu.Unlock()
		_ = page.Close()
		_ = incognito.Close()
		existing.mu.Lock()
		existing.lastUsed = time.Now()
		return existing, nil
	}
	r.sessions[sessionID] = session
	r.mu.Unlock()

	session.mu.Lock()
	return session, nil
}

func (r *rodRenderer) closeSessionByID(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}

	r.mu.Lock()
	session, ok := r.sessions[sessionID]
	if ok {
		delete(r.sessions, sessionID)
	}
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown browser session %q", sessionID)
	}

	session.mu.Lock()
	defer session.mu.Unlock()
	if session.page != nil {
		_ = session.page.Close()
	}
	if session.incognito != nil {
		_ = session.incognito.Close()
	}
	return nil
}
