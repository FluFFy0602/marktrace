package main

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"sort"
	"strings"
	"time"
)

const (
	tsLayout     = "2006-01-02T15:04:05-07:00"
	nValidItems  = 96
	nGTIN13Items = nValidItems / 5 // ~20% of items use GTIN-13
	nDemoCodes   = 30
	absentItem   = "ITM-0200" // item_code that never appears in items.csv
)

var msk = time.FixedZone("MSK", 3*60*60)

// Generation window: all valid histories fit into Sept–Oct 2026 (Moscow time).
var (
	windowStart = time.Date(2026, 9, 1, 0, 0, 0, 0, msk)
	windowEnd   = time.Date(2026, 10, 28, 0, 0, 0, 0, msk)
)

var statusOrder = []string{"issued", "introduced", "sold", "returned", "disposed"}

var newStatus = map[string]string{
	"issue":     "issued",
	"introduce": "introduced",
	"sell":      "sold",
	"return":    "returned",
	"dispose":   "disposed",
}

// pattern is a valid lifecycle history; count is how many codes get it.
type pattern struct {
	name  string
	ops   []string
	count int
}

var patterns = []pattern{
	{"issued", []string{"issue"}, 300},
	{"introduced", []string{"issue", "introduce"}, 110},
	{"sold", []string{"issue", "introduce", "sell"}, 70},
	{"returned", []string{"issue", "introduce", "sell", "return"}, 12},
	{"disposed_introduced", []string{"issue", "introduce", "dispose"}, 6},
	{"disposed_sold", []string{"issue", "introduce", "sell", "dispose"}, 4},
	{"disposed_returned", []string{"issue", "introduce", "sell", "return", "dispose"}, 2},
	{"resold", []string{"issue", "introduce", "sell", "return", "sell"}, 8},
}

var shops = []string{
	"op-shop-01", "op-shop-02", "op-shop-03", "op-shop-04",
	"op-shop-05", "op-shop-06", "op-shop-07", "op-shop-08",
}

// gapRange is the min/max delay before an operation of the given type.
var gapRange = map[string][2]time.Duration{
	"introduce": {2 * time.Hour, 72 * time.Hour},
	"sell":      {24 * time.Hour, 240 * time.Hour},
	"return":    {24 * time.Hour, 168 * time.Hour},
	"dispose":   {24 * time.Hour, 168 * time.Hour},
}

type item struct {
	code, name, gtin, producer string
}

type op struct {
	id, code, typ string
	ts            time.Time
	actor         string
}

type codeInfo struct {
	code    string
	item    *item
	pattern string
	shop    string
	ops     []*op
}

func (c *codeInfo) last() *op      { return c.ops[len(c.ops)-1] }
func (c *codeInfo) status() string { return newStatus[c.last().typ] }

// Violation is one row of golden/violations.csv.
type Violation struct {
	Code, Reason, Source, Ref string
}

// Injection documents one deliberately broken row (for the README / stdout report).
type Injection struct {
	File   string
	Line   int // 1-based file line, header is line 1
	Key    string
	Result string // reason code or "duplicate"
	Note   string
}

// Dataset is everything the generator writes.
type Dataset struct {
	Items, Codes, Ops [][]string // rows without header
	Registry          []string
	Demo              [][]string
	Summary           []int // counts parallel to statusOrder
	Violations        []Violation
	Injections        []Injection
}

type generator struct {
	rng       *rand.Rand
	usedCodes map[string]bool
	usedTS    map[int64]bool
	usedGTIN  map[string]bool
	items     []*item
}

func fmtTS(t time.Time) string { return t.In(msk).Format(tsLayout) }

func (g *generator) between(lo, hi time.Duration) time.Duration {
	return lo + time.Duration(g.rng.Int64N(int64(hi-lo)/int64(time.Second)+1))*time.Second
}

