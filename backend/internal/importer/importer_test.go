package importer

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"marktrace/backend/internal/rules"
	"marktrace/backend/internal/testdb"
)

// gtin14 completes a 13-digit body with a valid check digit.
func gtin14(body string) string {
	for d := '0'; d <= '9'; d++ {
		g := body + string(d)
		if rules.CheckGTIN(g) == "" {
			return g
		}
	}
	panic("no check digit")
}

const (
	g1 = "4601234567893" // GTIN-13 of I1
	t1 = "2026-10-01T09:00:00+03:00"
	t2 = "2026-10-02T09:00:00+03:00"
	t3 = "2026-10-03T10:00:00Z"
	t5 = "2026-10-05T10:00:00Z"
)

var (
	g2 = gtin14("0460999888777") // GTIN-14 of I2
	c1 = "01" + rules.NormalizeGTIN(g1) + "21" + "AAAAA1"
	c2 = "01" + rules.NormalizeGTIN(g1) + "21" + "AAAAA2"
	c3 = "01" + g2 + "21" + "BBBBB3"
	c4 = "01" + g2 + "21" + "BBBBB4"
	c5 = "01" + rules.NormalizeGTIN(g1) + "21" + "AAAAA5"
	c6 = "01" + rules.NormalizeGTIN(g1) + "21" + "AAAAA6"
	c7 = "01" + g2 + "21" + "BBBBB7" // GTIN of I2, bound to I1 -> mismatch
	cu = "01" + g2 + "21" + "ZZZZZ9" // well-formed, never registered
)

// Fixture files: each row comment states the expected outcome.
func itemsCSV() string {
	return strings.Join([]string{
		"item_code,name,gtin",
		"I1,Молоко,4601234567893",             // accepted
		"I2,Кефир," + g2,                      // accepted
		"I1,Молоко,4601234567893",             // duplicate
		"I1,Сметана,4601234567893",            // IDEMPOTENCY_CONFLICT
		"I3,,4601234567893",                   // REQUIRED_FIELD
		"I4,Bad,12345",                        // GTIN_FORMAT
		"I5,Bad,4601234567890",                // GTIN_CHECKSUM
		"I6,X",                                // VALUE_FORMAT
		`I7,"Name, with comma",4601234567893`, // accepted
	}, "\n") + "\n"
}

func codesCSV() string {
	return strings.Join([]string{
		"code,item_code,status,ts",
		c1 + ",I1,sold," + t3,                  // accepted
		c2 + ",I1,introduced," + t2,            // accepted (snapshot mismatch later)
		c3 + ",I2,issued," + t1,                // accepted (absent -> snapshot mismatch)
		c4 + ",I2,issued,2026-10-01T06:00:00Z", // accepted (matches, other ts notation)
		c1 + ",I1,sold," + t3,                  // duplicate
		c1 + ",I2,sold," + t3,                  // DUPLICATE_CODE
		",I1,issued," + t1,                     // REQUIRED_FIELD
		"BADCODE,I1,issued," + t1,              // CODE_FORMAT
		c5 + ",I1,pending," + t1,               // VALUE_FORMAT
		c5 + ",I1,issued,2026-10-01",           // TIME_FORMAT
		c6 + ",NOPE,issued," + t1,              // ITEM_NOT_FOUND
		c7 + ",I1,issued," + t1,                // GTIN_MISMATCH
		"X8,I1",                                // VALUE_FORMAT (columns)
	}, "\n") + "\n"
}

