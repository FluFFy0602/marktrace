package ops

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"marktrace/backend/internal/rules"
)

// MaxBatch caps the number of codes issued by one request.
const MaxBatch = 1000

// serialAlphabet is the character set of the 6-char serial number (SPEC §1.2 mask).
const serialAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// SerialLen is the length of the serial number in a code.
const SerialLen = 6

// NewSerial returns a uniformly random serial number read from r (crypto/rand.Reader in
// production). Bytes ≥ 248 are dropped so that byte % 62 has no modulo bias.
func NewSerial(r io.Reader) (string, error) {
	out := make([]byte, 0, SerialLen)
	buf := make([]byte, 16)
	for len(out) < SerialLen {
		if _, err := io.ReadFull(r, buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if b >= 248 { // 248 = 4 * 62
				continue
			}
			out = append(out, serialAlphabet[int(b)%len(serialAlphabet)])
			if len(out) == SerialLen {
				break
			}
		}
	}
	return string(out), nil
}

// BatchRequest is the body of POST /api/issue-batches.
type BatchRequest struct {
	// RequestID is the client's idempotency key (UUID): the same request returns the same batch.
	RequestID string `json:"request_id"`
	ItemCode  string `json:"item_code"`
	Quantity  int    `json:"quantity"`
}

// BatchResponse describes an issued batch.
type BatchResponse struct {
	BatchID  string    `json:"batch_id"`
	ItemCode string    `json:"item_code"`
	ItemName string    `json:"item_name"`
	GTIN     string    `json:"gtin"`
	Quantity int       `json:"quantity"`
	Ts       string    `json:"ts"`
	Actor    string    `json:"actor"`
	Replayed bool      `json:"replayed"`
	Codes    []CodeRow `json:"codes"`
}

// BatchError is a refusal with an HTTP status, a reason code and a Russian message.
type BatchError struct {
	Status  int
	Reason  string
	Message string
}

func (e *BatchError) Error() string { return e.Reason + ": " + e.Message }

func batchErr(status int, reason, detail string) *BatchError {
	return &BatchError{Status: status, Reason: reason, Message: rules.Message(reason, detail)}
}

// IssueBatch creates req.Quantity new codes for one item and records an issue operation for
// each, all or nothing. The codes are registered in the mock "ЦРПТ" first, then every issue
// passes the same checks and mock verification as a form operation.
func (s *Service) IssueBatch(ctx context.Context, username, role string, req BatchRequest) (BatchResponse, error) {
	if !RoleAllowed(role, rules.OpIssue) {
		return BatchResponse{}, ErrAuthDenied
	}
	switch {
	case req.RequestID == "":
		return BatchResponse{}, batchErr(http.StatusBadRequest, rules.RequiredField, "request_id")
	case req.ItemCode == "":
		return BatchResponse{}, batchErr(http.StatusBadRequest, rules.RequiredField, "item_code")
	}
	batchID, err := uuid.Parse(req.RequestID)
	if err != nil {
		return BatchResponse{}, batchErr(http.StatusBadRequest, rules.ValueFormat, "request_id должен быть UUID")
	}
	if req.Quantity < 1 || req.Quantity > MaxBatch {
		return BatchResponse{}, batchErr(http.StatusBadRequest, rules.ValueFormat,
			fmt.Sprintf("количество должно быть от 1 до %d", MaxBatch))
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}

	var resp BatchResponse
	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('batch:' || $1::text, 0))`, batchID.String()); err != nil {
			return err
		}
		prev, err := loadBatch(ctx, tx, batchID.String())
		if err != nil {
			return err
		}
		if prev != nil {
			if prev.ItemCode != req.ItemCode || prev.Quantity != req.Quantity {
				return batchErr(http.StatusConflict, rules.IdempotencyConflict,
					fmt.Sprintf("партия %s уже выпущена: товар %s, %d шт.", batchID, prev.ItemCode, prev.Quantity))
			}
			prev.Replayed = true
			resp = *prev
			return nil
		}

		var name, gtin string
		err = tx.QueryRow(ctx, `SELECT name, gtin FROM items WHERE item_code = $1`, req.ItemCode).Scan(&name, &gtin)
		if errors.Is(err, pgx.ErrNoRows) {
			return batchErr(http.StatusUnprocessableEntity, rules.ItemNotFound, req.ItemCode)
		}
		if err != nil {
			return err
		}
		if reason := rules.CheckGTIN(gtin); reason != "" {
			return batchErr(http.StatusUnprocessableEntity, reason, fmt.Sprintf("товар %s, GTIN %s", req.ItemCode, gtin))
		}
		codes, err := newCodes(ctx, tx, rules.NormalizeGTIN(gtin), req.Quantity)
		if err != nil {
			return err
		}
		if err := s.Mock.Register(ctx, codes); err != nil {
			return batchErr(http.StatusServiceUnavailable, rules.MockUnavailable, "регистрация кодов: "+err.Error())
		}

		at := now().Truncate(time.Second)
		ts := rules.FormatTime(at)
		resp = BatchResponse{
			BatchID: batchID.String(), ItemCode: req.ItemCode, ItemName: name, GTIN: rules.NormalizeGTIN(gtin),
			Quantity: req.Quantity, Ts: ts, Actor: username, Codes: make([]CodeRow, 0, len(codes)),
		}
		if _, err := tx.Exec(ctx, `INSERT INTO issue_batches(batch_id, item_code, quantity, ts, actor)
			VALUES ($1::uuid, $2, $3, $4, $5)`, batchID.String(), req.ItemCode, req.Quantity, at, username); err != nil {
			return err
		}
		for i, code := range codes {
			in := Input{
				OpID: uuid.NewString(), Code: code, OpType: rules.OpIssue, TsRaw: ts, Actor: username,
				ItemCode: req.ItemCode, FormIssue: true, Source: "form", Initiator: username, Role: role,
			}
			if err := lock(ctx, tx, in); err != nil {
				return err
			}
			out, err := Evaluate(ctx, tx, in)
			if err != nil {
				return err
			}
			if out.Decision != Accepted {
				return &BatchError{Status: http.StatusConflict, Reason: out.Reason,
					Message: fmt.Sprintf("код %s: %s", code, out.Message)}
			}
			res := s.Mock.Verify(ctx, code, out.GTIN)
			switch {
			case !res.Available:
				return batchErr(http.StatusServiceUnavailable, rules.MockUnavailable, "проверка кода "+code)
			case !res.IsValid:
				return batchErr(http.StatusBadGateway, rules.MockInvalid, fmt.Sprintf("код %s, причина: %s", code, res.Reason))
			}
			if _, err := Record(ctx, tx, in, out, res.Raw); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO issue_batch_codes(batch_id, pos, code) VALUES ($1::uuid, $2, $3)`,
				batchID.String(), i+1, code); err != nil {
				return err
			}
			resp.Codes = append(resp.Codes, CodeRow{Code: code, ItemCode: req.ItemCode, Status: rules.StatusIssued,
				LastTs: ts, LastActor: username})
		}
		return nil
	})
	if err != nil {
		return BatchResponse{}, err
	}
	return resp, nil
}

