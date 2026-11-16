// Package ops evaluates and records operation attempts (SPEC §2.3 operations, §3).
// The same engine serves operations.csv rows and the operation form.
package ops

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"marktrace/backend/internal/rules"
	"marktrace/backend/internal/store"
)

// Decisions.
const (
	Accepted  = "accepted"
	Rejected  = "rejected"
	Duplicate = "duplicate"
)

// Input is one operation attempt as received (raw strings, no trimming).
type Input struct {
	OpID, Code, OpType, TsRaw, Actor string
	// ItemCode is used only by the form for issue: the binding to create/validate.
	ItemCode string
	// FormIssue enables the form binding rules for issue (SPEC §3).
	FormIssue bool
	// PriorRejectedRaw: an identical raw import row was rejected earlier; a row that
	// fails step 1 again is then a duplicate (keeps re-import idempotent).
	PriorRejectedRaw bool

	Source      string // "form" | "import"
	ImportRowID string // empty when not from import
	Initiator   string
	Role        string
}

// Outcome is the result of evaluation.
type Outcome struct {
	Decision string
	Reason   string
	Message  string

	ts         time.Time
	tsOK       bool
	ItemCode   string // item the code is (or will be) bound to, if known
	GTIN       string // GTIN of that item, if known
	bindingNew bool
	newStatus  string
}

// Reject builds a rejected outcome.
func Reject(reason, detail string) Outcome {
	return Outcome{Decision: Rejected, Reason: reason, Message: rules.Message(reason, detail)}
}

func duplicate() Outcome { return Outcome{Decision: Duplicate} }

// lock serializes attempts on the same op_id and code within the transaction.
func lock(ctx context.Context, tx pgx.Tx, in Input) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('op:' || $1::text, 0))`, in.OpID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('code:' || $1::text, 0))`, in.Code); err != nil {
		return err
	}
	// Row lock on the code projection (no-op when the code has no history yet).
	_, err := tx.Exec(ctx, `SELECT 1 FROM codes WHERE code = $1 FOR UPDATE`, in.Code)
	return err
}

