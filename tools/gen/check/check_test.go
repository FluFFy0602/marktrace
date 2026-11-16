package check

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// A hand-written mini session exercising every reason the replayer knows.
func TestReplayHandWritten(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 4601234567893 is a valid GTIN-13 (docs/13), its GTIN-14 form is 04601234567893.
	write("items.csv", `item_code,name,gtin
I1,"Молоко, 1 л",4601234567893
I2,Кефир,04600000000015
I1,"Молоко, 1 л",4601234567893
I1,Другое,4601234567893
I3,Сыр,4601234567890
I4,Вода,46012345678
I5,,4601234567893
`)
	write("codes.csv", `code,item_code,status,ts
010460123456789321AAAAAA,I1,sold,2026-10-01T12:00:00+03:00
010460123456789321BBBBBB,I1,issued,2026-10-01T09:00:00Z
010460123456789321AAAAAA,I1,sold,2026-10-01T12:00:00+03:00
010460123456789321AAAAAA,I2,sold,2026-10-01T12:00:00+03:00
010460123456789321CCCCC,I1,issued,2026-10-01T09:00:00Z
010460123456789321DDDDDD,I1,lost,2026-10-01T09:00:00Z
010460123456789321EEEEEE,I1,issued,2026-10-01 09:00:00
010460123456789321FFFFFF,I9,issued,2026-10-01T09:00:00Z
010460000000001521GGGGGG,I1,issued,2026-10-01T09:00:00Z
010460000000001521HHHHHH,I2,issued,2026-10-01T09:00:00Z
`)
	write("operations.csv", `op_id,code,op_type,ts,actor
o1,010460123456789321AAAAAA,issue,2026-10-01T10:00:00+03:00,p
o2,010460123456789321AAAAAA,introduce,2026-10-01T11:00:00+03:00,p
o3,010460123456789321AAAAAA,sell,2026-10-01T09:00:00Z,s
o1,010460123456789321AAAAAA,issue,2026-10-01T07:00:00Z,p
o1,010460123456789321AAAAAA,issue,2026-10-01T10:00:00+03:00,x
o4,010460123456789321AAAAAA,transfer,2026-10-02T10:00:00+03:00,s
o5,010460123456789321AAAAAA,return,2026-10-02T10:00:00.5+03:00,s
o6,BAD,return,2026-10-02T10:00:00+03:00,s
o7,010460123456789321AAAAAA,sell,2026-10-01T12:00:00+03:00,s
o8,010460123456789321AAAAAA,sell,2026-10-01T12:00:00+03:00,zz
o9,010460123456789321ZZZZZZ,issue,2026-10-02T10:00:00+03:00,p
o10,010460123456789321ZZZZZZ,sell,2026-10-02T10:00:00+03:00,p
o11,010460123456789321AAAAAA,issue,2026-10-03T10:00:00+03:00,p
o12,010460123456789321AAAAAA,introduce,2026-10-03T10:00:00+03:00,p
o13,010460123456789321BBBBBB,issue,2026-10-01T12:00:00+03:00,p
o14,010460123456789321BBBBBB,introduce,2026-10-01T11:00:00+03:00,p
o15,,sell,2026-10-01T11:00:00+03:00,p
o4,010460123456789321AAAAAA,transfer,2026-10-02T10:00:00+03:00,s
`)
	res, err := Replay(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []Violation{
		{"", "IDEMPOTENCY_CONFLICT", "items", "I1"},
		{"", "GTIN_CHECKSUM", "items", "I3"},
		{"", "GTIN_FORMAT", "items", "I4"},
		{"", "REQUIRED_FIELD", "items", "I5"},
		{"010460000000001521GGGGGG", "GTIN_MISMATCH", "codes", "010460000000001521GGGGGG"},
		{"010460000000001521HHHHHH", "SNAPSHOT_MISMATCH", "codes", "010460000000001521HHHHHH"}, // no history
		{"010460123456789321AAAAAA", "DUPLICATE_CODE", "codes", "010460123456789321AAAAAA"},
		{"010460123456789321CCCCC", "CODE_FORMAT", "codes", "010460123456789321CCCCC"},
		{"010460123456789321DDDDDD", "VALUE_FORMAT", "codes", "010460123456789321DDDDDD"},
		{"010460123456789321EEEEEE", "TIME_FORMAT", "codes", "010460123456789321EEEEEE"},
		{"010460123456789321FFFFFF", "ITEM_NOT_FOUND", "codes", "010460123456789321FFFFFF"},
		{"010460123456789321AAAAAA", "IDEMPOTENCY_CONFLICT", "operations", "o1"},
		{"010460123456789321AAAAAA", "DUPLICATE_CODE", "operations", "o11"},
		{"010460123456789321AAAAAA", "TRANSITION_NOT_ALLOWED", "operations", "o12"},
		{"010460123456789321BBBBBB", "TIME_REVERSED", "operations", "o14"},
		{"", "REQUIRED_FIELD", "operations", "o15"},
		{"010460123456789321AAAAAA", "VALUE_FORMAT", "operations", "o4"},
		{"010460123456789321AAAAAA", "TIME_FORMAT", "operations", "o5"},
		{"BAD", "CODE_FORMAT", "operations", "o6"},
		{"010460123456789321AAAAAA", "IDEMPOTENCY_CONFLICT", "operations", "o8"},
		{"010460123456789321ZZZZZZ", "CODE_NOT_FOUND", "operations", "o9"},
		{"010460123456789321ZZZZZZ", "CODE_NOT_FOUND", "operations", "o10"},
	}
	SortViolations(want)
	if !reflect.DeepEqual(res.Violations, want) {
		t.Errorf("violations:\n got  %v\n want %v", res.Violations, want)
	}
	// o1 repeat (UTC form of same instant), o7 (same code+ts as o3), o4 repeat of rejected row.
	if res.Ops != (Counters{Accepted: 4, Duplicate: 3, Rejected: 11}) {
		t.Errorf("ops counters = %+v", res.Ops)
	}
	if res.Items != (Counters{Accepted: 2, Duplicate: 1, Rejected: 4}) {
		t.Errorf("items counters = %+v", res.Items)
	}
	if res.Codes != (Counters{Accepted: 3, Duplicate: 1, Rejected: 6}) {
		t.Errorf("codes counters = %+v", res.Codes)
	}
	// AAAAAA ends sold at 09:00Z, BBBBBB issued at 09:00Z: both match their snapshots
	// although the expected ts is written with a different offset.
	if want := []int{1, 0, 1, 0, 0}; !reflect.DeepEqual(res.Summary, want) {
		t.Errorf("summary = %v, want %v", res.Summary, want)
	}
}
