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

	// 5. Select GIF item without active chat -> pastes to composer
	model3.activeID = ""
	m5, focusCmd := model3.runPaletteAction(selectedItem)
	model5 := m5.(Model)
	if focusCmd == nil {
		t.Fatal("expected focus command")
	}
	if model5.compose.Value() != "https://klipy.com/f1.gif" {
		t.Fatalf("expected composer to have GIF URL, got %q", model5.compose.Value())
	}
}
