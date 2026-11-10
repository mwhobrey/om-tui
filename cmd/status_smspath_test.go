package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-gmessages/pkg/libgm"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"

	"github.com/maxghenis/openmessage/internal/freshness"
	"github.com/maxghenis/openmessage/internal/ingest"
	"github.com/maxghenis/openmessage/internal/localapi"
	"github.com/maxghenis/openmessage/internal/storage/sqlite"
)

type statusV2Account struct {
	id, bridgeKey, codec string
}

var (
	statusGoogleAccount = statusV2Account{"google-primary", "google_messages", ingest.GoogleCodec}
)

type statusV2Seed struct {
	account     statusV2Account
	lastMessage time.Time
	frames      []time.Time
}

func seedStatusV2Store(t *testing.T, dataDir string, seeds ...statusV2Seed) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dataDir, "v2"), 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(filepath.Join(dataDir, "v2", "store.sqlite3"))
	if err != nil {
		t.Fatalf("sqlite.Open(): %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	for _, seed := range seeds {
		createdMS := seed.lastMessage.Add(-30 * 24 * time.Hour).UnixMilli()
		if err := store.UpsertAccount(sqlite.Account{
			AccountID: seed.account.id, BridgeKey: seed.account.bridgeKey, DisplayName: seed.account.id,
			Mode: sqlite.AccountModeLive, Enabled: true, ConfigJSON: "{}",
			CreatedAtMS: createdMS, UpdatedAtMS: createdMS,
		}); err != nil {
			t.Fatalf("UpsertAccount(%s): %v", seed.account.id, err)
		}
		conversationID := seed.account.id + "-conversation"
		if err := store.UpsertConversation(sqlite.Conversation{
			ConversationID: conversationID, AccountID: seed.account.id,
			RemoteConversationID: "remote-" + conversationID, Kind: sqlite.ConversationKindDirect,
			Title: "Thread", NotificationMode: sqlite.NotificationModeAll, MetadataJSON: "{}",
			CreatedAtMS: createdMS, UpdatedAtMS: createdMS,
		}); err != nil {
			t.Fatalf("UpsertConversation(%s): %v", conversationID, err)
		}
		var clock time.Time
		repository, err := sqlite.NewMessageRepository(store, func() time.Time { return clock })
		if err != nil {
			t.Fatal(err)
		}
		clock = seed.lastMessage
		if err := repository.ImportMessage(ctx, sqlite.MessageProjection{Message: sqlite.Message{
			MessageID: seed.account.id + "-message", ConversationID: conversationID, AccountID: seed.account.id,
			RemoteMessageID: "remote-message", Direction: sqlite.MessageDirectionIncoming, Body: "hi",
			State: sqlite.MessageStateActive, OccurredAtMS: seed.lastMessage.UnixMilli(),
		}}); err != nil {
			t.Fatalf("ImportMessage(%s): %v", seed.account.id, err)
		}
		for i, at := range seed.frames {
			clock = at
			if _, err := repository.AppendInbox(ctx, sqlite.InboxRecord{
				InboxID: fmt.Sprintf("%s-frame-%d", seed.account.id, i), AccountID: seed.account.id,
				Generation: 1, DedupeKey: fmt.Sprintf("frame-%d", i),
				Codec: seed.account.codec, CodecVersion: 1, Payload: []byte("{}"),
			}); err != nil {
				t.Fatalf("AppendInbox(%s #%d): %v", seed.account.id, i, err)
			}
		}
	}
}

func setStatusEnv(t *testing.T, dataDir string, v2 bool) {
	t.Helper()
	t.Setenv("OPENMESSAGES_DATA_DIR", dataDir)
	t.Setenv("OPENMESSAGES_DEMO", "0")
	t.Setenv("OPENMESSAGES_APP_SANDBOX", "1")
	if v2 {
		t.Setenv("OPENMESSAGES_V2_PRIMARY", "1")
		t.Setenv("OPENMESSAGES_V2_SEND", "")
		t.Setenv("OPENMESSAGES_V2_INGEST", "")
	} else {
		t.Setenv("OPENMESSAGES_V2_PRIMARY", "0")
		t.Setenv("OPENMESSAGES_V2_SEND", "0")
		t.Setenv("OPENMESSAGES_V2_INGEST", "0")
	}
}

func openStatusSession(t *testing.T, dataDir string, v2 bool) *commandReadSession {
	t.Helper()
	setStatusEnv(t, dataDir, v2)
	session, err := openCommandReadSource(zerolog.Nop(), io.Discard)
	if err != nil {
		t.Fatalf("openCommandReadSource(): %v", err)
	}
	t.Cleanup(session.Close)
	return session
}

func downDaemon(t *testing.T) *localapi.Client {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	return localapi.NewClient("http://"+addr, "")
}

func runStatusOutput(t *testing.T, session *commandReadSession, deps statusDeps, asJSON bool) string {
	t.Helper()
	var buf strings.Builder
	deps.output = &buf
	if err := runStatus(context.Background(), session, deps, asJSON); err != nil {
		t.Fatalf("runStatus(json=%v): %v", asJSON, err)
	}
	return buf.String()
}