func opsCSV() string {
	return strings.Join([]string{
		"op_id,code,op_type,ts,actor",
		"o1," + c1 + ",issue," + t1 + ",prod",                // accepted
		"o2," + c1 + ",introduce," + t2 + ",prod",            // accepted
		"o3," + c1 + ",sell," + t3 + ",shop",                 // accepted
		"o4," + c2 + ",issue," + t1 + ",prod",                // accepted
		"o5," + c4 + ",issue," + t1 + ",prod",                // accepted
		"o1," + c1 + ",issue," + t1 + ",prod",                // duplicate (op_id)
		"o1," + c1 + ",issue," + t1 + ",other",               // IDEMPOTENCY_CONFLICT (op_id)
		"o6," + c1 + ",issue,2026-10-01T06:00:00Z,prod",      // duplicate (code, ts)
		"o7," + c1 + ",introduce," + t1 + ",prod",            // IDEMPOTENCY_CONFLICT (code, ts)
		"o8," + c1 + ",transfer," + t5 + ",prod",             // VALUE_FORMAT
		"o9," + c1 + ",sell,2026-10-05,prod",                 // TIME_FORMAT
		"o10,BADCODE,sell," + t5 + ",prod",                   // CODE_FORMAT
		"o11," + cu + ",issue," + t5 + ",prod",               // CODE_NOT_FOUND (not in registry)
		"o12," + c3 + ",sell," + t5 + ",shop",                // CODE_NOT_FOUND (no history)
		"o13," + c1 + ",issue," + t5 + ",prod",               // DUPLICATE_CODE
		"o14," + c2 + ",sell," + t5 + ",shop",                // TRANSITION_NOT_ALLOWED
		"o15," + c2 + ",introduce,2026-09-30T00:00:00Z,prod", // TIME_REVERSED
		"o16," + c2 + ",introduce," + t5 + ",",               // REQUIRED_FIELD
		"o17," + c2,                                          // VALUE_FORMAT (columns)
	}, "\n") + "\n"
}

var wantItems = []string{"accepted", "accepted", "duplicate", rules.IdempotencyConflict, rules.RequiredField,
	rules.GTINFormat, rules.GTINChecksum, rules.ValueFormat, "accepted"}

var wantCodes = []string{"accepted", "accepted", "accepted", "accepted", "duplicate", rules.DuplicateCode,
	rules.RequiredField, rules.CodeFormat, rules.ValueFormat, rules.TimeFormat, rules.ItemNotFound,
	rules.GTINMismatch, rules.ValueFormat}

var wantOps = []string{"accepted", "accepted", "accepted", "accepted", "accepted", "duplicate",
	rules.IdempotencyConflict, "duplicate", rules.IdempotencyConflict, rules.ValueFormat, rules.TimeFormat,
	rules.CodeFormat, rules.CodeNotFound, rules.CodeNotFound, rules.DuplicateCode, rules.TransitionNotAllowed,
	rules.TimeReversed, rules.RequiredField, rules.ValueFormat}

func allFiles() []File {
	return []File{
		{Entity: EntItems, Filename: "items.csv", Data: []byte(itemsCSV())},
		{Entity: EntCodes, Filename: "codes.csv", Data: []byte(codesCSV())},
		{Entity: EntOperations, Filename: "operations.csv", Data: []byte(opsCSV())},
	}
}