// newCodes generates n codes 01 + gtin14 + 21 + serial that are unique within the batch and
// unknown to the code registry.
func newCodes(ctx context.Context, tx pgx.Tx, gtin14 string, n int) ([]string, error) {
	seen := make(map[string]bool, n)
	codes := make([]string, 0, n)
	for len(codes) < n {
		need := n - len(codes)
		cand := make([]string, 0, need)
		for len(cand) < need {
			serial, err := NewSerial(rand.Reader)
			if err != nil {
				return nil, err
			}
			c := "01" + gtin14 + "21" + serial
			if !seen[c] {
				seen[c] = true
				cand = append(cand, c)
			}
		}
		taken := map[string]bool{}
		rows, err := tx.Query(ctx, `SELECT code FROM code_registry WHERE code = ANY($1)`, cand)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				rows.Close()
				return nil, err
			}
			taken[c] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		for _, c := range cand {
			if !taken[c] {
				codes = append(codes, c)
			}
		}
	}
	return codes, nil
}

// loadBatch returns a stored batch with the current state of its codes, or nil.
func loadBatch(ctx context.Context, tx pgx.Tx, batchID string) (*BatchResponse, error) {
	b := BatchResponse{BatchID: batchID}
	var ts time.Time
	err := tx.QueryRow(ctx, `SELECT b.item_code, i.name, i.gtin, b.quantity, b.ts, b.actor
		FROM issue_batches b JOIN items i USING (item_code) WHERE b.batch_id = $1::uuid`, batchID).
		Scan(&b.ItemCode, &b.ItemName, &b.GTIN, &b.Quantity, &ts, &b.Actor)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b.GTIN = rules.NormalizeGTIN(b.GTIN)
	b.Ts = rules.FormatTime(ts)
	rows, err := tx.Query(ctx, `SELECT c.code, c.item_code, c.status, c.ts, c.last_actor
		FROM issue_batch_codes bc JOIN codes c USING (code) WHERE bc.batch_id = $1::uuid ORDER BY bc.pos`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	b.Codes = []CodeRow{}
	for rows.Next() {
		var r CodeRow
		var cts time.Time
		if err := rows.Scan(&r.Code, &r.ItemCode, &r.Status, &cts, &r.LastActor); err != nil {
			return nil, err
		}
		r.LastTs = rules.FormatTime(cts)
		b.Codes = append(b.Codes, r)
	}
	return &b, rows.Err()
}
