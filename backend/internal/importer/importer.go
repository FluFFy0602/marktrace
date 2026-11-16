// Package importer processes CSV import sessions (SPEC §2).
package importer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"marktrace/backend/internal/ops"
	"marktrace/backend/internal/rules"
)

// Entities in processing order.
const (
	EntItems      = "items"
	EntCodes      = "codes"
	EntOperations = "operations"
)

// Entities lists entities in processing order.
var Entities = []string{EntItems, EntCodes, EntOperations}

// Headers are the exact required CSV headers.
var Headers = map[string][]string{
	EntItems:      {"item_code", "name", "gtin"},
	EntCodes:      {"code", "item_code", "status", "ts"},
	EntOperations: {"op_id", "code", "op_type", "ts", "actor"},
}

// File is one uploaded CSV.
type File struct {
	Entity   string
	Filename string
	Data     []byte
}

// Counters per entity.
type Counters struct {
	Accepted  int `json:"accepted"`
	Duplicate int `json:"duplicate"`
	Rejected  int `json:"rejected"`
}

// Result summarises a session.
type Result struct {
	SessionID   string               `json:"session_id"`
	Status      string               `json:"status"`
	Counters    map[string]*Counters `json:"counters"`
	RowsCreated int                  `json:"rows_created"`
	Snapshot    int                  `json:"snapshot_violations"`
}

// Importer runs import sessions.
type Importer struct {
	Pool *pgxpool.Pool
}

type row struct {
	line   int
	fields []string
}

type parsed struct {
	file File
	rows []row
}

// Run executes a full session: structure check, items → codes → operations → snapshot.
func (im *Importer) Run(ctx context.Context, username, role string, files []File) (*Result, error) {
	sessionID := uuid.NewString()
	filesMeta := []map[string]any{}
	for _, f := range files {
		sum := sha256.Sum256(f.Data)
		filesMeta = append(filesMeta, map[string]any{
			"entity": f.Entity, "filename": f.Filename, "size": len(f.Data), "sha256": hex.EncodeToString(sum[:]),
		})
	}
	metaJSON, _ := json.Marshal(filesMeta)
	if _, err := im.Pool.Exec(ctx, `INSERT INTO import_sessions(session_id, started_by, role, status, files)
		VALUES ($1::uuid,$2,$3,'running',$4::jsonb)`, sessionID, username, role, string(metaJSON)); err != nil {
		return nil, err
	}
	res := &Result{SessionID: sessionID, Counters: map[string]*Counters{}}

	// Structure check of every file before any row is processed.
	byEntity := map[string]*parsed{}
	for _, f := range files {
		p, reason := parseFile(f)
		if reason != "" {
			return im.finish(ctx, res, "structure_rejected", reason)
		}
		byEntity[f.Entity] = p
		res.Counters[f.Entity] = &Counters{}
	}

	initiator := username
	for _, ent := range Entities {
		p := byEntity[ent]
		if p == nil {
			continue
		}
		for _, r := range p.rows {
			status, created, err := im.processRow(ctx, sessionID, ent, r, initiator, role)
			if err != nil {
				return im.finish(ctx, res, "failed", fmt.Sprintf("Техническая ошибка в %s.csv, строка %d: %v", ent, r.line, err))
			}
			c := res.Counters[ent]
			switch status {
			case ops.Accepted:
				c.Accepted++
			case ops.Duplicate:
				c.Duplicate++
			default:
				c.Rejected++
			}
			res.RowsCreated += created
		}
	}
	if byEntity[EntCodes] != nil || byEntity[EntOperations] != nil {
		n, err := SnapshotCheck(ctx, im.Pool)
		if err != nil {
			return im.finish(ctx, res, "failed", fmt.Sprintf("Техническая ошибка проверки снимка: %v", err))
		}
		res.Snapshot = n
		res.RowsCreated += n
	}
	return im.finish(ctx, res, "completed", "")
}

func (im *Importer) finish(ctx context.Context, res *Result, status, reason string) (*Result, error) {
	res.Status = status
	counters, _ := json.Marshal(res.Counters)
	var fr *string
	if reason != "" {
		fr = &reason
	}
	// Use a fresh context so a cancelled request still closes the session.
	_, err := im.Pool.Exec(context.WithoutCancel(ctx), `UPDATE import_sessions SET status=$2, failure_reason=$3, finished_at=now(),
		counters=$4::jsonb, rows_created=$5 WHERE session_id=$1::uuid`,
		res.SessionID, status, fr, string(counters), res.RowsCreated)
	return res, err
}

