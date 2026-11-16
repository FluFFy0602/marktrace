package reports

import (
	"strings"
	"testing"
)

func TestSortViolationRows(t *testing.T) {
	rows := [][]string{
		{"c2", "TIME_REVERSED", "operations", "o2"},
		{"c1", "SNAPSHOT_MISMATCH", "codes", "c1"},
		{"", "GTIN_FORMAT", "items", "I9"},
		{"c1", "DUPLICATE_CODE", "codes", "c1"},
		{"", "GTIN_CHECKSUM", "items", "I10"},
		{"c9", "VALUE_FORMAT", "operations", "o10"},
	}
	SortViolationRows(rows)
	var got []string
	for _, r := range rows {
		got = append(got, strings.Join(r, ","))
	}
	want := []string{
		",GTIN_CHECKSUM,items,I10",
		",GTIN_FORMAT,items,I9",
		"c1,DUPLICATE_CODE,codes,c1",
		"c1,SNAPSHOT_MISMATCH,codes,c1",
		"c9,VALUE_FORMAT,operations,o10",
		"c2,TIME_REVERSED,operations,o2",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s", strings.Join(got, "\n"))
	}
}

func TestCompareMultiset(t *testing.T) {
	c := compare([][]string{{"a", "1"}, {"a", "1"}, {"b", "2"}}, [][]string{{"a", "1"}, {"c", "3"}, {"b", "2"}})
	if c.Match || c.matched != 2 || len(c.Missing) != 1 || c.Missing[0] != "a,1" || len(c.Extra) != 1 || c.Extra[0] != "c,3" {
		t.Fatalf("%+v", c)
	}
}

func TestParseBound(t *testing.T) {
	for _, s := range []string{"2026-10-01T09:00:00+03:00", "2026-10-01T09:00", "2026-10-01"} {
		if _, _, err := ParseBound(s, true); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
	if _, excl, _ := ParseBound("2026-10-01", true); !excl {
		t.Error("date-only upper bound must be exclusive next day")
	}
	if _, _, err := ParseBound("yesterday", false); err == nil {
		t.Error("garbage accepted")
	}
}