func outcomes(t *testing.T, pool *pgxpool.Pool, session, entity string) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT processing_status, coalesce(reason,'') FROM import_rows
		WHERE session_id=$1::uuid AND entity=$2 ORDER BY line_no`, session, entity)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var st, reason string
		if err := rows.Scan(&st, &reason); err != nil {
			t.Fatal(err)
		}
		if st == "rejected" {
			out = append(out, reason)
		} else {
			out = append(out, st)
		}
	}
	return out
}

func tableCounts(t *testing.T, pool *pgxpool.Pool) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, tbl := range []string{"items", "code_registry", "codes", "operations", "violations"} {
		var n int
		if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM "+tbl).Scan(&n); err != nil {
			t.Fatal(err)
		}
		out[tbl] = n
	}
	return out
}

func eq(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		for i := 0; i < len(got) || i < len(want); i++ {
			var g, w string
			if i < len(got) {
				g = got[i]
			}
			if i < len(want) {
				w = want[i]
			}
			if g != w {
				t.Errorf("%s row %d: got %q want %q", what, i+1, g, w)
			}
		}
	}
}

func TestImportReasonsSnapshotAndReimport(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	im := &Importer{Pool: pool}

	res, err := im.Run(ctx, "admin", "admin", allFiles())
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "completed" {
		t.Fatalf("status %s", res.Status)
	}
	eq(t, "items", outcomes(t, pool, res.SessionID, EntItems), wantItems)
	eq(t, "codes", outcomes(t, pool, res.SessionID, EntCodes), wantCodes)
	eq(t, "operations", outcomes(t, pool, res.SessionID, EntOperations), wantOps)

	if c := res.Counters[EntOperations]; c.Accepted != 5 || c.Duplicate != 2 || c.Rejected != 12 {
		t.Errorf("ops counters %+v", *c)
	}
	if res.Snapshot != 2 {
		t.Errorf("snapshot violations = %d, want 2", res.Snapshot)
	}
	// Snapshot mismatches are exactly c2 (wrong status) and c3 (absent).
	var snap []string
	rows, _ := pool.Query(ctx, `SELECT code FROM violations WHERE reason='SNAPSHOT_MISMATCH' ORDER BY code`)
	for rows.Next() {
		var c string
		rows.Scan(&c)
		snap = append(snap, c)
	}
	rows.Close()
	eq(t, "snapshot", snap, []string{c2, c3})

	// Projection after operations.
	var st, ts string
	pool.QueryRow(ctx, `SELECT status, to_char(ts AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS"Z"') FROM codes WHERE code=$1`, c1).Scan(&st, &ts)
	if st != "sold" || ts != t3 {
		t.Errorf("c1 = %s %s", st, ts)
	}

	// Exactly one violation per rejected row: 5 + 8 + 12 rejected rows + 2 snapshot.
	before := tableCounts(t, pool)
	if before["violations"] != 27 {
		t.Errorf("violations = %d, want 27", before["violations"])
	}
	if res.RowsCreated != 3+4+5+27 {
		t.Errorf("rows_created = %d", res.RowsCreated)
	}
	// Every operation row produced one attempt.
	var attempts int
	pool.QueryRow(ctx, `SELECT count(*) FROM operation_attempts`).Scan(&attempts)
	if attempts != len(wantOps) {
		t.Errorf("attempts = %d, want %d", attempts, len(wantOps))
	}

	// Re-import: every row duplicate, nothing new (K3 = 0).
	res2, err := im.Run(ctx, "admin", "admin", allFiles())
	if err != nil {
		t.Fatal(err)
	}
	for ent, n := range map[string]int{EntItems: len(wantItems), EntCodes: len(wantCodes), EntOperations: len(wantOps)} {
		c := res2.Counters[ent]
		if c.Duplicate != n || c.Accepted != 0 || c.Rejected != 0 {
			t.Errorf("re-import %s counters %+v", ent, *c)
		}
	}
	if res2.RowsCreated != 0 {
		t.Errorf("re-import rows_created = %d", res2.RowsCreated)
	}
	after := tableCounts(t, pool)
	if fmt.Sprint(after) != fmt.Sprint(before) {
		t.Errorf("re-import changed tables: %v -> %v", before, after)
	}
}

func TestStructureRejected(t *testing.T) {
	pool := testdb.New(t)
	im := &Importer{Pool: pool}
	files := []File{
		{Entity: EntItems, Data: []byte(itemsCSV())},
		{Entity: EntCodes, Data: []byte("code,item,status,ts\n" + c1 + ",I1,sold," + t3 + "\n")},
	}
	res, err := im.Run(context.Background(), "admin", "admin", files)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "structure_rejected" {
		t.Fatalf("status %s", res.Status)
	}
	var reason string
	var rowsN int
	pool.QueryRow(context.Background(), `SELECT failure_reason FROM import_sessions WHERE session_id=$1::uuid`, res.SessionID).Scan(&reason)
	pool.QueryRow(context.Background(), `SELECT count(*) FROM import_rows`).Scan(&rowsN)
	if reason == "" || rowsN != 0 {
		t.Fatalf("reason=%q rows=%d", reason, rowsN)
	}
	// Empty file.
	res, _ = im.Run(context.Background(), "admin", "admin", []File{{Entity: EntItems, Data: nil}})
	if res.Status != "structure_rejected" {
		t.Fatalf("empty: %s", res.Status)
	}
}