// parseFile reads the whole CSV and checks the header. Returns a Russian reason on failure.
func parseFile(f File) (*parsed, string) {
	name := f.Entity + ".csv"
	if !utf8.Valid(f.Data) {
		return nil, fmt.Sprintf("Файл %s не в кодировке UTF-8", name)
	}
	data := bytes.TrimPrefix(f.Data, []byte("\xef\xbb\xbf"))
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	// Tolerate a bare quote inside an unquoted field (e.g. Сыр "Х"): well-formed RFC 4180
	// input parses identically, and one sloppy row must not reject the whole session.
	r.LazyQuotes = true
	header, err := r.Read()
	if errors.Is(err, io.EOF) {
		return nil, fmt.Sprintf("Файл %s пуст: нет строки заголовка", name)
	}
	if err != nil {
		return nil, fmt.Sprintf("Файл %s: ошибка разбора CSV: %v", name, err)
	}
	want := Headers[f.Entity]
	if strings.Join(header, ",") != strings.Join(want, ",") || len(header) != len(want) {
		return nil, fmt.Sprintf("Файл %s: заголовок «%s» не совпадает с ожидаемым «%s»",
			name, strings.Join(header, ","), strings.Join(want, ","))
	}
	p := &parsed{file: f}
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Sprintf("Файл %s: ошибка разбора CSV: %v", name, err)
		}
		line, _ := r.FieldPos(0)
		p.rows = append(p.rows, row{line: line, fields: rec})
	}
	return p, ""
}

