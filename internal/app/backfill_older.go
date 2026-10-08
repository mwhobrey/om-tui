package app

import (
	"fmt"
	"sync"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

const (
	olderFetchPageSize    = 50
	olderFetchMaxPages    = 40  // ~2000 messages walked per call before yielding
	olderFetchDefaultWant = 100 // messages older than the cutoff to gather per call
	olderFetchMaxWant     = 500
)

// OlderHistoryResult reports one phone-side older-history fetch.
type OlderHistoryResult struct {
	// Fetched is how many messages older than the requested cutoff were pulled
	// from the phone and handed to the store. Zero with Exhausted=false means
	// the walk hit its page cap before reaching the cutoff; call again.
	Fetched int `json:"fetched"`
	// Exhausted is true once the phone has no more history for the conversation.
	Exhausted bool `json:"exhausted"`
}

// olderHistoryState remembers, per Google conversation, where the last
// phone-side walk stopped. The cursor Google hands back only moves toward
// older messages, so continuing from it avoids re-walking from the newest
// message on every scroll.
type olderHistoryState struct {
	mu    sync.Mutex // held for a whole fetch; contenders fail fast
	state sync.Map   // conversationID -> *olderCursor
}

type olderCursor struct {
	cursor *gmproto.Cursor
	done   bool
}

// FetchOlderGoogleHistory pulls messages older than beforeMS for one Google
// Messages conversation from the phone and writes them through the same
// storeMessage path as backfill (legacy store plus V2 ingest). The caller then
// re-reads the store; nothing is returned but counts.
//
// It walks from the saved cursor (or the newest message, the first time) until
// it has gathered `want` messages older than beforeMS, the phone runs out, or
// the page cap is reached.
func (a *App) FetchOlderGoogleHistory(conversationID string, beforeMS int64, want int) (OlderHistoryResult, error) {
	var res OlderHistoryResult
	if conversationID == "" || beforeMS <= 0 {
		return res, fmt.Errorf("conversation id and a positive cutoff are required")
	}
	if want <= 0 {
		want = olderFetchDefaultWant
	}
	if want > olderFetchMaxWant {
		want = olderFetchMaxWant
	}

	gm, token := a.currentBackfillClient()
	if gm == nil {
		return res, fmt.Errorf("client not connected")
	}
	if a.backfillRunning.Load() {
		return res, fmt.Errorf("a sync is already running — try again in a moment")
	}
	if !a.older.mu.TryLock() {
		return res, fmt.Errorf("an older-history fetch is already running")
	}
	defer a.older.mu.Unlock()

	cur := &olderCursor{}
	if v, ok := a.older.state.Load(conversationID); ok {
		cur = v.(*olderCursor)
	}
	if cur.done {
		res.Exhausted = true
		return res, nil
	}

	cursor := cur.cursor
	for page := 0; page < olderFetchMaxPages; page++ {
		if !a.backfillClientStillCurrent(token) {
			return res, fmt.Errorf("Google Messages client changed during fetch")
		}
		resp, err := gm.FetchMessages(conversationID, olderFetchPageSize, cursor)
		if err != nil {
			a.HandleGoogleAuthExpiredError(err)
			// The saved cursor may be what the server rejected; restart next time.
			a.older.state.Delete(conversationID)
			return res, fmt.Errorf("fetch older messages: %w", err)
		}
		msgs := resp.GetMessages()
		for _, msg := range msgs {
			a.storeMessage(msg)
			if msg.GetTimestamp()/1000 < beforeMS {
				res.Fetched++
			}
		}
		cursor = resp.GetCursor()
		if len(msgs) == 0 || cursor == nil {
			res.Exhausted = true
			break
		}
		if res.Fetched >= want {
			break
		}
	}

	a.older.state.Store(conversationID, &olderCursor{cursor: cursor, done: res.Exhausted})
	if res.Fetched > 0 {
		a.emitMessagesChange(conversationID)
	}
	return res, nil
}
