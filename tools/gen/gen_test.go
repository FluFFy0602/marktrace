package main

import (
	"bytes"
	"encoding/csv"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"marktrace/gen/check"
)

var outFiles = []string{
	"data/C16/T1/items.csv",
	"data/C16/T1/codes.csv",
	"data/C16/T1/operations.csv",
	"data/C16/mock_registry.csv",
	"data/C16/demo_codes.csv",
	"golden/status_summary.csv",
	"golden/violations.csv",
}

func generateTo(t *testing.T, seed uint64) string {
	t.Helper()
	ds, err := Generate(seed)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := WriteAll(ds, dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

// The independent replay must reproduce golden exactly, for several seeds.
func TestReplayReproducesGolden(t *testing.T) {
	for _, seed := range []uint64{16, 1, 2, 42, 2026} {
		dir := generateTo(t, seed)
		res, err := check.Replay(filepath.Join(dir, t1Dir))
		if err != nil {
			t.Fatalf("seed %d: replay: %v", seed, err)
		}
		if err := res.Validate(); err != nil {
			t.Errorf("seed %d: %v", seed, err)
		}
		if err := res.CompareGolden(filepath.Join(dir, goldenDir)); err != nil {
			t.Errorf("seed %d: %v", seed, err)
		}
		t.Logf("seed %d: %s", seed, res)
	}
}

func TestDeterministic(t *testing.T) {
	a, b := generateTo(t, 16), generateTo(t, 16)
	for _, f := range outFiles {
		x, _ := os.ReadFile(filepath.Join(a, f))
		y, _ := os.ReadFile(filepath.Join(b, f))
		if len(x) == 0 || !bytes.Equal(x, y) {
			t.Errorf("%s differs between two runs with the same seed", f)
		}
	}
}

// Files committed to the repo must equal a fresh `go run . -seed 16`.
func TestCommittedFilesUpToDate(t *testing.T) {
	root := "../.."
	if _, err := os.Stat(filepath.Join(root, t1Dir, "items.csv")); err != nil {
		t.Skip("generated files not present in repo")
	}
	dir := generateTo(t, 16)
	for _, f := range outFiles {
		want, _ := os.ReadFile(filepath.Join(dir, f))
		got, err := os.ReadFile(filepath.Join(root, f))
		if err != nil || !bytes.Equal(want, got) {
			t.Errorf("%s is stale: run `go run . -seed 16 -out ../../` in tools/gen", f)
		}
	}
}

func readAll(t *testing.T, path string) [][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	recs, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return recs[1:]
}

func TestRegistryAndDemoCodes(t *testing.T) {
	dir := generateTo(t, 16)
	mask := regexp.MustCompile(`^01(\d{14})21([A-Za-z0-9]{6})$`)

	items := map[string]string{}
	for _, r := range readAll(t, filepath.Join(dir, t1Dir, "items.csv"))[:nValidItems] {
		items[r[0]] = r[2]
	}
	t1 := map[string]bool{}
	for _, r := range readAll(t, filepath.Join(dir, t1Dir, "codes.csv")) {
		t1[r[0]] = true
	}
	for _, r := range readAll(t, filepath.Join(dir, t1Dir, "operations.csv")) {
		t1[r[1]] = true
	}
	registry := map[string]bool{}
	for _, r := range readAll(t, filepath.Join(dir, dataDir, "mock_registry.csv")) {
		registry[r[0]] = true
	}
	valid := readAll(t, filepath.Join(dir, t1Dir, "codes.csv"))[:512]
	for _, r := range valid {
		if !registry[r[0]] {
			t.Errorf("valid T1 code %s missing from mock registry", r[0])
		}
	}
	demo := readAll(t, filepath.Join(dir, dataDir, "demo_codes.csv"))
	if len(demo) != nDemoCodes {
		t.Fatalf("demo codes = %d, want %d", len(demo), nDemoCodes)
	}
	for _, r := range demo {
		m := mask.FindStringSubmatch(r[0])
		gtin, ok := items[r[1]]
		switch {
		case m == nil:
			t.Errorf("demo code %s is malformed", r[0])
		case !ok:
			t.Errorf("demo code %s refers to unknown item %s", r[0], r[1])
		case normGTIN(gtin) != m[1]:
			t.Errorf("demo code %s GTIN does not match item %s", r[0], r[1])
		case t1[r[0]]:
			t.Errorf("demo code %s is used in T1", r[0])
		case !registry[r[0]]:
			t.Errorf("demo code %s missing from mock registry", r[0])
		}
	}
	if want := 512 + nDemoCodes; len(registry) != want {
		t.Errorf("registry size = %d, want %d", len(registry), want)
	}
}

func TestGS1CheckDigit(t *testing.T) {
	// Example from docs/13-data-dictionary.md.
	if d := gs1CheckDigit("460123456789"); d != '3' {
		t.Errorf("check digit = %c, want 3", d)
	}
	if d := gs1CheckDigit("0460123456789"); d != '3' {
		t.Errorf("GTIN-14 check digit = %c, want 3", d)
	}
}