// workHours moves t forward into the 08:00–20:59 window of the same or next day.
func (g *generator) workHours(t time.Time) time.Time {
	t = t.In(msk)
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, msk)
	switch {
	case t.Hour() < 8:
		return day.Add(8*time.Hour + g.between(0, 13*time.Hour-time.Second))
	case t.Hour() >= 21:
		return day.AddDate(0, 0, 1).Add(8*time.Hour + g.between(0, 13*time.Hour-time.Second))
	}
	return t
}

// uniqueTS makes every generated instant globally unique (no ties in sorting).
func (g *generator) uniqueTS(t time.Time) time.Time {
	for g.usedTS[t.Unix()] {
		t = t.Add(time.Second)
	}
	g.usedTS[t.Unix()] = true
	return t
}

func (g *generator) randomTS() time.Time {
	days := int(windowEnd.Sub(windowStart).Hours() / 24)
	t := windowStart.AddDate(0, 0, g.rng.IntN(days)).Add(8*time.Hour + g.between(0, 13*time.Hour-time.Second))
	return g.uniqueTS(t)
}

const serialAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

func (g *generator) serial(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = serialAlphabet[g.rng.IntN(len(serialAlphabet))]
	}
	return string(b)
}

// newCode returns a fresh, well-formed, never-used code for the item.
func (g *generator) newCode(it *item) string {
	for {
		c := makeCode(it.gtin, g.serial(6))
		if !g.usedCodes[c] {
			g.usedCodes[c] = true
			return c
		}
	}
}

func (g *generator) randomItem() *item { return g.items[g.rng.IntN(len(g.items))] }

func (g *generator) otherItem(not *item) *item {
	for {
		if it := g.randomItem(); it != not {
			return it
		}
	}
}

func (g *generator) otherShop(not string) string {
	for {
		if s := shops[g.rng.IntN(len(shops))]; s != not {
			return s
		}
	}
}

// Generate builds the whole data set deterministically from seed.
func Generate(seed uint64) (*Dataset, error) {
	g := &generator{
		rng:       rand.New(rand.NewPCG(seed, seed^0x16C16C16)),
		usedCodes: map[string]bool{},
		usedTS:    map[int64]bool{},
		usedGTIN:  map[string]bool{},
	}
	ds := &Dataset{}

	badItemNames := g.buildItems()
	for _, it := range g.items {
		ds.Items = append(ds.Items, []string{it.code, it.name, it.gtin})
	}
	g.injectItems(ds, badItemNames)

	codes := g.buildCodes()

	// Valid operations, globally sorted by ts, get sequential op_ids.
	var validOps []*op
	for _, c := range codes {
		validOps = append(validOps, c.ops...)
	}
	sort.Slice(validOps, func(i, j int) bool { return validOps[i].ts.Before(validOps[j].ts) })
	for i, o := range validOps {
		o.id = fmt.Sprintf("OP-%06d", i+1)
		ds.Ops = append(ds.Ops, []string{o.id, o.code, o.typ, fmtTS(o.ts), o.actor})
	}

	// codes.csv lists codes in registration (issue) order.
	sort.Slice(codes, func(i, j int) bool { return codes[i].ops[0].ts.Before(codes[j].ops[0].ts) })
	pools := map[string][]*codeInfo{}
	for _, c := range codes {
		pools[c.pattern] = append(pools[c.pattern], c)
	}
	pick := func(name string) *codeInfo {
		p := pools[name]
		if len(p) == 0 {
			panic("pattern pool exhausted: " + name)
		}
		i := g.rng.IntN(len(p))
		c := p[i]
		pools[name] = slices.Delete(p, i, i+1)
		return c
	}

	// SNAPSHOT_MISMATCH: valid rows whose expected status is deliberately wrong.
	s1, s2 := pick("introduced"), pick("sold")
	wrongExpected := map[string]string{s1.code: "sold", s2.code: "returned"}

	for i, c := range codes {
		status := c.status()
		if w, ok := wrongExpected[c.code]; ok {
			status = w
			ds.Injections = append(ds.Injections, Injection{"codes.csv", i + 2, c.code, "SNAPSHOT_MISMATCH",
				fmt.Sprintf("ожидается %s, фактически %s", w, c.status())})
			ds.Violations = append(ds.Violations, Violation{c.code, "SNAPSHOT_MISMATCH", "codes", c.code})
		}
		ds.Codes = append(ds.Codes, []string{c.code, c.item.code, status, fmtTS(c.last().ts)})
		ds.Registry = append(ds.Registry, c.code)
	}

	g.injectCodes(ds, pick)
	g.injectOps(ds, pick)

	for i := 0; i < nDemoCodes; i++ {
		it := g.randomItem()
		code := g.newCode(it)
		ds.Demo = append(ds.Demo, []string{code, it.code})
		ds.Registry = append(ds.Registry, code)
	}

	ds.Summary = make([]int, len(statusOrder))
	for _, c := range codes {
		ds.Summary[slices.Index(statusOrder, c.status())]++
	}

	sortViolations(ds.Violations)
	return ds, nil
}