func openSMSPathTestStore(t *testing.T) (*sqlite.Store, func(at time.Time, id string, messageType int64)) {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "store.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, smsPathReceiver(t, store)
}

// smsPathReceiver returns a func that appends an incoming Google message frame
// of the given protobuf type (field 11) to store's inbox, received at its time.
func smsPathReceiver(t *testing.T, store *sqlite.Store) func(at time.Time, id string, messageType int64) {
	t.Helper()
	nowMS := time.Now().UnixMilli()
	if err := store.UpsertAccount(sqlite.Account{
		AccountID: "google-primary", BridgeKey: "google_messages", Mode: sqlite.AccountModeLive,
		Enabled: true, ConfigJSON: "{}", CreatedAtMS: nowMS, UpdatedAtMS: nowMS,
	}); err != nil {
		t.Fatal(err)
	}
	var clock time.Time
	repository, err := sqlite.NewMessageRepository(store, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	receive := func(at time.Time, id string, messageType int64) {
		t.Helper()
		payload, _, err := ingest.MarshalGoogleMessageFrame(&libgm.WrappedMessage{Message: &gmproto.Message{
			MessageID: id, ConversationID: "c", Type: messageType, Timestamp: at.UnixMicro(),
			MessageStatus: &gmproto.MessageStatus{Status: gmproto.MessageStatusType_INCOMING_COMPLETE},
			MessageInfo: []*gmproto.MessageInfo{{Data: &gmproto.MessageInfo_MessageContent{
				MessageContent: &gmproto.MessageContent{Content: "hello"},
			}}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		clock = at
		if _, err := repository.AppendInbox(context.Background(), sqlite.InboxRecord{
			InboxID: id, AccountID: "google-primary", Generation: 1, DedupeKey: "msg:" + id,
			Codec: ingest.GoogleCodec, CodecVersion: int64(ingest.GoogleCodecVersion), Payload: payload,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return receive
}

func TestStatusSMSPathFlagsSMSSilenceWhileRCSFlows(t *testing.T) {
	now := time.Now().Truncate(time.Minute)
	store, receive := openSMSPathTestStore(t)
	lastSMS := now.Add(-50 * time.Hour)
	for i := 0; i < 30*3; i++ { // three a day, eight hours apart, for 30 days
		receive(lastSMS.Add(-time.Duration(i)*8*time.Hour), fmt.Sprintf("sms-%d", i), 1)
	}
	for i := 0; i < 49; i++ {
		receive(lastSMS.Add(time.Duration(i+1)*time.Hour), fmt.Sprintf("rcs-%d", i), 4)
	}

	report := statusSMSPath(context.Background(), store, now)
	if report == nil || !report.Stalled || report.LastSMSMS != lastSMS.UnixMilli() {
		t.Fatalf("report = %+v, want stalled since %d", report, lastSMS.UnixMilli())
	}
	line := smsPathStatusLine(*report)
	for _, want := range []string{"⚠ Google SMS", "(2d)", "usual 3.0 arrivals a day", "about 6 would have come",
		"in the last 24h", "try restarting the phone"} {
		if !strings.Contains(line, want) {
			t.Fatalf("line %q lacks %q", line, want)
		}
	}
}

// runStatus writes the SMS-path line to its own output and --json carries the
// same verdict as google_sms_path; demo mode, like silence, judges nothing.
func TestRunStatusReportsTheSMSPath(t *testing.T) {
	now := time.Now().Truncate(time.Minute)
	lastSMS := now.Add(-50 * time.Hour)
	dataDir := t.TempDir()
	seedStatusV2Store(t, dataDir, statusV2Seed{account: statusGoogleAccount, lastMessage: lastSMS})
	store, err := sqlite.Open(filepath.Join(dataDir, "v2", "store.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	receive := smsPathReceiver(t, store)
	for i := 0; i < 30*3; i++ { // three a day, eight hours apart, for 30 days
		receive(lastSMS.Add(-time.Duration(i)*8*time.Hour), fmt.Sprintf("sms-%d", i), 1)
	}
	for i := 0; i < 49; i++ {
		receive(lastSMS.Add(time.Duration(i+1)*time.Hour), fmt.Sprintf("rcs-%d", i), 4)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	session := openStatusSession(t, dataDir, true)
	deps := statusDeps{daemon: downDaemon(t), now: func() time.Time { return now }}

	text := runStatusOutput(t, session, deps, false)
	line := "\n⚠ Google SMS: no incoming SMS since " + fmtTS(lastSMS.UnixMilli())
	if !strings.Contains(text, line) {
		t.Fatalf("status output lacks the stalled SMS-path line:\n%s", text)
	}
	// It is the last line, after the table.
	if !strings.HasSuffix(strings.TrimSpace(text), "try restarting the phone.") {
		t.Fatalf("the SMS-path line should close the output:\n%s", text)
	}
	// It reads the inbox under the caller's context: a cancelled one yields
	// no verdict rather than a judgment of nothing.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	var buffered strings.Builder
	cancelledDeps := deps
	cancelledDeps.output = &buffered
	if err := runStatus(cancelled, session, cancelledDeps, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buffered.String(), "Google SMS") {
		t.Fatalf("a cancelled status still judged the SMS path:\n%s", buffered.String())
	}
	var status struct {
		SMSPath *freshness.SMSPathReport `json:"google_sms_path"`
	}
	raw := runStatusOutput(t, session, deps, true)
	if err := json.Unmarshal([]byte(raw), &status); err != nil {
		t.Fatalf("decode status --json: %v\n%s", err, raw)
	}
	if status.SMSPath == nil || !status.SMSPath.Stalled || status.SMSPath.LastSMSMS != lastSMS.UnixMilli() {
		t.Fatalf("google_sms_path = %+v, want stalled since %d", status.SMSPath, lastSMS.UnixMilli())
	}

	deps.demo = true
	if text := runStatusOutput(t, session, deps, false); strings.Contains(text, "Google SMS") {
		t.Fatalf("demo status judged the SMS path:\n%s", text)
	}
	if raw := runStatusOutput(t, session, deps, true); strings.Contains(raw, "google_sms_path") {
		t.Fatalf("demo status --json carries google_sms_path:\n%s", raw)
	}
}

// The inbox can hold SMS and RCS frames that never became stored messages;
// the text output then still says what --json reports.
func TestRunStatusReportsTheSMSPathWithNoStoredMessages(t *testing.T) {
	now := time.Now().Truncate(time.Minute)
	lastSMS := now.Add(-50 * time.Hour)
	dataDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDir, "v2"), 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(filepath.Join(dataDir, "v2", "store.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	receive := smsPathReceiver(t, store)
	for i := 0; i < 30*3; i++ {
		receive(lastSMS.Add(-time.Duration(i)*8*time.Hour), fmt.Sprintf("sms-%d", i), 1)
	}
	for i := 0; i < 49; i++ {
		receive(lastSMS.Add(time.Duration(i+1)*time.Hour), fmt.Sprintf("rcs-%d", i), 4)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	session := openStatusSession(t, dataDir, true)
	text := runStatusOutput(t, session, statusDeps{daemon: downDaemon(t), now: func() time.Time { return now }}, false)
	if !strings.Contains(text, "No messages stored yet") || !strings.Contains(text, "⚠ Google SMS: no incoming SMS since") {
		t.Fatalf("empty store output:\n%s", text)
	}
}

func TestSMSPathStatusLineCoversEveryReason(t *testing.T) {
	cfg := freshness.DefaultSMSPathConfig
	base := freshness.NewSMSPathReport(freshness.SMSPathVerdict{
		LastSMS:          time.Date(2026, 10, 3, 16, 16, 0, 0, time.Local),
		Silence:          61 * time.Hour,
		ArrivalsPerDay:   5.8,
		ExpectedArrivals: 14.7,
		BaselineSpan:     27 * 24 * time.Hour,
		RegularSpan:      20 * 24 * time.Hour,
		LongGaps:         3,
		BaselineArrivals: 40,
	}, cfg)
	cases := map[string]string{
		freshness.SMSPathStalled:           "try restarting the phone",
		freshness.SMSPathUsualPace:         "within this phone's usual pace (5.8 arrivals a day)",
		freshness.SMSPathRCSQuiet:          "not an SMS-only failure",
		freshness.SMSPathHistoryStale:      "could not be reloaded",
		freshness.SMSPathFlowing:           "last incoming SMS 2026-10-03 16:16 (2d)",
		freshness.SMSPathThinBaseline:      "in the 28d before the last SMS it went a whole 24h without one 3 time(s) (allowed 1) and kept its usual rhythm for 20d (needs 21d), with 40 separate arrivals (needs 10)",
		freshness.SMSPathNoHistory:         "no incoming SMS in the Google inbox",
		freshness.SMSPathGoogleUnreachable: "no incoming SMS in the Google inbox",
	}
	for reason, want := range cases {
		report := base
		report.Reason = reason
		if line := smsPathStatusLine(report); !strings.Contains(line, want) {
			t.Errorf("%s: line %q lacks %q", reason, line, want)
		}
	}
}

func TestGoogleSMSPathMonitorNeedsTheV2StackAndIngest(t *testing.T) {
	if googleSMSPathMonitor(nil, true) != nil {
		t.Fatal("monitor without a v2 stack")
	}
	store, receive := openSMSPathTestStore(t)
	stack := &v2Stack{Store: store}
	if googleSMSPathMonitor(stack, false) != nil {
		t.Fatal("monitor without v2 ingest, whose inbox would hold only stale frames")
	}
	now := time.Now()
	receive(now.Add(-time.Hour), "sms", 1)
	report, ok := googleSMSPathMonitor(stack, true).Report(context.Background(), now)
	// The inbox stores receipt in milliseconds, which caps the timestamp.
	if !ok || report.LastSMSMS != now.Add(-time.Hour).UnixMilli() || report.Frames != 1 {
		t.Fatalf("report = %+v ok = %v, want the inbox SMS", report, ok)
	}
}
