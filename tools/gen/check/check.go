// Package check is an independent re-implementation of the SPEC §2.3 import rules.
// It replays T1 files in file order (fresh state, one session) and is used only to
// self-check the generator: the golden files are produced by construction in the
// generator, and this package must reproduce them exactly.
package check

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	codeMask = regexp.MustCompile(`^01(\d{14})21([A-Za-z0-9]{6})$`)
	tsMask   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(Z|[+-]\d{2}:\d{2})$`)
	digits   = regexp.MustCompile(`^\d+$`)
)

var (
	statusOrder = []string{"issued", "introduced", "sold", "returned", "disposed"}
	opTypes     = map[string]string{
		"issue": "issued", "introduce": "introduced", "sell": "sold", "return": "returned", "dispose": "disposed",
	}
	allowedFrom = map[string][]string{
		"introduce": {"issued"},
		"sell":      {"introduced", "returned"},
		"return":    {"sold"},
		"dispose":   {"introduced", "sold", "returned"},
	}
)

// Counters are per-entity processing results.
type Counters struct{ Accepted, Duplicate, Rejected int }

// Violation mirrors one golden/violations.csv row.
type Violation struct{ Code, Reason, Source, Ref string }

// Result of replaying one import session.
type Result struct {
	Items, Codes, Ops Counters
	ItemsGTIN13       int // accepted items with a 13-digit GTIN
	Summary           []int
	Violations        []Violation
}

// K1 = accepted / (accepted + rejected) * 100 over operation attempts.
func (r *Result) K1() float64 {
	return float64(r.Ops.Accepted) * 100 / float64(r.Ops.Accepted+r.Ops.Rejected)
}

func (r *Result) String() string {
	return fmt.Sprintf("items %+v, codes %+v, operations %+v, K1=%.2f%%, summary=%v, violations=%d",
		r.Items, r.Codes, r.Ops, r.K1(), r.Summary, len(r.Violations))
}

// Validate asserts the SPEC §9 size and injection invariants.
func (r *Result) Validate() error {
	rows := func(c Counters) int { return c.Accepted + c.Duplicate + c.Rejected }
	var errs []string
	inRange := func(name string, n, lo, hi int) {
		if n < lo || n > hi {
			errs = append(errs, fmt.Sprintf("%s rows = %d, want %d..%d", name, n, lo, hi))
		}
	}
	inRange("items", rows(r.Items), 80, 120)
	inRange("codes", rows(r.Codes), 500, 800)
	inRange("operations", rows(r.Ops), 600, 900)
	if r.Ops.Rejected != 7 {
		errs = append(errs, fmt.Sprintf("rejected operations = %d, want 7", r.Ops.Rejected))
	}
	if r.Ops.Duplicate != 1 {
		errs = append(errs, fmt.Sprintf("duplicate operations = %d, want 1", r.Ops.Duplicate))
	}
	if r.Items.Rejected != 3 {
		errs = append(errs, fmt.Sprintf("rejected items = %d, want 3", r.Items.Rejected))
	}
	if r.Codes.Rejected != 6 {
		errs = append(errs, fmt.Sprintf("rejected codes rows = %d, want 6", r.Codes.Rejected))
	}
	if k := r.K1(); k < 99 {
		errs = append(errs, fmt.Sprintf("K1 = %.2f%%, want >= 99%%", k))
	}
	if share := float64(r.ItemsGTIN13) / float64(r.Items.Accepted); share < 0.15 || share > 0.25 {
		errs = append(errs, fmt.Sprintf("GTIN-13 share = %.2f, want ~0.20", share))
	}
	for i, n := range r.Summary {
		if n == 0 {
			errs = append(errs, "no codes end in status "+statusOrder[i])
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func gtinCheck(g string) string {
	if !digits.MatchString(g) || (len(g) != 13 && len(g) != 14) {
		return "GTIN_FORMAT"
	}
	sum, w := 0, 3
	for i := len(g) - 2; i >= 0; i-- {
		sum += int(g[i]-'0') * w
		w = 4 - w
	}
	if byte('0'+(10-sum%10)%10) != g[len(g)-1] {
		return "GTIN_CHECKSUM"
	}
	return ""
}

func norm(g string) string {
	if len(g) == 13 {
		return "0" + g
	}
	return g
}

func parseTS(s string) (time.Time, bool) {
	if !tsMask.MatchString(s) {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	return t, err == nil
}

func readCSV(path string, header []string) ([][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	recs, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(recs) == 0 || strings.Join(recs[0], ",") != strings.Join(header, ",") {
		return nil, fmt.Errorf("%s: structure_rejected: header must be %v", path, header)
	}
	return recs[1:], nil
}

type opRec struct {
	code, typ string
	ts        time.Time
	actor     string
}

type codeState struct {
	status string
	ts     time.Time
}

type registryRow struct {
	item, status, ts string
}

type replayer struct {
	res      Result
	items    map[string][2]string // item_code -> (name, gtin)
	registry map[string]registryRow
	regOrder []string
	codes    map[string]codeState
	ops      map[string]opRec
	byCodeTS map[string]opRec
	rejected map[string]bool // entity|key|raw of rejected rows
}

func (p *replayer) reject(source, code, ref, reason string, raw []string, c *Counters) {
	c.Rejected++
	p.res.Violations = append(p.res.Violations, Violation{code, reason, source, ref})
	p.rejected[source+"|"+ref+"|"+strings.Join(raw, "\x00")] = true
}

func (p *replayer) wasRejected(source, key string, raw []string) bool {
	return p.rejected[source+"|"+key+"|"+strings.Join(raw, "\x00")]
}

func anyEmpty(row []string) bool {
	for _, v := range row {
		if v == "" {
			return true
		}
	}
	return false
}

// Replay processes items.csv, codes.csv, operations.csv from dir as one session.
func Replay(dir string) (*Result, error) {
	p := &replayer{
		items:    map[string][2]string{},
		registry: map[string]registryRow{},
		codes:    map[string]codeState{},
		ops:      map[string]opRec{},
		byCodeTS: map[string]opRec{},
		rejected: map[string]bool{},
	}
	items, err := readCSV(filepath.Join(dir, "items.csv"), []string{"item_code", "name", "gtin"})
	if err != nil {
		return nil, err
	}
	codes, err := readCSV(filepath.Join(dir, "codes.csv"), []string{"code", "item_code", "status", "ts"})
	if err != nil {
		return nil, err
	}
	ops, err := readCSV(filepath.Join(dir, "operations.csv"), []string{"op_id", "code", "op_type", "ts", "actor"})
	if err != nil {
		return nil, err
	}
	for _, row := range items {
		p.item(row)
	}
	for _, row := range codes {
		p.code(row)
	}
	for _, row := range ops {
		p.op(row)
	}
	p.snapshot()

	p.res.Summary = make([]int, len(statusOrder))
	for _, st := range p.codes {
		for i, s := range statusOrder {
			if s == st.status {
				p.res.Summary[i]++
			}
		}
	}
	SortViolations(p.res.Violations)
	return &p.res, nil
}

func (p *replayer) item(row []string) {
	c := &p.res.Items
	key := ""
	if len(row) > 0 {
		key = row[0]
	}
	if len(row) != 3 {
		p.reject("items", "", key, "VALUE_FORMAT", row, c)
		return
	}
	code, name, gtin := row[0], row[1], row[2]
	if anyEmpty(row) {
		p.reject("items", "", code, "REQUIRED_FIELD", row, c)
		return
	}
	if ex, ok := p.items[code]; ok {
		if ex == [2]string{name, gtin} {
			c.Duplicate++
		} else {
			p.reject("items", "", code, "IDEMPOTENCY_CONFLICT", row, c)
		}
		return
	}
	if p.wasRejected("items", code, row) {
		c.Duplicate++
		return
	}
	if reason := gtinCheck(gtin); reason != "" {
		p.reject("items", "", code, reason, row, c)
		return
	}
	p.items[code] = [2]string{name, gtin}
	if len(gtin) == 13 {
		p.res.ItemsGTIN13++
	}
	c.Accepted++
}

func (p *replayer) code(row []string) {
	c := &p.res.Codes
	key := ""
	if len(row) > 0 {
		key = row[0]
	}
	if len(row) != 4 {
		p.reject("codes", key, key, "VALUE_FORMAT", row, c)
		return
	}
	code, itemCode, status, ts := row[0], row[1], row[2], row[3]
	if anyEmpty(row) {
		p.reject("codes", code, code, "REQUIRED_FIELD", row, c)
		return
	}
	if ex, ok := p.registry[code]; ok {
		if ex == (registryRow{itemCode, status, ts}) {
			c.Duplicate++
		} else {
			p.reject("codes", code, code, "DUPLICATE_CODE", row, c)
		}
		return
	}
	if p.wasRejected("codes", code, row) {
		c.Duplicate++
		return
	}
	m := codeMask.FindStringSubmatch(code)
	if m == nil {
		p.reject("codes", code, code, "CODE_FORMAT", row, c)
		return
	}
	if _, ok := opTypes[statusToOp(status)]; !ok {
		p.reject("codes", code, code, "VALUE_FORMAT", row, c)
		return
	}
	if _, ok := parseTS(ts); !ok {
		p.reject("codes", code, code, "TIME_FORMAT", row, c)
		return
	}
	it, ok := p.items[itemCode]
	if !ok {
		p.reject("codes", code, code, "ITEM_NOT_FOUND", row, c)
		return
	}
	if norm(it[1]) != m[1] {
		p.reject("codes", code, code, "GTIN_MISMATCH", row, c)
		return
	}
	p.registry[code] = registryRow{itemCode, status, ts}
	p.regOrder = append(p.regOrder, code)
	c.Accepted++
}

// statusToOp maps a CodeStatus back to the op type producing it ("" if unknown).
func statusToOp(status string) string {
	for op, st := range opTypes {
		if st == status {
			return op
		}
	}
	return ""
}

func (p *replayer) op(row []string) {
	c := &p.res.Ops
	key := ""
	if len(row) > 0 {
		key = row[0]
	}
	if len(row) != 5 {
		code := ""
		if len(row) > 1 {
			code = row[1]
		}
		p.reject("operations", code, key, "VALUE_FORMAT", row, c)
		return
	}
	id, code, typ, tsRaw, actor := row[0], row[1], row[2], row[3], row[4]
	rej := func(reason string) { p.reject("operations", code, id, reason, row, c) }
	if anyEmpty(row) {
		rej("REQUIRED_FIELD")
		return
	}
	ts, tsOK := parseTS(tsRaw)
	if ex, ok := p.ops[id]; ok {
		if tsOK && ex.code == code && ex.typ == typ && ex.ts.Equal(ts) && ex.actor == actor {
			c.Duplicate++
		} else {
			rej("IDEMPOTENCY_CONFLICT")
		}
		return
	}
	if p.wasRejected("operations", id, row) {
		c.Duplicate++
		return
	}
	newSt, ok := opTypes[typ]
	if !ok {
		rej("VALUE_FORMAT")
		return
	}
	if !tsOK {
		rej("TIME_FORMAT")
		return
	}
	if !codeMask.MatchString(code) {
		rej("CODE_FORMAT")
		return
	}
	ctKey := code + "|" + strconv.FormatInt(ts.Unix(), 10)
	if ex, ok := p.byCodeTS[ctKey]; ok {
		if ex.typ == typ && ex.actor == actor {
			c.Duplicate++
		} else {
			rej("IDEMPOTENCY_CONFLICT")
		}
		return
	}
	cur, hasHistory := p.codes[code]
	if typ == "issue" {
		if _, inReg := p.registry[code]; !inReg {
			rej("CODE_NOT_FOUND")
			return
		}
		if hasHistory {
			rej("DUPLICATE_CODE")
			return
		}
	} else {
		if !hasHistory {
			rej("CODE_NOT_FOUND")
			return
		}
		allowed := false
		for _, s := range allowedFrom[typ] {
			if s == cur.status {
				allowed = true
			}
		}
		if !allowed {
			rej("TRANSITION_NOT_ALLOWED")
			return
		}
		if !ts.After(cur.ts) {
			rej("TIME_REVERSED")
			return
		}
	}
	rec := opRec{code, typ, ts, actor}
	p.ops[id] = rec
	p.byCodeTS[ctKey] = rec
	p.codes[code] = codeState{newSt, ts}
	c.Accepted++
}

func (p *replayer) snapshot() {
	for _, code := range p.regOrder {
		reg := p.registry[code]
		exp, _ := parseTS(reg.ts)
		act, ok := p.codes[code]
		if !ok || act.status != reg.status || !act.ts.Equal(exp) {
			p.res.Violations = append(p.res.Violations, Violation{code, "SNAPSHOT_MISMATCH", "codes", code})
		}
	}
}

var sourceRank = map[string]int{"items": 0, "codes": 1, "operations": 2}

// SortViolations applies the golden order: source, then ref, then reason (byte order).
func SortViolations(v []Violation) {
	sort.SliceStable(v, func(i, j int) bool {
		a, b := v[i], v[j]
		if sourceRank[a.Source] != sourceRank[b.Source] {
			return sourceRank[a.Source] < sourceRank[b.Source]
		}
		if a.Ref != b.Ref {
			return a.Ref < b.Ref
		}
		return a.Reason < b.Reason
	})
}

// CompareGolden checks that the replay reproduces golden/*.csv exactly.
func (r *Result) CompareGolden(dir string) error {
	sum, err := readCSV(filepath.Join(dir, "status_summary.csv"), []string{"status", "count"})
	if err != nil {
		return err
	}
	var want, got []string
	for _, row := range sum {
		want = append(want, strings.Join(row, ","))
	}
	for i, s := range statusOrder {
		got = append(got, s+","+strconv.Itoa(r.Summary[i]))
	}
	if strings.Join(want, "\n") != strings.Join(got, "\n") {
		return fmt.Errorf("status_summary differs:\n golden: %v\n replay: %v", want, got)
	}

	vio, err := readCSV(filepath.Join(dir, "violations.csv"), []string{"code", "reason", "source", "ref"})
	if err != nil {
		return err
	}
	want, got = nil, nil
	for _, row := range vio {
		want = append(want, strings.Join(row, ","))
	}
	for _, v := range r.Violations {
		got = append(got, strings.Join([]string{v.Code, v.Reason, v.Source, v.Ref}, ","))
	}
	if strings.Join(want, "\n") != strings.Join(got, "\n") {
		return fmt.Errorf("violations differ:\n golden:\n  %s\n replay:\n  %s",
			strings.Join(want, "\n  "), strings.Join(got, "\n  "))
	}
	return nil
}