// buildItems creates the valid catalogue and returns names for the 3 bad items.
func (g *generator) buildItems() []string {
	type combo struct {
		cat            int
		product, brand string
	}
	var combos []combo
	for ci, c := range catalog {
		for _, p := range c.products {
			for _, b := range c.brands {
				combos = append(combos, combo{ci, p, b})
			}
		}
	}
	g.rng.Shuffle(len(combos), func(i, j int) { combos[i], combos[j] = combos[j], combos[i] })
	if len(combos) < nValidItems+3 {
		panic("catalog too small")
	}
	chosen := combos[:nValidItems]
	sort.SliceStable(chosen, func(i, j int) bool {
		if chosen[i].cat != chosen[j].cat {
			return chosen[i].cat < chosen[j].cat
		}
		return chosen[i].product+chosen[i].brand < chosen[j].product+chosen[j].brand
	})

	isGTIN13 := map[int]bool{}
	for _, i := range g.rng.Perm(nValidItems)[:nGTIN13Items] {
		isGTIN13[i] = true
	}
	brandPrefix := map[string]string{}
	for i, c := range chosen {
		gtin := g.newGTIN13(brandPrefix, c.brand)
		if !isGTIN13[i] {
			gtin = "0" + gtin
		}
		g.items = append(g.items, &item{
			code:     fmt.Sprintf("ITM-%04d", i+1),
			name:     fmt.Sprintf("%s, ТМ «%s»", c.product, c.brand),
			gtin:     gtin,
			producer: catalog[c.cat].producer,
		})
	}
	var bad []string
	for _, c := range combos[nValidItems : nValidItems+3] {
		bad = append(bad, fmt.Sprintf("%s, ТМ «%s»", c.product, c.brand))
	}
	return bad
}

// newGTIN13 returns a unique valid GTIN-13 under the brand's Russian (46x) company prefix.
func (g *generator) newGTIN13(prefixes map[string]string, brand string) string {
	prefix, ok := prefixes[brand]
	if !ok {
		for {
			prefix = fmt.Sprintf("46%d%04d", g.rng.IntN(10), g.rng.IntN(10000))
			if !slices.Contains(mapValues(prefixes), prefix) {
				break
			}
		}
		prefixes[brand] = prefix
	}
	for {
		body := fmt.Sprintf("%s%05d", prefix, g.rng.IntN(100000))
		gtin := body + string(gs1CheckDigit(body))
		if !g.usedGTIN[gtin] {
			g.usedGTIN[gtin] = true
			return gtin
		}
	}
}

func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

func (g *generator) buildCodes() []*codeInfo {
	var codes []*codeInfo
	opsOf := map[string][]string{}
	for _, p := range patterns {
		opsOf[p.name] = p.ops
		for k := 0; k < p.count; k++ {
			it := g.randomItem()
			codes = append(codes, &codeInfo{
				code:    g.newCode(it),
				item:    it,
				pattern: p.name,
				shop:    shops[g.rng.IntN(len(shops))],
			})
		}
	}
	g.rng.Shuffle(len(codes), func(i, j int) { codes[i], codes[j] = codes[j], codes[i] })
	for _, c := range codes {
		g.buildHistory(c, opsOf[c.pattern])
	}
	return codes
}

