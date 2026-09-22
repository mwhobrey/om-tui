// Package cookiebridge relays Google Gaia cookie requests between the
// om-tui daemon and a Chrome MV3 extension connected through a native
// messaging host. The extension must initiate the connection; once the
// host is long-polling the daemon, credential repair can pull live cookies
// without DevTools paste or opening a ProtectLocalControl hole for
// chrome-extension:// origins.
package cookiebridge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	// HostName is the Chrome native messaging host identifier.
	HostName = "com.mwhobrey.om_tui.cookies"
	// ExtensionID is the stable unpacked extension ID derived from the
	// public key embedded in extensions/google-cookies/manifest.json.
	ExtensionID = "akakhclmanbjmbojfbjcnakfbmobinee"
	// DefaultRequestTimeout is how long the daemon waits for the extension
	// to answer a cookie pull.
	DefaultRequestTimeout = 3 * time.Second
)

var (
	// ErrBridgeOffline means no native host is currently waiting for work.
	ErrBridgeOffline = errors.New("chrome cookie bridge offline")
	// ErrBridgeTimeout means the host/extension did not reply in time.
	ErrBridgeTimeout = errors.New("chrome cookie bridge timed out")
	// ErrWaitIdle means the host wait timed out with no work (normal).
	ErrWaitIdle = errors.New("chrome cookie bridge wait idle")
)

// RequiredCookieNames are the Gaia + Messages cookies libgm needs for
// Google Account cookie reauth (matches mautrix-gmessages login fields).
var RequiredCookieNames = []string{"SID", "HSID", "SSID", "APISID", "SAPISID", "OSID"}

// OptionalCookieNames are collected when present and merged into session.json.
var OptionalCookieNames = []string{
	"__Secure-1PSIDTS",
	"__Secure-1PSID",
	"__Secure-3PSID",
	"__Secure-1PSIDCC",
	"__Secure-3PSIDCC",
}

// ValidateCookies ensures all required Gaia + Messages cookies are present.
func ValidateCookies(cookies map[string]string) error {
	if len(cookies) == 0 {
		return errors.New("empty cookie map")
	}
	var missing []string
	for _, name := range RequiredCookieNames {
		if strings.TrimSpace(cookies[name]) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required cookies: %s", strings.Join(missing, ", "))
	}
	return nil
}

// Request is work handed to a waiting native host.
type Request struct {
	ID string `json:"id"`
	Op string `json:"op"`
}

// Registry tracks the connected native host and in-flight cookie requests.
type Registry struct {
	mu sync.Mutex

	waiter *waiter
	pending *pending
}

type waiter struct {
	ch     chan Request
	cancel chan struct{}
}

type pending struct {
	id      string
	replyCh chan reply
}

type reply struct {
	cookies map[string]string
	err     error
}

// New returns an empty bridge registry.
func New() *Registry {
	return &Registry{}
}

// Online reports whether a native host is currently waiting for work.
func (r *Registry) Online() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.waiter != nil
}

// Wait blocks until the daemon issues a cookie request, ctx is cancelled,
// or timeout elapses. The host should call Wait in a loop while the
// extension port is open.
func (r *Registry) Wait(ctx context.Context, timeout time.Duration) (Request, error) {
	if timeout <= 0 {
		timeout = 25 * time.Second
	}
	w := &waiter{
		ch:     make(chan Request, 1),
		cancel: make(chan struct{}),
	}

	r.mu.Lock()
	if r.waiter != nil {
		close(r.waiter.cancel)
	}
	r.waiter = w
	// If a request is already pending with no delivery yet, hand it over.
	if r.pending != nil {
		req := Request{ID: r.pending.id, Op: "getGoogleCookies"}
		r.mu.Unlock()
		select {
		case w.ch <- req:
		default:
		}
	} else {
		r.mu.Unlock()
	}

	defer func() {
		r.mu.Lock()
		if r.waiter == w {
			r.waiter = nil
		}
		r.mu.Unlock()
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return Request{}, ctx.Err()
	case <-w.cancel:
		return Request{}, errors.New("cookie bridge wait superseded")
	case <-timer.C:
		return Request{}, ErrWaitIdle
	case req := <-w.ch:
		return req, nil
	}
}

// Reply delivers cookies (or an error) for a prior Request ID.
func (r *Registry) Reply(id string, cookies map[string]string, replyErr error) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("missing request id")
	}
	r.mu.Lock()
	p := r.pending
	if p == nil || p.id != id {
		r.mu.Unlock()
		return fmt.Errorf("unknown cookie bridge request %q", id)
	}
	r.pending = nil
	r.mu.Unlock()

	select {
	case p.replyCh <- reply{cookies: cookies, err: replyErr}:
		return nil
	default:
		return errors.New("cookie bridge reply already consumed")
	}
}

// RequestCookies asks the connected host/extension for Gaia cookies.
func (r *Registry) RequestCookies(ctx context.Context, timeout time.Duration) (map[string]string, error) {
	if timeout <= 0 {
		timeout = DefaultRequestTimeout
	}
	id, err := newRequestID()
	if err != nil {
		return nil, err
	}
	replyCh := make(chan reply, 1)

	r.mu.Lock()
	if r.waiter == nil {
		r.mu.Unlock()
		return nil, ErrBridgeOffline
	}
	if r.pending != nil {
		r.mu.Unlock()
		return nil, errors.New("cookie bridge request already in flight")
	}
	r.pending = &pending{id: id, replyCh: replyCh}
	w := r.waiter
	r.mu.Unlock()

	req := Request{ID: id, Op: "getGoogleCookies"}
	select {
	case w.ch <- req:
	default:
		// Waiter may already be waking; try non-blocking then fail closed.
		r.mu.Lock()
		if r.pending != nil && r.pending.id == id {
			r.pending = nil
		}
		r.mu.Unlock()
		return nil, ErrBridgeOffline
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		r.clearPending(id)
		return nil, ctx.Err()
	case <-timer.C:
		r.clearPending(id)
		return nil, ErrBridgeTimeout
	case rep := <-replyCh:
		if rep.err != nil {
			return nil, rep.err
		}
		if err := ValidateCookies(rep.cookies); err != nil {
			return nil, err
		}
		return rep.cookies, nil
	}
}

func (r *Registry) clearPending(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending != nil && r.pending.id == id {
		r.pending = nil
	}
}

func newRequestID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
