package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/rs/zerolog"

	"github.com/maxghenis/openmessage/internal/app"
	"github.com/maxghenis/openmessage/internal/db"
	"github.com/maxghenis/openmessage/internal/freshness"
	"github.com/maxghenis/openmessage/internal/localapi"
)

// statusDeps is what runStatus reads besides the store, so tests can pin the
// clock and output writer.
type statusDeps struct {
	daemon *localapi.Client
	now    func() time.Time
	demo   bool
	output io.Writer
}

// RunStatus handles "openmessage status [--json]".
//
// It reports stored message coverage per platform — counts and the latest
// sent/received timestamps — straight from the local store, without starting
// any live transports. Use it to spot a platform that has silently stopped
// syncing: its row falls behind the others and is flagged "Nd behind".
func RunStatus(logger zerolog.Logger, args ...string) error {
	session, err := openCommandReadSource(logger, os.Stderr)
	if err != nil {
		return err
	}
	defer session.Close()

	return runStatus(context.Background(), session, statusDeps{
		daemon: localapi.NewClient("", localapi.LoadControlToken(session.DataDir)),
		now:    time.Now,
		demo:   app.DemoMode(),
		output: os.Stdout,
	}, hasFlag(args, "--json"))
}

func runStatus(ctx context.Context, session *commandReadSession, deps statusDeps, asJSON bool) error {
	stats, err := session.Reads.PlatformStats()
	if err != nil {
		return fmt.Errorf("platform stats: %w", err)
	}

	now := time.Now()
	if deps.now != nil {
		now = deps.now()
	}
	var total int
	var newestOverall int64
	for _, st := range stats {
		total += st.Count
		if st.LatestMS > newestOverall {
			newestOverall = st.LatestMS
		}
	}

	dbPath := session.StorePath
	out := deps.output
	if out == nil {
		out = os.Stdout
	}

	// Only the v2 inbox can tell SMS from RCS; a legacy-mode session skips it.
	var smsPath *freshness.SMSPathReport
	if session.V2Store != nil && !deps.demo {
		smsPath = statusSMSPath(ctx, session.V2Store, now)
	}

	if asJSON {
		return writeStatusJSON(out, dbPath, session.DataDir, total, stats, smsPath)
	}

	fmt.Fprintf(out, "OM-TUI store — %s\n", dbPath)
	if len(stats) == 0 {
		fmt.Fprintln(out, "\nNo messages stored yet. Pair and serve, or run an `openmessage import …`.")
		if smsPath != nil {
			fmt.Fprintln(out, smsPathStatusLine(*smsPath))
		}
		return nil
	}
	fmt.Fprintln(out)

	w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "PLATFORM\tMESSAGES\tLATEST\tLAST RECEIVED\tAGE")
	for _, st := range stats {
		age := humanAge(st.LatestMS, now)
		if behind := daysBetween(st.LatestMS, newestOverall); st.LatestMS > 0 && behind >= 3 {
			age += fmt.Sprintf("  ⚠ %dd behind", behind)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			st.Platform, commaInt(st.Count), fmtTS(st.LatestMS), fmtTS(st.LatestRecvMS), age)
	}
	w.Flush()

	fmt.Fprintf(out, "\nTotal: %s messages across %d platform(s).\n", commaInt(total), len(stats))
	if newestOverall > 0 {
		fmt.Fprintf(out, "Newest message overall: %s (%s).\n", fmtTS(newestOverall), humanAge(newestOverall, now))
	}
	if smsPath != nil {
		fmt.Fprintln(out, smsPathStatusLine(*smsPath))
	}
	return nil
}

func writeStatusJSON(
	out io.Writer,
	dbPath, dataDir string,
	total int,
	stats []db.PlatformStat,
	smsPath *freshness.SMSPathReport,
) error {
	type platformJSON struct {
		Platform         string `json:"platform"`
		Count            int    `json:"count"`
		LatestMS         int64  `json:"latest_ms"`
		LatestReceivedMS int64  `json:"latest_received_ms"`
		Latest           string `json:"latest,omitempty"`
		LatestReceived   string `json:"latest_received,omitempty"`
	}
	payload := struct {
		DataDir   string                   `json:"data_dir"`
		DBPath    string                   `json:"db_path"`
		Total     int                      `json:"total_messages"`
		Platforms []platformJSON           `json:"platforms"`
		SMSPath   *freshness.SMSPathReport `json:"google_sms_path,omitempty"`
	}{DataDir: dataDir, DBPath: dbPath, Total: total, SMSPath: smsPath}

	for _, st := range stats {
		pj := platformJSON{
			Platform: st.Platform, Count: st.Count,
			LatestMS: st.LatestMS, LatestReceivedMS: st.LatestRecvMS,
		}
		if st.LatestMS > 0 {
			pj.Latest = time.UnixMilli(st.LatestMS).Format(time.RFC3339)
		}
		if st.LatestRecvMS > 0 {
			pj.LatestReceived = time.UnixMilli(st.LatestRecvMS).Format(time.RFC3339)
		}
		payload.Platforms = append(payload.Platforms, pj)
	}

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

// fmtTS renders a millisecond timestamp in local time, or an em dash if zero.
func fmtTS(ms int64) string {
	if ms == 0 {
		return "—"
	}
	return time.UnixMilli(ms).Format("2006-01-02 15:04")
}

// humanAge renders how long ago a timestamp was, relative to now.
func humanAge(ms int64, now time.Time) string {
	if ms == 0 {
		return "—"
	}
	d := now.Sub(time.UnixMilli(ms))
	if d < 0 {
		d = 0
	}
	switch days := int(d.Hours() / 24); {
	case days <= 0:
		return "today"
	case days == 1:
		return "1d ago"
	default:
		return fmt.Sprintf("%dd ago", days)
	}
}

// daysBetween returns how many whole days `older` trails `newer` (0 if not behind).
func daysBetween(older, newer int64) int {
	if older == 0 || newer <= older {
		return 0
	}
	return int(time.UnixMilli(newer).Sub(time.UnixMilli(older)).Hours() / 24)
}

// commaInt formats an integer with thousands separators (e.g. 506664 → "506,664").
func commaInt(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
		if len(s) > pre {
			b.WriteByte(',')
		}
	}
	for i := pre; i < len(s); i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < len(s) {
			b.WriteByte(',')
		}
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}
