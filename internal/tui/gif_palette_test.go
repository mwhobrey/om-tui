package tui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maxghenis/openmessage/internal/localapi"
)

func TestGIFSearchPaletteFlow(t *testing.T) {
	// Fake daemon answering /api/gifs
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/gifs" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"results": [
					{"id":"1", "title":"Happy Cat", "preview_url":"https://klipy.com/p1.gif", "url":"https://klipy.com/f1.gif"},
					{"id":"2", "title":"Dancing Dog", "preview_url":"https://klipy.com/p2.gif", "url":"https://klipy.com/f2.gif"}
				]
			}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	client := localapi.NewClient(ts.URL, "")
	session := &Session{Client: client}
	m := NewModel(session)
	m.ready = true
	m.activeID = "conv-1"
	m.status.Google.Paired = true
	m.status.Google.Connected = true

	// 1. Usage error when query is empty
	m1, _ := m.runGIFSearch("")
	model1 := m1.(Model)
	if !strings.Contains(model1.err, "usage: >gif") {
		t.Fatalf("expected usage error, got %q", model1.err)
	}

	// 2. Dispatch search with query
	m2, cmd := m.runGIFSearch("cat")
	model2 := m2.(Model)
	if cmd == nil {
		t.Fatal("expected search command")
	}

	// Execute cmd
	msg := cmd()
	resultsMsg, ok := msg.(gifSearchResultsMsg)
	if !ok || len(resultsMsg.results) != 2 {
		t.Fatalf("unexpected search results msg: %+v", msg)
	}

	// 3. Process search results in Model Update
	m3, _ := model2.Update(resultsMsg)
	model3 := m3.(Model)
	if !model3.palette.open {
		t.Fatal("expected palette to be open after search results")
	}
	if model3.palette.mode != "gif" {
		t.Fatalf("expected palette mode 'gif', got %q", model3.palette.mode)
	}
	if len(model3.palette.matches) != 2 {
		t.Fatalf("expected 2 matches in palette, got %d", len(model3.palette.matches))
	}
	if model3.palette.matches[0].Label != "Happy Cat" {
		t.Fatalf("expected 'Happy Cat', got %q", model3.palette.matches[0].Label)
	}

	// 4. Select GIF item to send to active chat
	selectedItem := model3.palette.matches[0]
	m4, sendCmd := model3.runPaletteAction(selectedItem)
	model4 := m4.(Model)
	if sendCmd == nil {
		t.Fatal("expected sendCmd when active chat is connected")
	}
	if !model4.sending {
		t.Fatal("expected model sending flag to be true")
	}

	// 5. Select GIF item without active chat -> informs user
	model3.activeID = ""
	m5, noCmd := model3.runPaletteAction(selectedItem)
	model5 := m5.(Model)
	if noCmd != nil {
		t.Fatal("expected no command when activeID is empty")
	}
	if !strings.Contains(model5.info, "Open a conversation") {
		t.Fatalf("expected open conversation info message, got %q", model5.info)
	}

	// 6. Select GIF item when chat is open but offline -> inserts/appends into composer
	model3.activeID = "conv-1"
	model3.status.Google.Connected = false
	model3.compose.SetValue("Check this out:")
	m6, focusCmd := model3.runPaletteAction(selectedItem)
	model6 := m6.(Model)
	if focusCmd == nil {
		t.Fatal("expected focus command")
	}
	if model6.compose.Value() != "Check this out: https://klipy.com/f1.gif" {
		t.Fatalf("expected composer to have appended GIF URL, got %q", model6.compose.Value())
	}

	// 7. Select GIF item with replyTarget -> clears replyTarget and sends
	model3.status.Google.Connected = true
	model3.replyTarget = &localapi.Message{MessageID: "target-123", SenderName: "Alice"}
	m7, sendReplyCmd := model3.runPaletteAction(selectedItem)
	model7 := m7.(Model)
	if sendReplyCmd == nil {
		t.Fatal("expected send command with reply target")
	}
	if model7.replyTarget != nil {
		t.Fatal("expected reply target to be cleared after sending GIF")
	}
}
