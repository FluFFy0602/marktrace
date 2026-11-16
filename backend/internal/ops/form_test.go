package ops

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"marktrace/backend/internal/rules"
	"marktrace/backend/internal/testdb"
)

const (
	gtinA = "4601234567893"
	gtinB = "04601234567886" // another valid GTIN-14
)

var (
	codeA1 = "01" + rules.NormalizeGTIN(gtinA) + "21" + "FORM01"
	codeA2 = "01" + rules.NormalizeGTIN(gtinA) + "21" + "FORM02"
	codeB1 = "01" + gtinB + "21" + "FORM03"
)

// mockServer answers according to mode: normal, invalid, error, garbage, slow.
func mockServer(t *testing.T, mode *atomic.Value) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cz/verify" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		switch mode.Load().(string) {
		case "invalid":
			json.NewEncoder(w).Encode(map[string]any{"is_valid": false, "reason": "UNKNOWN_CODE"})
		case "error":
			http.Error(w, "boom", http.StatusInternalServerError)
		case "garbage":
			w.Write([]byte(`{}`))
		case "slow":
			time.Sleep(3 * time.Second)
			json.NewEncoder(w).Encode(map[string]any{"is_valid": true})
		default:
			json.NewEncoder(w).Encode(map[string]any{"is_valid": true})
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newService(t *testing.T) (*Service, *atomic.Value) {
	pool := testdb.New(t)
	ctx := context.Background()
	if rules.CheckGTIN(gtinB) != "" {
		t.Fatalf("fixture gtinB invalid: %s", rules.CheckGTIN(gtinB))
	}
	if _, err := pool.Exec(ctx, `INSERT INTO items(item_code, name, gtin) VALUES ('A','Товар A',$1),('B','Товар B',$2)`, gtinA, gtinB); err != nil {
		t.Fatal(err)
	}
	mode := &atomic.Value{}
	mode.Store("normal")
	srv := mockServer(t, mode)
	return &Service{Pool: pool, Mock: NewMockClient(srv.URL)}, mode
}

func submit(t *testing.T, s *Service, user, role string, req FormRequest) FormResponse {
	t.Helper()
	resp, err := s.Submit(context.Background(), user, role, req)
	if err != nil {
		t.Fatalf("submit %+v: %v", req, err)
	}
	return resp
}

func expect(t *testing.T, resp FormResponse, decision, reason string) {
	t.Helper()
	if resp.Decision != decision || resp.Reason != reason {
		t.Fatalf("got %s/%s (%s), want %s/%s", resp.Decision, resp.Reason, resp.Message, decision, reason)
	}
}

func TestFormLifecycle(t *testing.T) {
	s, _ := newService(t)
	r := submit(t, s, "prod", "A1", FormRequest{OpType: "issue", Code: codeA1, ItemCode: "A", Ts: "2026-11-01T10:00:00+03:00"})
	expect(t, r, Accepted, "")
	if r.Code == nil || r.Code.Status != "issued" || r.Code.LastActor != "prod" || r.Code.LastTs != "2026-11-01T07:00:00Z" {
		t.Fatalf("code row %+v", r.Code)
	}
	expect(t, submit(t, s, "prod", "A1", FormRequest{OpType: "introduce", Code: codeA1, Ts: "2026-11-01T11:00:00+03:00"}), Accepted, "")
	expect(t, submit(t, s, "retail", "A2", FormRequest{OpType: "sell", Code: codeA1, Ts: "2026-11-02T11:00:00+03:00"}), Accepted, "")
	r = submit(t, s, "retail", "A2", FormRequest{OpType: "return", Code: codeA1, Ts: "2026-11-03T11:00:00+03:00", OpID: "ret-1"})
	expect(t, r, Accepted, "")
	if r.Code.Status != "returned" || r.OpID != "ret-1" {
		t.Fatalf("after return %+v", r.Code)
	}
	// Exact repeat of an accepted op_id is a duplicate.
	expect(t, submit(t, s, "retail", "A2", FormRequest{OpType: "return", Code: codeA1, Ts: "2026-11-03T11:00:00+03:00", OpID: "ret-1"}), Duplicate, "")
	// Same op_id, different content.
	expect(t, submit(t, s, "retail", "A2", FormRequest{OpType: "return", Code: codeA1, Ts: "2026-11-04T11:00:00+03:00", OpID: "ret-1"}), Rejected, rules.IdempotencyConflict)
	// Transition and time rules.
	expect(t, submit(t, s, "retail", "A2", FormRequest{OpType: "return", Code: codeA1, Ts: "2026-11-05T11:00:00+03:00"}), Rejected, rules.TransitionNotAllowed)
	expect(t, submit(t, s, "retail", "A2", FormRequest{OpType: "sell", Code: codeA1, Ts: "2026-11-01T12:00:00+03:00"}), Rejected, rules.TimeReversed)
	// Default ts = now: accepted after all earlier ones.
	expect(t, submit(t, s, "retail", "A2", FormRequest{OpType: "sell", Code: codeA1}), Accepted, "")

	var ops, attempts, viol int
	s.Pool.QueryRow(context.Background(), `SELECT count(*) FROM operations`).Scan(&ops)
	s.Pool.QueryRow(context.Background(), `SELECT count(*) FROM operation_attempts WHERE source='form'`).Scan(&attempts)
	s.Pool.QueryRow(context.Background(), `SELECT count(*) FROM violations WHERE source='operations'`).Scan(&viol)
	if ops != 5 || attempts != 9 || viol != 3 {
		t.Fatalf("ops=%d attempts=%d violations=%d", ops, attempts, viol)
	}
}

