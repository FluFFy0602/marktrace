package rules

import (
	"testing"
	"time"
)

func TestCheckGTIN(t *testing.T) {
	cases := []struct {
		gtin string
		want string
	}{
		{"4601234567893", ""},
		{"04601234567893", ""},
		{"4006381333931", ""},
		{"4601234567890", GTINChecksum},
		{"04601234567894", GTINChecksum},
		{"460123456789", GTINFormat},
		{"046012345678933", GTINFormat},
		{"460123456789A", GTINFormat},
		{" 4601234567893", GTINFormat},
		{"", GTINFormat},
	}
	for _, c := range cases {
		if got := CheckGTIN(c.gtin); got != c.want {
			t.Errorf("CheckGTIN(%q) = %q, want %q", c.gtin, got, c.want)
		}
	}
}

func TestNormalizeGTIN(t *testing.T) {
	if NormalizeGTIN("4601234567893") != "04601234567893" {
		t.Fatal("GTIN-13 must be prefixed with 0")
	}
	if NormalizeGTIN("04601234567893") != "04601234567893" {
		t.Fatal("GTIN-14 must be unchanged")
	}
}

func TestParseCode(t *testing.T) {
	cases := []struct {
		code string
		gtin string
		ok   bool
	}{
		{"010460123456789321AbC123", "04601234567893", true},
		{"010460123456789321abcdef", "04601234567893", true},
		{"010460123456789321AbC12", "", false},   // short serial
		{"010460123456789321AbC1234", "", false}, // long serial
		{"020460123456789321AbC123", "", false},  // wrong AI
		{"010460123456789322AbC123", "", false},  // wrong serial AI
		{"010460123456789321AbC-23", "", false},  // bad char
		{" 010460123456789321AbC12", "", false},  // no trimming
		{"01046012345678A321AbC123", "", false},  // non-digit gtin
		{"", "", false},
	}
	for _, c := range cases {
		g, ok := ParseCode(c.code)
		if ok != c.ok || g != c.gtin {
			t.Errorf("ParseCode(%q) = %q,%v want %q,%v", c.code, g, ok, c.gtin, c.ok)
		}
	}
}

func TestParseTime(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		want string
	}{
		{"2026-10-01T09:00:00+03:00", true, "2026-10-01T06:00:00Z"},
		{"2026-10-01T06:00:00Z", true, "2026-10-01T06:00:00Z"},
		{"2026-10-01T06:00:00-01:30", true, "2026-10-01T07:30:00Z"},
		{"2026-10-01T06:00:00", false, ""},      // no offset
		{"2026-10-01T06:00:00.123Z", false, ""}, // fractional
		{"2026-10-01 06:00:00Z", false, ""},     // space separator
		{"2026-10-01T06:00Z", false, ""},        // no seconds
		{"2026-10-01", false, ""},               // date only
		{"2026-13-01T06:00:00Z", false, ""},     // bad month
		{"2026-10-01t06:00:00z", false, ""},     // lowercase
		{"01.10.2026 09:00", false, ""},         // other format
		{"", false, ""},
	}
	for _, c := range cases {
		got, ok := ParseTime(c.in)
		if ok != c.ok {
			t.Errorf("ParseTime(%q) ok=%v want %v", c.in, ok, c.ok)
			continue
		}
		if ok && FormatTime(got) != c.want {
			t.Errorf("ParseTime(%q) = %s want %s", c.in, FormatTime(got), c.want)
		}
	}
}

func TestFormatTime(t *testing.T) {
	ts := time.Date(2026, 10, 1, 9, 0, 0, 0, time.FixedZone("MSK", 3*3600))
	if got := FormatTime(ts); got != "2026-10-01T06:00:00Z" {
		t.Fatal(got)
	}
}

func TestNextStatus(t *testing.T) {
	from := append([]string{""}, Statuses...)
	allowed := map[string]map[string]string{
		OpIssue:     {"": StatusIssued},
		OpIntroduce: {StatusIssued: StatusIntroduced},
		OpSell:      {StatusIntroduced: StatusSold, StatusReturned: StatusSold},
		OpReturn:    {StatusSold: StatusReturned},
		OpDispose:   {StatusIntroduced: StatusDisposed, StatusSold: StatusDisposed, StatusReturned: StatusDisposed},
	}
	for _, op := range append(OpTypes, "transfer") {
		for _, f := range from {
			to, ok := NextStatus(op, f)
			want, wantOK := allowed[op][f]
			if ok != wantOK || to != want {
				t.Errorf("NextStatus(%q,%q) = %q,%v want %q,%v", op, f, to, ok, want, wantOK)
			}
		}
	}
}

func TestEnums(t *testing.T) {
	if !IsStatus("sold") || IsStatus("Sold") || IsStatus("pending") {
		t.Fatal("IsStatus")
	}
	if !IsOpType("return") || IsOpType("transfer") || IsOpType("") {
		t.Fatal("IsOpType")
	}
}

func TestMessages(t *testing.T) {
	for _, r := range Reasons {
		if m := Message(r, ""); m == r || m == "" {
			t.Errorf("no message for %s", r)
		}
	}
	if Message(RequiredField, "gtin") != "Не заполнено обязательное поле: gtin" {
		t.Fatal(Message(RequiredField, "gtin"))
	}
}