// Evaluate runs checks 1–10 of SPEC §2.3 (operations) in order; the first failure wins.
func Evaluate(ctx context.Context, db store.DB, in Input) (Outcome, error) {
	// 1. Required fields.
	missing := ""
	for _, f := range []struct{ name, v string }{
		{"op_id", in.OpID}, {"code", in.Code}, {"op_type", in.OpType}, {"ts", in.TsRaw}, {"actor", in.Actor},
	} {
		if f.v == "" {
			missing = appendName(missing, f.name)
		}
	}
	if in.FormIssue && in.OpType == rules.OpIssue && in.ItemCode == "" {
		missing = appendName(missing, "item_code")
	}
	if missing != "" {
		if in.PriorRejectedRaw {
			return duplicate(), nil
		}
		return Reject(rules.RequiredField, missing), nil
	}

	ts, tsOK := rules.ParseTime(in.TsRaw)

	// 2. op_id idempotency.
	var exCode, exType, exActor string
	var exTs time.Time
	err := db.QueryRow(ctx, `SELECT code, op_type, ts, actor FROM operations WHERE op_id = $1`, in.OpID).
		Scan(&exCode, &exType, &exTs, &exActor)
	found := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Outcome{}, err
	}
	if found && exCode == in.Code && exType == in.OpType && tsOK && exTs.Equal(ts) && exActor == in.Actor {
		return duplicate(), nil
	}
	prior, err := priorRejected(ctx, db, in)
	if err != nil {
		return Outcome{}, err
	}
	if prior {
		return duplicate(), nil
	}
	if found {
		return Reject(rules.IdempotencyConflict,
			fmt.Sprintf("операция %s уже принята с другим содержимым", in.OpID)), nil
	}

	// 3. op_type.
	if !rules.IsOpType(in.OpType) {
		return Reject(rules.ValueFormat, fmt.Sprintf("тип операции «%s» не поддерживается", in.OpType)), nil
	}
	// 4. ts.
	if !tsOK {
		return Reject(rules.TimeFormat, fmt.Sprintf("«%s»", in.TsRaw)), nil
	}
	// 5. code mask.
	embedded, ok := rules.ParseCode(in.Code)
	if !ok {
		return Reject(rules.CodeFormat, fmt.Sprintf("«%s»", in.Code)), nil
	}

	out := Outcome{ts: ts, tsOK: true}

	// Registry binding and projection of the code.
	var regItem string
	err = db.QueryRow(ctx, `SELECT item_code FROM code_registry WHERE code = $1`, in.Code).Scan(&regItem)
	inRegistry := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Outcome{}, err
	}
	var curStatus string
	var curTs time.Time
	err = db.QueryRow(ctx, `SELECT status, ts FROM codes WHERE code = $1`, in.Code).Scan(&curStatus, &curTs)
	hasHistory := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Outcome{}, err
	}
	if inRegistry {
		out.ItemCode = regItem
	}

	// Form binding rules for issue (SPEC §3).
	if in.FormIssue && in.OpType == rules.OpIssue {
		if inRegistry && regItem != in.ItemCode {
			return Reject(rules.DuplicateCode, fmt.Sprintf("код привязан к товару %s", regItem)), nil
		}
		var gtin string
		err = db.QueryRow(ctx, `SELECT gtin FROM items WHERE item_code = $1`, in.ItemCode).Scan(&gtin)
		if errors.Is(err, pgx.ErrNoRows) {
			return Reject(rules.ItemNotFound, in.ItemCode), nil
		}
		if err != nil {
			return Outcome{}, err
		}
		if rules.NormalizeGTIN(gtin) != embedded {
			return Reject(rules.GTINMismatch, fmt.Sprintf("товар %s имеет GTIN %s, в коде %s", in.ItemCode, gtin, embedded)), nil
		}
		out.ItemCode = in.ItemCode
		out.GTIN = gtin
		out.bindingNew = !inRegistry
		inRegistry = true
	}

	// 6. (code, ts) uniqueness.
	var sameOpID, sameType, sameActor string
	err = db.QueryRow(ctx, `SELECT op_id, op_type, actor FROM operations WHERE code = $1 AND ts = $2`, in.Code, ts).
		Scan(&sameOpID, &sameType, &sameActor)
	if err == nil {
		if sameType == in.OpType && sameActor == in.Actor {
			return duplicate(), nil
		}
		return Reject(rules.IdempotencyConflict,
			fmt.Sprintf("на время %s по коду уже принята операция %s", rules.FormatTime(ts), sameOpID)), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Outcome{}, err
	}

	// 7. Existence.
	if in.OpType == rules.OpIssue {
		if !inRegistry {
			return Reject(rules.CodeNotFound, fmt.Sprintf("код %s отсутствует в реестре кодов", in.Code)), nil
		}
	} else if !hasHistory {
		return Reject(rules.CodeNotFound, fmt.Sprintf("у кода %s нет принятой истории", in.Code)), nil
	}
	// 8. Repeated issue.
	if in.OpType == rules.OpIssue && hasHistory {
		return Reject(rules.DuplicateCode, "код уже выпущен"), nil
	}
	// 9. Transition.
	from := ""
	if hasHistory {
		from = curStatus
	}
	next, ok := rules.NextStatus(in.OpType, from)
	if !ok {
		return Reject(rules.TransitionNotAllowed,
			fmt.Sprintf("операция %s из статуса %s (%s)", in.OpType, from, rules.StatusRu(from))), nil
	}
	// 10. Time order.
	if hasHistory && !ts.After(curTs) {
		return Reject(rules.TimeReversed,
			fmt.Sprintf("%s не позже %s", rules.FormatTime(ts), rules.FormatTime(curTs))), nil
	}
	out.Decision = Accepted
	out.newStatus = next
	if out.GTIN == "" && out.ItemCode != "" {
		if err := db.QueryRow(ctx, `SELECT gtin FROM items WHERE item_code = $1`, out.ItemCode).Scan(&out.GTIN); err != nil &&
			!errors.Is(err, pgx.ErrNoRows) {
			return Outcome{}, err
		}
	}
	return out, nil
}

func appendName(list, name string) string {
	if list == "" {
		return name
	}
	return list + ", " + name
}

