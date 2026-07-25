// Package slack adapts slacklive clients to the bridge registry surface.
package slack

import (
	"context"
	"fmt"
	"time"

	"github.com/maxghenis/openmessage/internal/bridge"
	"github.com/maxghenis/openmessage/internal/slacklive"
)

// Adapter is a thin registry entry for one Slack river.
type Adapter struct {
	accountID string
	client    *slacklive.Client
}

func New(accountID string, client *slacklive.Client) *Adapter {
	return &Adapter{accountID: accountID, client: client}
}

func (a *Adapter) AccountID() string           { return a.accountID }
func (a *Adapter) Platform() bridge.Platform   { return bridge.PlatformSlack }
func (a *Adapter) DeclaredCapabilities() bridge.CapabilitySet {
	return bridge.CapabilitySet{TextSend: true}
}

func (a *Adapter) Start(ctx context.Context, req bridge.StartRequest, sink bridge.ConnectionSink) (bridge.Run, error) {
	if a.client == nil {
		return nil, fmt.Errorf("slack adapter: client required")
	}
	if err := a.client.Start(ctx); err != nil {
		return nil, err
	}
	return &run{client: a.client, started: time.Now()}, nil
}

type run struct {
	client  *slacklive.Client
	started time.Time
	ready   chan struct{}
	done    chan error
}

func (r *run) Ready() <-chan struct{} {
	if r.ready == nil {
		r.ready = make(chan struct{})
		close(r.ready)
	}
	return r.ready
}

func (r *run) Done() <-chan error {
	if r.done == nil {
		r.done = make(chan error)
	}
	return r.done
}

func (r *run) Probe(ctx context.Context) (bridge.Liveness, error) {
	ok, detail := r.client.Status()
	if !ok {
		return bridge.Liveness{AliveAt: time.Now(), Detail: detail}, fmt.Errorf("slack disconnected: %s", detail)
	}
	return bridge.Liveness{AliveAt: time.Now(), Detail: "ok"}, nil
}

func (r *run) Stop(ctx context.Context) error {
	r.client.Stop()
	if r.done != nil {
		select {
		case <-r.done:
		default:
			close(r.done)
		}
	}
	return nil
}