// buildHistory generates strictly increasing timestamps for a valid history.
func (g *generator) buildHistory(c *codeInfo, types []string) {
	var span time.Duration
	for _, typ := range types[1:] {
		span += gapRange[typ][1] + 24*time.Hour // +1 day margin for the working-hours shift
	}
	days := int((windowEnd.Sub(windowStart) - span).Hours() / 24)
	t := windowStart.AddDate(0, 0, g.rng.IntN(days)).Add(8*time.Hour + g.between(0, 13*time.Hour-time.Second))
	for i, typ := range types {
		if i > 0 {
			r := gapRange[typ]
			t = g.workHours(t.Add(g.between(r[0], r[1])))
		}
		t = g.uniqueTS(t)
		actor := c.shop
		if typ == "issue" || typ == "introduce" {
			actor = c.item.producer
		}
		c.ops = append(c.ops, &op{code: c.code, typ: typ, ts: t, actor: actor})
	}
}

func (g *generator) injectItems(ds *Dataset, names []string) {
	add := func(gtin, reason, note string, name string) {
		code := fmt.Sprintf("ITM-%04d", len(ds.Items)+1)
		ds.Items = append(ds.Items, []string{code, name, gtin})
		ds.Injections = append(ds.Injections, Injection{"items.csv", len(ds.Items) + 1, code, reason, note})
		ds.Violations = append(ds.Violations, Violation{"", reason, "items", code})
	}
	wrongCheck := func(gtin string) string {
		last := int(gtin[len(gtin)-1] - '0')
		return gtin[:len(gtin)-1] + string(byte('0'+(last+1+g.rng.IntN(8))%10))
	}
	prefixes := map[string]string{}
	g13 := g.newGTIN13(prefixes, "bad-1")
	add(wrongCheck(g13), "GTIN_CHECKSUM", "GTIN-13 "+g13[:12]+"?: неверная контрольная цифра", names[0])
	g14 := "0" + g.newGTIN13(prefixes, "bad-2")
	add(wrongCheck(g14), "GTIN_CHECKSUM", "GTIN-14 "+g14[:13]+"?: неверная контрольная цифра", names[1])
	g13f := g.newGTIN13(prefixes, "bad-3")
	add(g13f[:2]+"O"+g13f[3:], "GTIN_FORMAT", "латинская буква O вместо цифры в GTIN", names[2])
}

func (g *generator) injectCodes(ds *Dataset, pick func(string) *codeInfo) {
	add := func(row []string, reason, note string) {
		ds.Codes = append(ds.Codes, row)
		ds.Injections = append(ds.Injections, Injection{"codes.csv", len(ds.Codes) + 1, row[0], reason, note})
		ds.Violations = append(ds.Violations, Violation{row[0], reason, "codes", row[0]})
	}
	issuedRow := func(code, itemCode string) []string {
		return []string{code, itemCode, "issued", fmtTS(g.randomTS())}
	}

	it := g.randomItem()
	add(issuedRow("(01)"+normGTIN(it.gtin)+"(21)"+g.serial(6), it.code),
		"CODE_FORMAT", "код в человекочитаемом виде GS1 со скобками")
	it = g.randomItem()
	add(issuedRow("01"+normGTIN(it.gtin)+"21"+g.serial(5), it.code),
		"CODE_FORMAT", "серийный номер из 5 символов вместо 6")

	for i := 0; i < 2; i++ {
		d := pick("issued")
		other := g.otherItem(d.item)
		add([]string{d.code, other.code, d.status(), fmtTS(d.last().ts)},
			"DUPLICATE_CODE", "повтор принятого кода с другим товаром "+other.code+" (исходно "+d.item.code+")")
	}

	a := g.randomItem()
	b := g.otherItem(a)
	add(issuedRow(g.newCode(a), b.code), "GTIN_MISMATCH",
		"код содержит GTIN товара "+a.code+", а привязан к "+b.code)

	a = g.randomItem()
	add(issuedRow(g.newCode(a), absentItem), "ITEM_NOT_FOUND", "товара "+absentItem+" нет в справочнике")
}

