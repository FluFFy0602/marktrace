package ops

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"marktrace/backend/internal/rules"
)

// ErrAuthDenied is returned when the role may not perform the operation type.
var ErrAuthDenied = errors.New("auth denied")

// FormRequest is the body of POST /api/operations.
type FormRequest struct {
	OpType   string `json:"op_type"`
	Code     string `json:"code"`
	ItemCode string `json:"item_code"`
	Ts       string `json:"ts"`
	Actor    string `json:"actor"`
	OpID     string `json:"op_id"`
}

// CodeRow is the current projection of a code.
type CodeRow struct {
	Code      string `json:"code"`
	ItemCode  string `json:"item_code"`
	Status    string `json:"status"`
	LastTs    string `json:"last_ts"`
	LastActor string `json:"last_actor"`
}

// FormResponse is the decision returned to the form.
type FormResponse struct {
	AttemptID string   `json:"attempt_id"`
	OpID      string   `json:"op_id"`
	Decision  string   `json:"decision"`
	Reason    string   `json:"reason,omitempty"`
	Message   string   `json:"message,omitempty"`
	Code      *CodeRow `json:"code,omitempty"`
}

// Service handles form operations.
type Service struct {
	Pool *pgxpool.Pool
	Mock *MockClient
	Now  func() time.Time
}

// RoleAllowed implements the role check of SPEC §3.
func RoleAllowed(role, opType string) bool {
	if role == "admin" {
		return true
	}
	switch opType {
	case rules.OpIssue, rules.OpIntroduce:
		return role == "A1"
	case rules.OpSell, rules.OpReturn, rules.OpDispose:
		return role == "A2"
	}
	// Unknown op_type: operators pass the role check and get VALUE_FORMAT.
	return role == "A1" || role == "A2"
}

// Submit processes one form operation (SPEC §3).
func (s *Service) Submit(ctx context.Context, username, role string, req FormRequest) (FormResponse, error) {
	if !RoleAllowed(role, req.OpType) {
		return FormResponse{}, ErrAuthDenied
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	if req.OpID == "" {
		req.OpID = uuid.NewString()
	}
	if req.Ts == "" {
		req.Ts = rules.FormatTime(now())
	}
	if req.Actor == "" {
		req.Actor = username
	}
	in := Input{
		OpID: req.OpID, Code: req.Code, OpType: req.OpType, TsRaw: req.Ts, Actor: req.Actor,
		ItemCode: req.ItemCode, FormIssue: true,
		Source: "form", Initiator: username, Role: role,
	}

	// Pre-check without locks; the mock is called only for candidates.
	out, err := Evaluate(ctx, s.Pool, in)
	if err != nil {
		return FormResponse{}, err
	}
	var mockRaw []byte
	if out.Decision == Accepted {
		res := s.Mock.Verify(ctx, in.Code, out.GTIN)
		mockRaw = res.Raw
		switch {
		case !res.Available:
			out = withReason(out, rules.MockUnavailable, "")
		case !res.IsValid:
			out = withReason(out, rules.MockInvalid, fmt.Sprintf("причина: %s", res.Reason))
		}
	}

	var attemptID string
	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := lock(ctx, tx, in); err != nil {
			return err
		}
		if out.Decision == Accepted {
			// Re-check state under lock: something may have changed during the mock call.
			fresh, err := Evaluate(ctx, tx, in)
			if err != nil {
				return err
			}
			out = fresh
		}
		id, err := Record(ctx, tx, in, out, mockRaw)
		attemptID = id
		return err
	})
	if err != nil {
		return FormResponse{}, err
	}
	resp := FormResponse{AttemptID: attemptID, OpID: in.OpID, Decision: out.Decision, Reason: out.Reason, Message: out.Message}
	if out.Decision == Duplicate {
		resp.Message = "Повтор: операция уже учтена, изменений нет"
	}
	if row, err := LoadCodeRow(ctx, s.Pool, in.Code); err == nil {
		resp.Code = row
	}
	return resp, nil
}

func withReason(o Outcome, reason, detail string) Outcome {
	o.Decision = Rejected
	o.Reason = reason
	o.Message = rules.Message(reason, detail)
	return o
}

// LoadCodeRow returns the projection of a code, or nil when it has no history.
func LoadCodeRow(ctx context.Context, db interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, code string) (*CodeRow, error) {
	var r CodeRow
	var ts time.Time
	err := db.QueryRow(ctx, `SELECT code, item_code, status, ts, last_actor FROM codes WHERE code = $1`, code).
		Scan(&r.Code, &r.ItemCode, &r.Status, &ts, &r.LastActor)
	if err != nil {
		return nil, err
	}
	r.LastTs = rules.FormatTime(ts)
	return &r, nil
}
