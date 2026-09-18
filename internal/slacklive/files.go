package slacklive

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/maxghenis/openmessage/internal/db"
	"github.com/maxghenis/openmessage/internal/river"
	"github.com/slack-go/slack"
)

// IngressFile is one Slack file attached to a captured message.
type IngressFile struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	MIME string `json:"mime,omitempty"`
	Size int64  `json:"size,omitempty"`
}

// DownloadOpaqueV1 is packed into message_attachments.remote_ref.
type DownloadOpaqueV1 struct {
	V      int    `json:"v"`
	FileID string `json:"file_id"`
}

// DownloadedFile is one Slack private-file fetch.
type DownloadedFile struct {
	Data     []byte
	Filename string
	MIME     string
}

func MarshalDownloadOpaque(fileID string) ([]byte, error) {
	fileID = strings.TrimSpace(fileID)
	if fileID == "" {
		return nil, fmt.Errorf("slack download opaque: file_id is empty")
	}
	return json.Marshal(DownloadOpaqueV1{V: 1, FileID: fileID})
}

func UnmarshalDownloadOpaque(raw []byte) (DownloadOpaqueV1, error) {
	var payload DownloadOpaqueV1
	if !utf8.Valid(raw) {
		return payload, fmt.Errorf("slack download opaque is not valid UTF-8")
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return payload, fmt.Errorf("decode slack download opaque: %w", err)
	}
	if payload.V != 1 {
		return payload, fmt.Errorf("unsupported slack download opaque version %d", payload.V)
	}
	payload.FileID = strings.TrimSpace(payload.FileID)
	if payload.FileID == "" {
		return payload, fmt.Errorf("slack download opaque: file_id is empty")
	}
	return payload, nil
}

func (c *Client) DownloadFile(ctx context.Context, fileID string) (DownloadedFile, error) {
	if c == nil || c.api == nil {
		return DownloadedFile{}, fmt.Errorf("slack client is not connected")
	}
	if ctx == nil {
		return DownloadedFile{}, fmt.Errorf("slack file download context is nil")
	}
	if err := ctx.Err(); err != nil {
		return DownloadedFile{}, err
	}
	fileID = strings.TrimSpace(fileID)
	if fileID == "" {
		return DownloadedFile{}, fmt.Errorf("slack file id is empty")
	}
	info, _, _, err := c.api.GetFileInfoContext(ctx, fileID, 0, 0)
	if err != nil {
		return DownloadedFile{}, fmt.Errorf("files.info: %w", err)
	}
	if info == nil {
		return DownloadedFile{}, fmt.Errorf("files.info: transport returned no file")
	}
	url := strings.TrimSpace(info.URLPrivateDownload)
	if url == "" {
		url = strings.TrimSpace(info.URLPrivate)
	}
	if url == "" {
		return DownloadedFile{}, fmt.Errorf("files.info: file %q has no private download url", fileID)
	}
	var buf bytes.Buffer
	if err := c.api.GetFileContext(ctx, url, &buf); err != nil {
		return DownloadedFile{}, fmt.Errorf("files.download: %w", err)
	}
	return DownloadedFile{
		Data:     buf.Bytes(),
		Filename: strings.TrimSpace(info.Name),
		MIME:     strings.TrimSpace(info.Mimetype),
	}, nil
}