func (g *generator) injectOps(ds *Dataset, pick func(string) *codeInfo) {
	nextID := len(ds.Ops) + 1
	freshID := func() string {
		id := fmt.Sprintf("OP-%06d", nextID)
		nextID++
		return id
	}
	add := func(row []string, result, note string) {
		ds.Ops = append(ds.Ops, row)
		ds.Injections = append(ds.Injections, Injection{"operations.csv", len(ds.Ops) + 1, row[0], result, note})
		if result != "duplicate" {
			ds.Violations = append(ds.Violations, Violation{row[1], result, "operations", row[0]})
		}
	}
	after := func(c *codeInfo, lo, hi time.Duration) string {
		return fmtTS(g.uniqueTS(g.workHours(c.last().ts.Add(g.between(lo, hi)))))
	}
	day := 24 * time.Hour

	c := pick("issued")
	add([]string{freshID(), c.code, "sell", after(c, day, 3*day), shops[g.rng.IntN(len(shops))]},
		"TRANSITION_NOT_ALLOWED", "продажа кода, который только выпущен (issued)")

	c = pick("disposed_sold")
	add([]string{freshID(), c.code, "dispose", after(c, day, 3*day), c.shop},
		"TRANSITION_NOT_ALLOWED", "повторное списание (disposed — финальный статус)")

	c = pick("sold")
	add([]string{freshID(), c.code, "sell", after(c, day, 3*day), g.otherShop(c.shop)},
		"TRANSITION_NOT_ALLOWED", "повторная продажа уже проданного кода")

	c = pick("introduced")
	issueTS, introTS := c.ops[0].ts, c.ops[1].ts
	// Introduce is at least 2h after issue, so this ts lies strictly between them.
	early := introTS.Add(-g.between(20*time.Minute, 60*time.Minute))
	if !early.After(issueTS) {
		panic("TIME_REVERSED injection: introduce too close to issue")
	}
	add([]string{freshID(), c.code, "sell", fmtTS(early), c.shop},
		"TIME_REVERSED", "продажа введённого кода с временем раньше ввода ("+fmtTS(introTS)+")")

	unknown := g.newCode(g.randomItem())
	add([]string{freshID(), unknown, "sell", fmtTS(g.randomTS()), shops[g.rng.IntN(len(shops))]},
		"CODE_NOT_FOUND", "корректный по маске код, отсутствующий в реестре")

	c = pick("introduced")
	add([]string{freshID(), c.code, "transfer", after(c, day, 2*day), c.shop},
		"VALUE_FORMAT", "неизвестный тип операции transfer")

	c = pick("sold")
	x := c.ops[2]
	add([]string{x.id, x.code, x.typ, fmtTS(x.ts), g.otherShop(x.actor)},
		"IDEMPOTENCY_CONFLICT", "op_id "+x.id+" уже принят с actor "+x.actor)

	c = pick("introduced")
	y := c.ops[1]
	add([]string{y.id, y.code, y.typ, fmtTS(y.ts), y.actor},
		"duplicate", "точный повтор строки "+y.id+" (не нарушение)")
}

var sourceRank = map[string]int{"items": 0, "codes": 1, "operations": 2}

// sortViolations applies the golden sort order (SPEC §7).
func sortViolations(v []Violation) {
	sort.SliceStable(v, func(i, j int) bool {
		a, b := v[i], v[j]
		if sourceRank[a.Source] != sourceRank[b.Source] {
			return sourceRank[a.Source] < sourceRank[b.Source]
		}
		if a.Ref != b.Ref {
			return a.Ref < b.Ref
		}
		return strings.Compare(a.Reason, b.Reason) < 0
	})
}
