package main

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strconv"
)

// Output layout relative to the -out directory (SPEC §0).
const (
	t1Dir     = "data/C16/T1"
	dataDir   = "data/C16"
	goldenDir = "golden"
)

func writeCSV(path string, header []string, rows [][]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	if err := w.Write(header); err != nil {
		f.Close()
		return err
	}
	if err := w.WriteAll(rows); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// WriteAll writes every generated file under out.
func WriteAll(ds *Dataset, out string) error {
	registry := make([][]string, len(ds.Registry))
	for i, c := range ds.Registry {
		registry[i] = []string{c}
	}
	summary := make([][]string, len(statusOrder))
	for i, s := range statusOrder {
		summary[i] = []string{s, strconv.Itoa(ds.Summary[i])}
	}
	violations := make([][]string, len(ds.Violations))
	for i, v := range ds.Violations {
		violations[i] = []string{v.Code, v.Reason, v.Source, v.Ref}
	}

	files := []struct {
		path   string
		header []string
		rows   [][]string
	}{
		{filepath.Join(out, t1Dir, "items.csv"), []string{"item_code", "name", "gtin"}, ds.Items},
		{filepath.Join(out, t1Dir, "codes.csv"), []string{"code", "item_code", "status", "ts"}, ds.Codes},
		{filepath.Join(out, t1Dir, "operations.csv"), []string{"op_id", "code", "op_type", "ts", "actor"}, ds.Ops},
		{filepath.Join(out, dataDir, "mock_registry.csv"), []string{"code"}, registry},
		{filepath.Join(out, dataDir, "demo_codes.csv"), []string{"code", "item_code"}, ds.Demo},
		{filepath.Join(out, goldenDir, "status_summary.csv"), []string{"status", "count"}, summary},
		{filepath.Join(out, goldenDir, "violations.csv"), []string{"code", "reason", "source", "ref"}, violations},
	}
	for _, f := range files {
		if err := writeCSV(f.path, f.header, f.rows); err != nil {
			return err
		}
	}
	return nil
}