// SendFile uploads bytes via files.getUploadURLExternal + UploadToURL +
// files.completeUploadExternal, then emits ingress so PRIMARY can download.
func (c *Client) SendFile(ctx context.Context, conversationID, filename, mime, caption, replyToID string, data []byte) (*db.Message, error) {
	if c == nil || c.api == nil {
		return nil, fmt.Errorf("slack client is not connected")
	}
	if ctx == nil {
		return nil, fmt.Errorf("slack file send context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, channelID, ok := river.ParseSlackConversationID(conversationID)
	if !ok {
		return nil, fmt.Errorf("not a slack conversation id")
	}
	filename = strings.TrimSpace(filename)
	if filename == "" {
		filename = "upload"
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("empty file")
	}
	if int64(len(data)) > math.MaxInt {
		return nil, fmt.Errorf("file too large")
	}
	upload, err := c.api.GetUploadURLExternalContext(ctx, slack.GetUploadURLExternalParameters{
		FileName: filename,
		FileSize: len(data),
	})
	if err != nil {
		return nil, fmt.Errorf("files.getUploadURLExternal: %w", err)
	}
	if upload == nil || strings.TrimSpace(upload.UploadURL) == "" || strings.TrimSpace(upload.FileID) == "" {
		return nil, fmt.Errorf("files.getUploadURLExternal: missing upload url or file id")
	}
	if err := c.api.UploadToURL(ctx, slack.UploadToURLParameters{
		UploadURL: upload.UploadURL,
		Reader:    bytes.NewReader(data),
		Filename:  filename,
	}); err != nil {
		return nil, fmt.Errorf("files.upload: %w", err)
	}
	caption = strings.TrimSpace(caption)
	threadTS := slackMessageTS(replyToID)
	complete, err := c.api.CompleteUploadExternalContext(ctx, slack.CompleteUploadExternalParameters{
		Files: []slack.FileSummary{{
			ID:    upload.FileID,
			Title: filename,
		}},
		Channel:         channelID,
		InitialComment:  caption,
		ThreadTimestamp: threadTS,
	})
	if err != nil {
		return nil, fmt.Errorf("files.completeUploadExternal: %w", err)
	}
	fileID := upload.FileID
	if complete != nil && len(complete.Files) > 0 && strings.TrimSpace(complete.Files[0].ID) != "" {
		fileID = strings.TrimSpace(complete.Files[0].ID)
	}
	info, _, _, infoErr := c.api.GetFileInfoContext(ctx, fileID, 0, 0)
	if infoErr != nil {
		return nil, fmt.Errorf("files.info: %w", infoErr)
	}
	ts := slackShareTS(info, channelID)
	if ts == "" {
		return nil, fmt.Errorf("files.info: share timestamp missing for %s in %s", fileID, channelID)
	}
	if mime == "" && info != nil {
		mime = strings.TrimSpace(info.Mimetype)
	}
	body := caption
	if body == "" {
		body = "[file]"
	}
	msg := &db.Message{
		MessageID:      fmt.Sprintf("slack:%s:%s", channelID, ts),
		ConversationID: conversationID,
		Body:           body,
		TimestampMS:    slackTSToMS(ts),
		Status:         "OUTGOING_COMPLETE",
		IsFromMe:       true,
		ReplyToID:      replyToID,
		SourcePlatform: "slack",
		SourceID:       channelID + ":" + ts,
	}
	c.emitIngress(IngressFrame{
		Kind:        "message",
		ChannelID:   channelID,
		ChannelName: c.channelTitle(channelID),
		ChannelKind: slackChannelKind(channelID),
		TS:          ts,
		ThreadTS:    threadTS,
		UserID:      c.userID,
		UserName:    "you",
		Body:        body,
		IsFromMe:    true,
		TimestampMS: msg.TimestampMS,
		Files: []IngressFile{{
			ID:   fileID,
			Name: filename,
			MIME: strings.TrimSpace(mime),
			Size: int64(len(data)),
		}},
	})
	return msg, nil
}

func slackShareTS(file *slack.File, channelID string) string {
	if file == nil {
		return ""
	}
	channelID = strings.TrimSpace(channelID)
	pick := func(shares map[string][]slack.ShareFileInfo) string {
		if len(shares) == 0 {
			return ""
		}
		for _, info := range shares[channelID] {
			if ts := strings.TrimSpace(info.Ts); ts != "" {
				return ts
			}
		}
		for _, infos := range shares {
			for _, info := range infos {
				if ts := strings.TrimSpace(info.Ts); ts != "" {
					return ts
				}
			}
		}
		return ""
	}
	if ts := pick(file.Shares.Public); ts != "" {
		return ts
	}
	return pick(file.Shares.Private)
}

func ingressFiles(files []slack.File) []IngressFile {
	if len(files) == 0 {
		return nil
	}
	out := make([]IngressFile, 0, len(files))
	for _, file := range files {
		id := strings.TrimSpace(file.ID)
		if id == "" || file.Mode == "tombstone" {
			continue
		}
		out = append(out, IngressFile{
			ID:   id,
			Name: strings.TrimSpace(file.Name),
			MIME: strings.TrimSpace(file.Mimetype),
			Size: int64(file.Size),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
