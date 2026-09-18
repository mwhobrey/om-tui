package app

import (
	"errors"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

var errGoogleSendsRepeatedlyFailed = errors.New("Google sends repeatedly failed while the phone was responding")

// GoogleLifecycleNotifier lets legacy App send paths report lifecycle changes
// without owning reconnect or repair. The Google bridge adapter implements
// this interface and leaves all generation changes to the supervisor.
type GoogleLifecycleNotifier interface {
	// ReportError ends the current generation with an error that the
	// supervisor classifies, including recoverable credential expiry.
	ReportError(error) bool
	// ParkCurrent ends the current generation in a terminal manual-repair
	// state for a known-dead linked session.
	ParkCurrent(error) bool
}

// GoogleHistoryIngest tees Google list/fetch history into the v2 ingest sink.
// The Google bridge adapter implements this; a nil value means v1-only.
type GoogleHistoryIngest interface {
	IngestHistoryConversation(*gmproto.Conversation)
	IngestHistoryMessage(*gmproto.Message)
}

// SetGoogleLifecycleNotifier installs the lifecycle owner notified by every
// App Google send path. It is safe to replace or clear the notifier while send
// operations are in flight.
func (a *App) SetGoogleLifecycleNotifier(notifier GoogleLifecycleNotifier) {
	a.googleLifecycleMu.Lock()
	a.googleLifecycleNotifier = notifier
	a.googleLifecycleMu.Unlock()
}

// SetGoogleHistoryIngest installs the v2 ingest tee for Google history
// fetches (startup shallow backfill, deep backfill, phone backfill). Live
// frames already go through the adapter sink; request/response FetchMessages
// does not, so PRIMARY would otherwise leave that history in frozen v1.
func (a *App) SetGoogleHistoryIngest(ingest GoogleHistoryIngest) {
	a.googleLifecycleMu.Lock()
	a.googleHistoryIngest = ingest
	a.googleLifecycleMu.Unlock()
}

func (a *App) googleLifecycle() GoogleLifecycleNotifier {
	a.googleLifecycleMu.RLock()
	notifier := a.googleLifecycleNotifier
	a.googleLifecycleMu.RUnlock()
	return notifier
}

func (a *App) reportGoogleLifecycleError(err error) {
	if notifier := a.googleLifecycle(); notifier != nil {
		notifier.ReportError(err)
	}
}

func (a *App) markGoogleNeedsRepairAndPark(err error) {
	if !a.googleNeedsRepair.CompareAndSwap(false, true) {
		return
	}
	if notifier := a.googleLifecycle(); notifier != nil {
		notifier.ParkCurrent(err)
	}
}

func (a *App) googleHistory() GoogleHistoryIngest {
	a.googleLifecycleMu.RLock()
	ingest := a.googleHistoryIngest
	a.googleLifecycleMu.RUnlock()
	return ingest
}
