package tui

import (
	"strings"
	"testing"

	"github.com/maxghenis/openmessage/internal/export"
)

func TestParseExportArgs(t *testing.T) {
	o, err := parseExportArgs("")
	if err != nil || o.Format != export.JSON || o.Range != (export.Range{}) || o.Last != 0 || o.Selected {
		t.Fatalf("defaults: %+v %v", o, err)
	}

	o, err = parseExportArgs("csv 2026-05-01..2026-05-31")
	if err != nil || o.Format != export.CSV || o.Range.SinceMS == 0 || o.Range.UntilMS <= o.Range.SinceMS {
		t.Fatalf("range: %+v %v", o, err)
	}

	o, _ = parseExportArgs("2026-05-01..")
	if o.Range.SinceMS == 0 || o.Range.UntilMS != 0 {
		t.Fatalf("open end: %+v", o.Range)
	}
	o, _ = parseExportArgs("..2026-05-01")
	if o.Range.SinceMS != 0 || o.Range.UntilMS == 0 {
		t.Fatalf("open start: %+v", o.Range)
	}

	o, _ = parseExportArgs("yaml 2026-05-10")
	if o.Range.SinceMS == 0 || o.Range.UntilMS-o.Range.SinceMS < 23*3600*1000 {
		t.Fatalf("single day: %+v", o.Range)
	}

	o, err = parseExportArgs("last 25 csv")
	if err != nil || o.Last != 25 || o.Format != export.CSV {
		t.Fatalf("last: %+v %v", o, err)
	}
	o, err = parseExportArgs("selected yaml")
	if err != nil || !o.Selected || o.Format != export.YAML {
		t.Fatalf("selected: %+v %v", o, err)
	}
	if o, err = parseExportArgs("last 5 2026-05-10"); err != nil || o.Last != 5 || o.Range.SinceMS == 0 {
		t.Fatalf("last+range: %+v %v", o, err)
	}

	for _, bad := range []string{
		"2026-06-01..2026-05-01", "nonsense", "last", "last x", "last 0", "last 5000",
		"selected last 5", "selected 2026-05-01",
	} {
		if _, err := parseExportArgs(bad); err == nil {
			t.Fatalf("%q should fail", bad)
		}
	}
}

// The palette must hand ">export" arguments to the exporter; a missing wire-up
// here silently exports the whole thread as JSON no matter what was typed.
func TestPaletteExportCarriesArgs(t *testing.T) {
	m := NewModel(nil)
	m.activeID = "c1"

	var item paletteItem
	for _, it := range m.commandPaletteItems("export csv last 5") {
		if it.ActionID == "export" {
			item = it
		}
	}
	if item.ActionID != "export" || item.Args != "csv last 5" {
		t.Fatalf("palette item = %+v", item)
	}

	// Bad args must surface as an error, proving they reached parseExportArgs
	// (the no-args path would have started an export instead).
	bad := item
	bad.Args = "last x"
	next, cmd := m.runPaletteAction(bad)
	got := next.(Model)
	if cmd != nil || !strings.Contains(got.err, "last N must be") {
		t.Fatalf("bad args: cmd=%v err=%q", cmd != nil, got.err)
	}
}
