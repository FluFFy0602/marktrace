package httpapi

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"marktrace/backend/internal/importer"
	"marktrace/backend/internal/testdb"
)

// TestGoldenT1 imports the generated T1 files and compares with golden/ (K5 = 100%),
// then re-imports them (K3 = 0). Skipped when the generated files are absent.
func TestGoldenT1(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	t1 := filepath.Join(root, "data", "C16", "T1")
	var files []importer.File
	for _, ent := range importer.Entities {
		data, err := os.ReadFile(filepath.Join(t1, ent+".csv"))
		if err != nil {
			t.Skipf("T1 data not available: %v", err)
		}
		files = append(files, importer.File{Entity: ent, Filename: ent + ".csv", Data: data})
	}
	pool := testdb.New(t)
	s := New(pool, "http://127.0.0.1:1", filepath.Join(root, "golden"))
	ctx := context.Background()
	res, err := s.Importer.Run(ctx, "admin", "admin", files)
	if err != nil || res.Status != "completed" {
		t.Fatalf("import: %v %+v", err, res)
	}
	for ent, c := range res.Counters {
		t.Logf("%s: %+v", ent, *c)
	}
	g, err := s.Reports.GoldenCompare(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if g.Percent != 100 {
		t.Errorf("golden percent %.2f\nstatus missing=%v extra=%v\nviolations missing=%v extra=%v",
			g.Percent, g.StatusSummary.Missing, g.StatusSummary.Extra, g.Violations.Missing, g.Violations.Extra)
	}
	k, err := s.Reports.KPI(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("K1 %.2f%% (accepted %d, rejected %d), K2 %.2f%%", k.K1.Percent, k.K1.Accepted, k.K1.Rejected, k.K2.Percent)

	res2, err := s.Importer.Run(ctx, "admin", "admin", files)
	if err != nil {
		t.Fatal(err)
	}
	if res2.RowsCreated != 0 {
		t.Errorf("re-import rows_created = %d", res2.RowsCreated)
	}
	for ent, c := range res2.Counters {
		if c.Accepted != 0 || c.Rejected != 0 {
			t.Errorf("re-import %s: %+v", ent, *c)
		}
	}
}