func TestFormBindingRules(t *testing.T) {
	s, _ := newService(t)
	expect(t, submit(t, s, "prod", "A1", FormRequest{OpType: "issue", Code: codeA1}), Rejected, rules.RequiredField)
	expect(t, submit(t, s, "prod", "A1", FormRequest{OpType: "issue", Code: codeA1, ItemCode: "NOPE"}), Rejected, rules.ItemNotFound)
	expect(t, submit(t, s, "prod", "A1", FormRequest{OpType: "issue", Code: codeA1, ItemCode: "B"}), Rejected, rules.GTINMismatch)
	expect(t, submit(t, s, "prod", "A1", FormRequest{OpType: "issue", Code: "bad", ItemCode: "A"}), Rejected, rules.CodeFormat)
	expect(t, submit(t, s, "prod", "A1", FormRequest{OpType: "issue", Code: codeA1, ItemCode: "A", Ts: "2026-11-01 10:00"}), Rejected, rules.TimeFormat)
	expect(t, submit(t, s, "prod", "A1", FormRequest{OpType: "transfer", Code: codeA1}), Rejected, rules.ValueFormat)
	expect(t, submit(t, s, "prod", "A1", FormRequest{OpType: "introduce", Code: codeA2}), Rejected, rules.CodeNotFound)
	expect(t, submit(t, s, "prod", "A1", FormRequest{OpType: "issue", Code: codeB1, ItemCode: "B"}), Accepted, "")
	// The binding exists: issuing again is DUPLICATE_CODE (bound to another item / already issued).
	expect(t, submit(t, s, "prod", "A1", FormRequest{OpType: "issue", Code: codeB1, ItemCode: "A"}), Rejected, rules.DuplicateCode)
	expect(t, submit(t, s, "prod", "A1", FormRequest{OpType: "issue", Code: codeB1, ItemCode: "B", Ts: "2030-01-01T00:00:00Z"}), Rejected, rules.DuplicateCode)
	var expected *string
	s.Pool.QueryRow(context.Background(), `SELECT expected_status FROM code_registry WHERE code=$1`, codeB1).Scan(&expected)
	if expected != nil {
		t.Fatal("form binding must have NULL expected_status")
	}
}

func TestFormRoles(t *testing.T) {
	s, _ := newService(t)
	cases := []struct {
		role, op string
		ok       bool
	}{
		{"A1", "issue", true}, {"A1", "introduce", true}, {"A1", "sell", false}, {"A1", "dispose", false},
		{"A2", "sell", true}, {"A2", "return", true}, {"A2", "dispose", true}, {"A2", "issue", false},
		{"A3", "issue", false}, {"A4", "sell", false}, {"admin", "issue", true}, {"admin", "dispose", true},
	}
	for _, c := range cases {
		if RoleAllowed(c.role, c.op) != c.ok {
			t.Errorf("RoleAllowed(%s,%s) != %v", c.role, c.op, c.ok)
		}
	}
	_, err := s.Submit(context.Background(), "auditor", "A4", FormRequest{OpType: "sell", Code: codeA1})
	if !errors.Is(err, ErrAuthDenied) {
		t.Fatalf("auditor: %v", err)
	}
	var n int
	s.Pool.QueryRow(context.Background(), `SELECT count(*) FROM operation_attempts`).Scan(&n)
	if n != 0 {
		t.Fatal("AUTH_DENIED must not be recorded as an attempt")
	}
}

func TestFormMockFailures(t *testing.T) {
	s, mode := newService(t)
	for _, c := range []struct{ mode, reason string }{
		{"invalid", rules.MockInvalid},
		{"error", rules.MockUnavailable},
		{"garbage", rules.MockUnavailable},
		{"slow", rules.MockUnavailable},
	} {
		mode.Store(c.mode)
		r := submit(t, s, "prod", "A1", FormRequest{OpType: "issue", Code: codeA1, ItemCode: "A"})
		expect(t, r, Rejected, c.reason)
		if c.mode == "invalid" && r.Message != "Мок-сервис ЦРПТ отклонил код: причина: UNKNOWN_CODE" {
			t.Errorf("message %q", r.Message)
		}
	}
	// Nothing was applied; mock answer is stored on the attempt.
	var codes, withMock int
	s.Pool.QueryRow(context.Background(), `SELECT count(*) FROM codes`).Scan(&codes)
	s.Pool.QueryRow(context.Background(), `SELECT count(*) FROM operation_attempts WHERE mock_result IS NOT NULL`).Scan(&withMock)
	if codes != 0 || withMock != 4 {
		t.Fatalf("codes=%d withMock=%d", codes, withMock)
	}
	// Unreachable mock.
	s.Mock = NewMockClient("http://127.0.0.1:1")
	expect(t, submit(t, s, "prod", "A1", FormRequest{OpType: "issue", Code: codeA1, ItemCode: "A"}), Rejected, rules.MockUnavailable)
	// Recovery.
	mode.Store("normal")
	s.Mock = NewMockClient(mockServer(t, mode).URL)
	expect(t, submit(t, s, "prod", "A1", FormRequest{OpType: "issue", Code: codeA1, ItemCode: "A"}), Accepted, "")
}
