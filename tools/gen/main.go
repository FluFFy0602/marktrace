// Command gen produces the synthetic T1 data set, the mock registry, demo codes
// and the golden files for MarkTrace (SPEC §9). Usage (from tools/gen):
//
//	go run . -seed 16 -out ../../
//	go run . -check-only -out ../../   # only replay existing files against golden
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"marktrace/gen/check"
)

func main() {
	seed := flag.Uint64("seed", 16, "random seed (output is deterministic for a seed)")
	out := flag.String("out", "../../", "repository root to write data/ and golden/ into")
	checkOnly := flag.Bool("check-only", false, "do not generate, only verify existing files against golden")
	flag.Parse()

	if !*checkOnly {
		ds, err := Generate(*seed)
		if err != nil {
			fail(err)
		}
		if err := WriteAll(ds, *out); err != nil {
			fail(err)
		}
		fmt.Printf("generated seed=%d into %s\n", *seed, *out)
		fmt.Printf("items=%d codes=%d operations=%d registry=%d demo=%d violations=%d\n",
			len(ds.Items), len(ds.Codes), len(ds.Ops), len(ds.Registry), len(ds.Demo), len(ds.Violations))
		fmt.Println("injected rows:")
		for _, in := range ds.Injections {
			fmt.Printf("  %-15s line %4d  %-28s %-22s %s\n", in.File, in.Line, in.Key, in.Result, in.Note)
		}
	}

	res, err := check.Replay(filepath.Join(*out, t1Dir))
	if err != nil {
		fail(fmt.Errorf("replay: %w", err))
	}
	if err := res.Validate(); err != nil {
		fail(fmt.Errorf("invariants: %w", err))
	}
	if err := res.CompareGolden(filepath.Join(*out, goldenDir)); err != nil {
		fail(fmt.Errorf("golden: %w", err))
	}
	fmt.Printf("check OK: %s\n", res)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gen:", err)
	os.Exit(1)
}