// A closed SNAPSHOT_MISMATCH must not be raised again by a re-import (K3 stays 0).
func TestReimportAfterClosedSnapshotViolation(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	im := &Importer{Pool: pool}
	if _, err := im.Run(ctx, "admin", "admin", allFiles()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO violation_resolutions(id, violation_id, action, comment, author)
		SELECT gen_random_uuid(), violation_id, 'close', 'Проверено', 'qc' FROM violations WHERE reason='SNAPSHOT_MISMATCH'`); err != nil {
		t.Fatal(err)
	}
	res, err := im.Run(ctx, "admin", "admin", allFiles())
	if err != nil {
		t.Fatal(err)
	}
	if res.RowsCreated != 0 || res.Snapshot != 0 {
		t.Fatalf("re-import after close: rows_created=%d snapshot=%d", res.RowsCreated, res.Snapshot)
	}
}

// Files uploaded in separate sessions in the wrong order: rows rejected only because the
// referenced code/item did not exist yet are evaluated again once it exists.
func TestWrongOrderSessionsRecover(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	im := &Importer{Pool: pool}
	files := allFiles()
	itemsF, codesF, opsF := files[0], files[1], files[2]
	run := func(fs ...File) *Result {
		t.Helper()
		res, err := im.Run(ctx, "admin", "admin", fs)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	// codes before items: every well-formed row is ITEM_NOT_FOUND.
	run(codesF)
	if n := tableCounts(t, pool)["code_registry"]; n != 0 {
		t.Fatalf("registry after codes-only = %d", n)
	}
	// operations before codes: every well-formed op is CODE_NOT_FOUND.
	if res := run(opsF); res.Counters[EntOperations].Accepted != 0 {
		t.Fatalf("ops before codes accepted %d", res.Counters[EntOperations].Accepted)
	}
	run(itemsF)
	if res := run(codesF); res.Counters[EntCodes].Accepted != 4 {
		t.Fatalf("codes after items: %+v", *res.Counters[EntCodes])
	}
	if res := run(opsF); res.Counters[EntOperations].Accepted != 5 {
		t.Fatalf("ops after codes: %+v", *res.Counters[EntOperations])
	}
	var st, ts string
	pool.QueryRow(ctx, `SELECT status, to_char(ts AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS"Z"') FROM codes WHERE code=$1`, c1).Scan(&st, &ts)
	if st != "sold" || ts != t3 {
		t.Errorf("c1 = %s %s", st, ts)
	}
	// Repeating everything now creates nothing.
	if res := run(allFiles()...); res.RowsCreated != 0 {
		t.Fatalf("repeat rows_created = %d", res.RowsCreated)
	}
}

// A bare quote inside an unquoted field is tolerated (common in product names).
func TestLazyQuotes(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	im := &Importer{Pool: pool}
	res, err := im.Run(ctx, "admin", "admin", []File{{Entity: EntItems, Data: []byte(
		"item_code,name,gtin\nI1,Сыр \"Х\",4601234567893\n")}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "completed" || res.Counters[EntItems].Accepted != 1 {
		t.Fatalf("status %s counters %+v", res.Status, res.Counters[EntItems])
	}
	var name string
	pool.QueryRow(ctx, `SELECT name FROM items WHERE item_code='I1'`).Scan(&name)
	if name != `Сыр "Х"` {
		t.Fatalf("name = %q", name)
	}
}

func TestSnapshotNotDuplicatedAndOperationsOnlySession(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	im := &Importer{Pool: pool}
	if _, err := im.Run(ctx, "admin", "admin", allFiles()[:2]); err != nil {
		t.Fatal(err)
	}
	// Without operations every registered code is absent: 4 mismatches.
	var n int
	pool.QueryRow(ctx, `SELECT count(*) FROM violations WHERE reason='SNAPSHOT_MISMATCH'`).Scan(&n)
	if n != 4 {
		t.Fatalf("snapshot after codes-only = %d", n)
	}
	// Operations-only session: new mismatch content for c1/c2/c4 (now present), c3 unchanged.
	res, err := im.Run(ctx, "admin", "admin", allFiles()[2:])
	if err != nil {
		t.Fatal(err)
	}
	if res.Snapshot != 1 { // only c2 gets a new message (issued vs expected introduced); c1, c4 now match
		t.Fatalf("snapshot new = %d", res.Snapshot)
	}
	res, _ = im.Run(ctx, "admin", "admin", allFiles()[2:])
	if res.Snapshot != 0 || res.RowsCreated != 0 {
		t.Fatalf("repeat: snapshot=%d rows=%d", res.Snapshot, res.RowsCreated)
	}
}