// priorRejected reports whether an earlier attempt with the same op_id and identical
// raw content was rejected (re-import must yield duplicate, not a new violation).
// Transient mock unavailability is not considered final. A CODE_NOT_FOUND rejection is not
// final either once the code has appeared (files uploaded in separate sessions in the
// wrong order): the row is then evaluated again instead of staying a permanent duplicate.
func priorRejected(ctx context.Context, db store.DB, in Input) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM operation_attempts a
		WHERE a.op_id = $1 AND a.code = $2 AND a.op_type = $3 AND a.ts_raw = $4 AND a.actor = $5
		  AND a.decision = 'rejected' AND a.reason IS DISTINCT FROM 'MOCK_UNAVAILABLE'
		  AND NOT (a.reason = 'CODE_NOT_FOUND' AND (
		        EXISTS (SELECT 1 FROM codes c WHERE c.code = $2)
		     OR ($3 = 'issue' AND EXISTS (SELECT 1 FROM code_registry r WHERE r.code = $2)))))`,
		in.OpID, in.Code, in.OpType, in.TsRaw, in.Actor).Scan(&exists)
	return exists, err
}

// Record stores the attempt and its effects (operation + projection, or violation)
// inside tx. Returns the attempt id.
func Record(ctx context.Context, tx pgx.Tx, in Input, out Outcome, mockResult []byte) (string, error) {
	attemptID := uuid.NewString()
	var reason *string
	if out.Decision == Rejected {
		reason = &out.Reason
	}
	var importRow *string
	if in.ImportRowID != "" {
		importRow = &in.ImportRowID
	}
	var mock any
	if mockResult != nil {
		mock = string(mockResult)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO operation_attempts
		(attempt_id, source, import_row_id, op_id, code, op_type, ts_raw, actor, initiator, role,
		 decision, reason, mock_result, rules_version)
		VALUES ($1::uuid,$2,$3::uuid,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb,$14)`,
		attemptID, in.Source, importRow, in.OpID, in.Code, in.OpType, in.TsRaw, in.Actor,
		in.Initiator, in.Role, out.Decision, reason, mock, rules.Version); err != nil {
		return "", err
	}
	switch out.Decision {
	case Accepted:
		if out.bindingNew {
			if _, err := tx.Exec(ctx, `INSERT INTO code_registry(code, item_code) VALUES ($1,$2)`,
				in.Code, out.ItemCode); err != nil {
				return "", err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO operations(op_id, code, op_type, ts, actor, attempt_id)
			VALUES ($1,$2,$3,$4,$5,$6::uuid)`, in.OpID, in.Code, in.OpType, out.ts, in.Actor, attemptID); err != nil {
			return "", err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO codes(code, item_code, status, ts, last_actor)
			VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (code) DO UPDATE SET status = EXCLUDED.status, ts = EXCLUDED.ts, last_actor = EXCLUDED.last_actor`,
			in.Code, out.ItemCode, out.newStatus, out.ts, in.Actor); err != nil {
			return "", err
		}
	case Rejected:
		var opTs *time.Time
		if out.tsOK {
			opTs = &out.ts
		} else if t, ok := rules.ParseTime(in.TsRaw); ok {
			opTs = &t
		}
		itemCode := out.ItemCode
		if itemCode == "" {
			// Best effort: the binding of the code, if any.
			_ = tx.QueryRow(ctx, `SELECT item_code FROM code_registry WHERE code = $1`, in.Code).Scan(&itemCode)
		}
		if itemCode == "" && in.ItemCode != "" {
			itemCode = in.ItemCode
		}
		if _, err := tx.Exec(ctx, `INSERT INTO violations
			(violation_id, source, ref, code, item_code, reason, message, attempt_id, import_row_id, rules_version, op_ts, actor)
			VALUES ($1::uuid,'operations',$2,$3,$4,$5,$6,$7::uuid,$8::uuid,$9,$10,$11)`,
			uuid.NewString(), in.OpID, nullIfEmpty(in.Code), nullIfEmpty(itemCode), out.Reason, out.Message,
			attemptID, importRow, rules.Version, opTs, nullIfEmpty(in.Actor)); err != nil {
			return "", err
		}
	}
	return attemptID, nil
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ProcessImportRow evaluates and records one operations.csv row in its own locked transaction.
func ProcessImportRow(ctx context.Context, tx pgx.Tx, in Input) (Outcome, error) {
	if err := lock(ctx, tx, in); err != nil {
		return Outcome{}, err
	}
	out, err := Evaluate(ctx, tx, in)
	if err != nil {
		return Outcome{}, err
	}
	if _, err := Record(ctx, tx, in, out, nil); err != nil {
		return Outcome{}, err
	}
	return out, nil
}
