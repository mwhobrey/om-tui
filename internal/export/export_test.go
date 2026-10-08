package export

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"gopkg.in/yaml.v3"
)

var sample = []Message{
	{MessageID: "b", TimestampMS: 2000, Sender: "Alice", Body: "line1\nline2, \"quoted\""},
	{MessageID: "a", TimestampMS: 1000, Sender: "me", IsFromMe: true, Body: "hi"},
	{MessageID: "a", TimestampMS: 1000, Sender: "me", IsFromMe: true, Body: "hi"}, // dup
}

func TestSortChronologicalDedupes(t *testing.T) {
	got := SortChronological(sample)
	if len(got) != 2 || got[0].MessageID != "a" || got[1].MessageID != "b" {
		t.Fatalf("got %+v", got)
	}
}

func TestWriteFormatsRoundTrip(t *testing.T) {
	msgs := SortChronological(sample)
	conv := Conversation{ID: "c1", Name: "Alice", Platform: "sms"}

	var jb bytes.Buffer
	if err := Write(&jb, JSON, conv, Range{}, msgs); err != nil {
		t.Fatal(err)
	}
	var jd Document
	if err := json.Unmarshal(jb.Bytes(), &jd); err != nil || jd.Count != 2 || jd.Messages[1].Body != sample[0].Body {
		t.Fatalf("json: %v %+v", err, jd)
	}

	var yb bytes.Buffer
	if err := Write(&yb, YAML, conv, Range{}, msgs); err != nil {
		t.Fatal(err)
	}
	var yd Document
	if err := yaml.Unmarshal(yb.Bytes(), &yd); err != nil || yd.Count != 2 || yd.Messages[1].Body != sample[0].Body {
		t.Fatalf("yaml: %v %+v", err, yd)
	}

	var cb bytes.Buffer
	if err := Write(&cb, CSV, conv, Range{}, msgs); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&cb).ReadAll()
	if err != nil || len(rows) != 3 || rows[0][0] != "timestamp" {
		t.Fatalf("csv: %v %v", err, rows)
	}
	if rows[2][5] != sample[0].Body {
		t.Fatalf("csv body mangled: %q", rows[2][5])
	}
}

func TestEmptyExportIsEmptyListNotNull(t *testing.T) {
	var b bytes.Buffer
	if err := Write(&b, JSON, Conversation{ID: "c"}, Range{}, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `"messages": []`) {
		t.Fatalf("got %s", b.String())
	}
}

func TestParseFormatAndBound(t *testing.T) {
	if f, err := ParseFormat(" YML "); err != nil || f != YAML {
		t.Fatalf("yml: %v %v", f, err)
	}
	if _, err := ParseFormat("xml"); err == nil {
		t.Fatal("xml should fail")
	}
	start, _ := ParseBound("2026-05-28", false)
	end, _ := ParseBound("2026-05-28", true)
	if end-start != int64(24*time.Hour/time.Millisecond)-1 {
		t.Fatalf("day span %d", end-start)
	}
	if _, err := ParseBound("garbage", false); err == nil {
		t.Fatal("garbage should fail")
	}
}

func TestDefaultFilenameIsSafe(t *testing.T) {
	r := Range{SinceMS: mustMS(t, "2026-05-01"), UntilMS: mustMS(t, "2026-05-31")}
	got := DefaultFilename(Conversation{Name: "../Bob/Smith: #ops", Platform: "slack"}, r, CSV)
	if strings.ContainsAny(got, `/\:#`) || strings.HasPrefix(got, ".") || !strings.HasSuffix(got, "_2026-05-01_2026-05-31.csv") {
		t.Fatalf("unsafe or wrong name %q", got)
	}
}

func mustMS(t *testing.T, s string) int64 {
	t.Helper()
	ms, err := ParseBound(s, false)
	if err != nil {
		t.Fatal(err)
	}
	return ms
}

// A date-only --until must cover the whole local calendar day even when DST
// makes that day 23 or 25 hours long.
func TestParseBoundEndOfDayAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("no tz database: %v", err)
	}
	old := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = old })

	for _, day := range []string{"2026-03-08", "2026-11-01", "2026-06-15"} { // spring-forward, fall-back, normal
		end, err := ParseBound(day, true)
		if err != nil {
			t.Fatal(err)
		}
		got := time.UnixMilli(end).In(loc)
		if got.Format("2006-01-02 15:04:05.000") != day+" 23:59:59.999" {
			t.Fatalf("%s end of day = %s", day, got.Format(time.RFC3339Nano))
		}
	}
}

func TestWriteFileTightensExistingPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteFile(path, JSON, Conversation{ID: "c"}, Range{}, nil); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		return // Windows has no POSIX mode bits to assert on
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %o, want 600", perm)
	}
}
