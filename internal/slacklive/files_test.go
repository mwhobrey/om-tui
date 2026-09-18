package slacklive

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

func TestDownloadOpaqueRoundTrip(t *testing.T) {
	t.Parallel()
	raw, err := MarshalDownloadOpaque(" F123 ")
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalDownloadOpaque(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.V != 1 || got.FileID != "F123" {
		t.Fatalf("opaque = %+v", got)
	}
	if _, err := MarshalDownloadOpaque(" "); err == nil {
		t.Fatal("empty file id should fail")
	}
	if _, err := UnmarshalDownloadOpaque([]byte(`{"v":2,"file_id":"F1"}`)); err == nil {
		t.Fatal("unsupported version should fail")
	}
}

func TestSendFileUploadsCompletesAndEmitsIngress(t *testing.T) {
	api := &fakeSlackAPI{
		uploadFileID: "FUP1",
		fileInfo: &slack.File{
			ID:       "FUP1",
			Mimetype: "image/png",
			Shares: slack.Share{
				Public: map[string][]slack.ShareFileInfo{
					"C1": {{Ts: "1700000001.000100"}},
				},
			},
		},
	}
	client, _ := newTestClient(t, api)
	var frames []IngressFrame
	client.SetIngress(func(frame IngressFrame) { frames = append(frames, frame) })

	msg, err := client.SendFile(
		context.Background(),
		"slack:T1:C1",
		"photo.png",
		"image/png",
		"look",
		"slack:C1:1700000000.000001",
		[]byte("png-bytes"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(api.uploaded) != "png-bytes" || api.uploadedName != "photo.png" {
		t.Fatalf("uploaded name=%q bytes=%q", api.uploadedName, api.uploaded)
	}
	if api.completeParams.Channel != "C1" || api.completeParams.InitialComment != "look" || api.completeParams.ThreadTimestamp != "1700000000.000001" {
		t.Fatalf("complete = %+v", api.completeParams)
	}
	if len(api.completeParams.Files) != 1 || api.completeParams.Files[0].ID != "FUP1" {
		t.Fatalf("complete files = %+v", api.completeParams.Files)
	}
	if msg.MessageID != "slack:C1:1700000001.000100" || msg.Body != "look" || msg.SourceID != "C1:1700000001.000100" {
		t.Fatalf("message = %+v", msg)
	}
	if len(frames) != 1 {
		t.Fatalf("frames = %d", len(frames))
	}
	if frames[0].TS != "1700000001.000100" || frames[0].ThreadTS != "1700000000.000001" || frames[0].Body != "look" {
		t.Fatalf("frame = %+v", frames[0])
	}
	if len(frames[0].Files) != 1 || frames[0].Files[0].ID != "FUP1" || frames[0].Files[0].Size != 9 {
		t.Fatalf("files = %+v", frames[0].Files)
	}
}

func TestSendFileEmptyAndMissingShareAreErrors(t *testing.T) {
	client, _ := newTestClient(t, &fakeSlackAPI{})
	if _, err := client.SendFile(context.Background(), "slack:T1:C1", "a.png", "image/png", "", "", nil); err == nil {
		t.Fatal("empty file should fail")
	}
	if _, err := client.SendFile(context.Background(), "not-slack", "a.png", "image/png", "", "", []byte("x")); err == nil {
		t.Fatal("bad conversation id should fail")
	}

	api := &fakeSlackAPI{fileInfo: &slack.File{ID: "FUP1"}}
	client, _ = newTestClient(t, api)
	_, err := client.SendFile(context.Background(), "slack:T1:C1", "a.png", "image/png", "", "", []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "share timestamp missing") {
		t.Fatalf("err = %v", err)
	}
}

func TestSendFileTransportError(t *testing.T) {
	api := &fakeSlackAPI{getURLErr: errors.New("missing_scope")}
	client, _ := newTestClient(t, api)
	_, err := client.SendFile(context.Background(), "slack:T1:C1", "a.png", "image/png", "", "", []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "files.getUploadURLExternal") {
		t.Fatalf("err = %v", err)
	}
}