// processRow handles one row in its own transaction. Returns the processing status
// and the number of new domain rows (items/code_registry/operations/violations).
func (im *Importer) processRow(ctx context.Context, sessionID, ent string, r row, initiator, role string) (string, int, error) {
	var status string
	created := 0
	err := pgx.BeginFunc(ctx, im.Pool, func(tx pgx.Tx) error {
		created = 0
		rowID := uuid.NewString()
		rawJSON, _ := json.Marshal(r.fields)
		key := ""
		if len(r.fields) > 0 {
			key = r.fields[0]
		}
		// Serialize identical keys across concurrent sessions.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, "row:"+ent+":"+key); err != nil {
			return err
		}
		// An ITEM_NOT_FOUND rejection of a codes row is not final once the item exists
		// (codes.csv uploaded before items.csv in a separate session).
		var prior bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM import_rows ir
			WHERE ir.entity=$1 AND ir.key=$2 AND ir.raw=$3::jsonb AND ir.processing_status='rejected'
			  AND NOT (ir.reason = 'ITEM_NOT_FOUND' AND EXISTS (SELECT 1 FROM items i WHERE i.item_code = ir.raw->>1)))`,
			ent, key, string(rawJSON)).Scan(&prior); err != nil {
			return err
		}
		// import_rows must exist before attempts/violations reference it; reason is set at the end.
		if _, err := tx.Exec(ctx, `INSERT INTO import_rows(row_id, session_id, entity, line_no, key, raw, processing_status)
			VALUES ($1::uuid,$2::uuid,$3,$4,$5,$6::jsonb,'rejected')`,
			rowID, sessionID, ent, r.line, key, string(rawJSON)); err != nil {
			return err
		}
		var reason string
		var err error
		switch ent {
		case EntItems:
			status, reason, err = processItem(ctx, tx, rowID, r.fields, prior)
		case EntCodes:
			status, reason, err = processCode(ctx, tx, rowID, r.fields, prior)
		case EntOperations:
			status, reason, err = processOperation(ctx, tx, rowID, r.fields, prior, initiator, role)
		}
		if err != nil {
			return err
		}
		switch status {
		case ops.Accepted:
			created = 1
		case ops.Rejected:
			created = 1 // the violation
		}
		var rp *string
		if reason != "" {
			rp = &reason
		}
		_, err = tx.Exec(ctx, `UPDATE import_rows SET processing_status=$2, reason=$3 WHERE row_id=$1::uuid`, rowID, status, rp)
		return err
	})
	return status, created, err
}

func insertViolation(ctx context.Context, tx pgx.Tx, source, ref, code, itemCode, reason, message, rowID string) error {
	_, err := tx.Exec(ctx, `INSERT INTO violations
		(violation_id, source, ref, code, item_code, reason, message, import_row_id, rules_version)
		VALUES ($1::uuid,$2,$3,$4,$5,$6,$7,$8::uuid,$9)`,
		uuid.NewString(), source, ref, nullIfEmpty(code), nullIfEmpty(itemCode), reason, message, rowID, rules.Version)
	return err
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func field(f []string, i int) string {
	if i < len(f) {
		return f[i]
	}
	return ""
}

func requiredMissing(names, values []string) string {
	var miss []string
	for i, n := range names {
		if values[i] == "" {
			miss = append(miss, n)
		}
	}
	return strings.Join(miss, ", ")
}

func columnsDetail(got, want int) string {
	return fmt.Sprintf("в строке %d полей, ожидается %d", got, want)
}

// processItem implements SPEC §2.3 items.csv.
func processItem(ctx context.Context, tx pgx.Tx, rowID string, f []string, prior bool) (string, string, error) {
	itemCode := field(f, 0)
	rej := func(reason, detail string) (string, string, error) {
		return ops.Rejected, reason, insertViolation(ctx, tx, EntItems, itemCode, "", itemCode, reason, rules.Message(reason, detail), rowID)
	}
	if len(f) != 3 {
		if prior {
			return ops.Duplicate, "", nil
		}
		return rej(rules.ValueFormat, columnsDetail(len(f), 3))
	}
	name, gtin := f[1], f[2]
	// 1. Required.
	if miss := requiredMissing(Headers[EntItems], f); miss != "" {
		if prior {
			return ops.Duplicate, "", nil
		}
		return rej(rules.RequiredField, miss)
	}
	// 2. Idempotency.
	var exName, exGtin string
	err := tx.QueryRow(ctx, `SELECT name, gtin FROM items WHERE item_code=$1`, itemCode).Scan(&exName, &exGtin)
	found := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", "", err
	}
	if found && exName == name && exGtin == gtin {
		return ops.Duplicate, "", nil
	}
	if prior {
		return ops.Duplicate, "", nil
	}
	if found {
		return rej(rules.IdempotencyConflict, fmt.Sprintf("товар %s уже есть с другими наименованием или GTIN", itemCode))
	}
	// 3–4. GTIN.
	if reason := rules.CheckGTIN(gtin); reason != "" {
		return rej(reason, fmt.Sprintf("«%s»", gtin))
	}
	// 5. Accept.
	if _, err := tx.Exec(ctx, `INSERT INTO items(item_code, name, gtin) VALUES ($1,$2,$3)`, itemCode, name, gtin); err != nil {
		return "", "", err
	}
	return ops.Accepted, "", nil
}

// processCode implements SPEC §2.3 codes.csv.
func processCode(ctx context.Context, tx pgx.Tx, rowID string, f []string, prior bool) (string, string, error) {
	code, itemCode := field(f, 0), field(f, 1)
	rej := func(reason, detail string) (string, string, error) {
		return ops.Rejected, reason, insertViolation(ctx, tx, EntCodes, code, code, itemCode, reason, rules.Message(reason, detail), rowID)
	}
	if len(f) != 4 {
		if prior {
			return ops.Duplicate, "", nil
		}
		return rej(rules.ValueFormat, columnsDetail(len(f), 4))
	}
	status, tsRaw := f[2], f[3]
	// 1. Required.
	if miss := requiredMissing(Headers[EntCodes], f); miss != "" {
		if prior {
			return ops.Duplicate, "", nil
		}
		return rej(rules.RequiredField, miss)
	}
	ts, tsOK := rules.ParseTime(tsRaw)
	// 2. Already registered.
	var exItem string
	var exStatus *string
	var exTs *time.Time
	err := tx.QueryRow(ctx, `SELECT item_code, expected_status, expected_ts FROM code_registry WHERE code=$1`, code).
		Scan(&exItem, &exStatus, &exTs)
	found := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", "", err
	}
	if found && exItem == itemCode && exStatus != nil && *exStatus == status && exTs != nil && tsOK && exTs.Equal(ts) {
		return ops.Duplicate, "", nil
	}
	if prior {
		return ops.Duplicate, "", nil
	}
	if found {
		return rej(rules.DuplicateCode, fmt.Sprintf("код уже привязан к товару %s", exItem))
	}
	// 3. Mask.
	embedded, ok := rules.ParseCode(code)
	if !ok {
		return rej(rules.CodeFormat, fmt.Sprintf("«%s»", code))
	}
	// 4. Status.
	if !rules.IsStatus(status) {
		return rej(rules.ValueFormat, fmt.Sprintf("статус «%s» не входит в допустимые", status))
	}
	// 5. Time.
	if !tsOK {
		return rej(rules.TimeFormat, fmt.Sprintf("«%s»", tsRaw))
	}
	// 6. Item.
	var gtin string
	err = tx.QueryRow(ctx, `SELECT gtin FROM items WHERE item_code=$1`, itemCode).Scan(&gtin)
	if errors.Is(err, pgx.ErrNoRows) {
		return rej(rules.ItemNotFound, itemCode)
	}
	if err != nil {
		return "", "", err
	}
	// 7. GTIN match.
	if rules.NormalizeGTIN(gtin) != embedded {
		return rej(rules.GTINMismatch, fmt.Sprintf("товар %s имеет GTIN %s, в коде %s", itemCode, gtin, embedded))
	}
	// 8. Accept.
	if _, err := tx.Exec(ctx, `INSERT INTO code_registry(code, item_code, expected_status, expected_ts) VALUES ($1,$2,$3,$4)`,
		code, itemCode, status, ts); err != nil {
		return "", "", err
	}
	return ops.Accepted, "", nil
}

// processOperation implements SPEC §2.3 operations.csv via the shared ops engine.
func processOperation(ctx context.Context, tx pgx.Tx, rowID string, f []string, prior bool, initiator, role string) (string, string, error) {
	in := ops.Input{
		OpID: field(f, 0), Code: field(f, 1), OpType: field(f, 2), TsRaw: field(f, 3), Actor: field(f, 4),
		Source: "import", ImportRowID: rowID, Initiator: initiator, Role: role, PriorRejectedRaw: prior,
	}
	if len(f) != 5 {
		out := ops.Duplicate
		reason := ""
		o := ops.Outcome{Decision: ops.Duplicate}
		if !prior {
			o = ops.Reject(rules.ValueFormat, columnsDetail(len(f), 5))
			out, reason = ops.Rejected, rules.ValueFormat
		}
		_, err := ops.Record(ctx, tx, in, o, nil)
		return out, reason, err
	}
	o, err := ops.ProcessImportRow(ctx, tx, in)
	if err != nil {
		return "", "", err
	}
	return o.Decision, o.Reason, nil
}

// SnapshotCheck compares code_registry expectations with the codes projection and
// records SNAPSHOT_MISMATCH violations not already reported (open or closed) with the
// same content, so a re-import after QC closed a discrepancy still creates 0 rows.
func SnapshotCheck(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	type mismatch struct{ code, item, msg string }
	rows, err := pool.Query(ctx, `SELECT r.code, r.item_code, r.expected_status, r.expected_ts, c.status, c.ts
		FROM code_registry r LEFT JOIN codes c ON c.code = r.code
		WHERE r.expected_status IS NOT NULL AND r.expected_ts IS NOT NULL
		ORDER BY r.code`)
	if err != nil {
		return 0, err
	}
	var list []mismatch
	for rows.Next() {
		var code, item, expStatus string
		var expTs time.Time
		var actStatus *string
		var actTs *time.Time
		if err := rows.Scan(&code, &item, &expStatus, &expTs, &actStatus, &actTs); err != nil {
			rows.Close()
			return 0, err
		}
		if actStatus != nil && *actStatus == expStatus && actTs != nil && actTs.Equal(expTs) {
			continue
		}
		actual := "код отсутствует (нет принятых операций)"
		if actStatus != nil {
			actual = fmt.Sprintf("статус %s, ts %s", *actStatus, rules.FormatTime(*actTs))
		}
		detail := fmt.Sprintf("ожидалось статус %s, ts %s; фактически %s", expStatus, rules.FormatTime(expTs), actual)
		list = append(list, mismatch{code, item, rules.Message(rules.SnapshotMismatch, detail)})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	created := 0
	for _, m := range list {
		tag, err := pool.Exec(ctx, `INSERT INTO violations
			(violation_id, source, ref, code, item_code, reason, message, rules_version)
			SELECT $1::uuid, 'codes', $2, $2, $3, 'SNAPSHOT_MISMATCH', $4, $5
			WHERE NOT EXISTS (
				SELECT 1 FROM violations v
				WHERE v.reason = 'SNAPSHOT_MISMATCH' AND v.code = $2 AND v.message = $4)`,
			uuid.NewString(), m.code, m.item, m.msg, rules.Version)
		if err != nil {
			return created, err
		}
		created += int(tag.RowsAffected())
	}
	return created, nil
}
