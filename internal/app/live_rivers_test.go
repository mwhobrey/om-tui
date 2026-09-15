package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/maxghenis/openmessage/internal/db"
	"github.com/maxghenis/openmessage/internal/river"
)

func TestCreateLiveRiverAllocatesIDsAndSessionDirs(t *testing.T) {
	t.Setenv("OPENMESSAGES_DATA_DIR", t.TempDir())
	t.Setenv("OPENMESSAGES_VAULT_INSECURE", "1")
	t.Setenv("OPENMESSAGES_DEMO", "")

	a, err := New(zerolog.Nop())
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	defer a.Close()

	wa, err := a.CreateLiveRiver(river.ProviderWhatsApp, "")
	if err != nil {
		t.Fatalf("CreateLiveRiver(whatsapp): %v", err)
	}
	if wa.ID != "whatsapp-2" || wa.Provider != river.ProviderWhatsApp {
		t.Fatalf("whatsapp extra = %+v", wa)
	}
	wa2, err := a.CreateLiveRiver(river.ProviderWhatsApp, "")
	if err != nil {
		t.Fatalf("CreateLiveRiver(whatsapp 2): %v", err)
	}
	if wa2.ID != "whatsapp-3" {
		t.Fatalf("second extra whatsapp = %q", wa2.ID)
	}
	sig, err := a.CreateLiveRiver(river.ProviderSignal, "")
	if err != nil {
		t.Fatalf("CreateLiveRiver(signal): %v", err)
	}
	if sig.ID != "signal-2" {
		t.Fatalf("signal extra = %q", sig.ID)
	}
	if _, err := a.CreateLiveRiver(river.ProviderMessages, ""); err == nil {
		t.Fatal("expected extra Messages river to be rejected")
	}
	if _, err := a.CreateLiveRiver(river.ProviderSlack, ""); err == nil {
		t.Fatal("expected slack extra river to be rejected")
	}
	if err := a.Store.UpsertRiver(&db.River{
		ID:          "messages-2",
		Provider:    river.ProviderMessages,
		DisplayName: "Messages 2",
		AccountKey:  "messages-2",
		Status:      river.StatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	listed, err := a.ListRiversWithUnread()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range listed {
		if r.ID == "messages-2" {
			t.Fatal("extra Messages rivers must not appear in the river list")
		}
	}

	sessionDir := river.SessionDir(a.DataDir, wa.ID)
	if st, err := os.Stat(sessionDir); err != nil || !st.IsDir() {
		t.Fatalf("session dir %q: %v", sessionDir, err)
	}
	if got := a.whatsAppSessionPathFor(wa.ID); got != filepath.Join(sessionDir, "whatsapp-session.db") {
		t.Fatalf("extra wa session = %q", got)
	}
	if got := a.whatsAppSessionPathFor(river.DefaultWhatsAppRiverID); got != a.WhatsAppSessionPath {
		t.Fatalf("default wa session = %q, want %q", got, a.WhatsAppSessionPath)
	}
	if got := a.signalConfigPathFor(sig.ID); !strings.HasSuffix(got, filepath.Join("rivers", "signal-2", "signal-cli")) {
		t.Fatalf("extra signal config = %q", got)
	}
}

func TestLiveRiverIDForConversation(t *testing.T) {
	a := &App{}
	if got := a.liveRiverIDForConversation("whatsapp/whatsapp-2/1555@s.whatsapp.net"); got != "whatsapp-2" {
		t.Fatalf("scoped wa = %q", got)
	}
	if got := a.liveRiverIDForConversation("whatsapp:1555@s.whatsapp.net"); got != river.DefaultWhatsAppRiverID {
		t.Fatalf("default wa = %q", got)
	}
	if got := a.liveRiverIDForConversation("signal-group/signal-2/abc"); got != "signal-2" {
		t.Fatalf("scoped group = %q", got)
	}
	if got := a.liveRiverIDForConversation("signal:+15551212"); got != river.DefaultSignalRiverID {
		t.Fatalf("default signal = %q", got)
	}
}
