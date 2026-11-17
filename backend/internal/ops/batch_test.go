package ops

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"marktrace/backend/internal/rules"
	"marktrace/backend/internal/testdb"
)

// registryMock is a stand-in for the mock ЦРПТ that keeps a registry:
// /cz/register adds codes, /cz/verify accepts only registered ones.
type registryMock struct {
	mu        sync.Mutex
	codes     map[string]bool
	down      atomic.Bool
	registers atomic.Int32
	verifies  atomic.Int32
}

func (m *registryMock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if m.down.Load() {
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	switch r.URL.Path {
	case "/cz/register":
		m.registers.Add(1)
		var req struct{ Codes []string }
		json.NewDecoder(r.Body).Decode(&req)
		m.mu.Lock()
		n := 0
		for _, c := range req.Codes {
			if !m.codes[c] {
				m.codes[c] = true
				n++
			}
		}
		m.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]int{"registered": n})
	case "/cz/verify":
		m.verifies.Add(1)
		var req struct{ Code, GTIN string }
		json.NewDecoder(r.Body).Decode(&req)
		m.mu.Lock()
		ok := m.codes[req.Code]
		m.mu.Unlock()
		if !ok {
			json.NewEncoder(w).Encode(map[string]any{"is_valid": false, "reason": "UNKNOWN_CODE"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"is_valid": true})
	default:
		http.NotFound(w, r)
	}
}

func newBatchService(t *testing.T) (*Service, *registryMock) {
	pool := testdb.New(t)
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO items(item_code, name, gtin) VALUES ('A','Товар A',$1),('B','Товар B',$2)`, gtinA, gtinB); err != nil {
		t.Fatal(err)
	}
	m := &registryMock{codes: map[string]bool{}}
	srv := httptest.NewServer(m)
	t.Cleanup(srv.Close)
	return &Service{Pool: pool, Mock: NewMockClient(srv.URL)}, m
}

func count(t *testing.T, s *Service, sql string) int {
	t.Helper()
	var n int
	if err := s.Pool.QueryRow(context.Background(), sql).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// tableCounts snapshots every table a batch writes to.
func tableCounts(t *testing.T, s *Service) [6]int {
	return [6]int{
		count(t, s, `SELECT count(*) FROM code_registry`),
		count(t, s, `SELECT count(*) FROM codes`),
		count(t, s, `SELECT count(*) FROM operations`),
		count(t, s, `SELECT count(*) FROM operation_attempts`),
		count(t, s, `SELECT count(*) FROM issue_batches`),
		count(t, s, `SELECT count(*) FROM issue_batch_codes`),
	}
}

func wantBatchErr(t *testing.T, err error, status int, reason string) {
	t.Helper()
	var be *BatchError
	if !errors.As(err, &be) || be.Status != status || be.Reason != reason {
		t.Fatalf("got %v, want %d %s", err, status, reason)
	}
}

func TestNewSerial(t *testing.T) {
	re := regexp.MustCompile(`^[A-Za-z0-9]{6}$`)
	seen := map[string]bool{}
	for i := 0; i < 2000; i++ {
		s, err := NewSerial(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if !re.MatchString(s) {
			t.Fatalf("serial %q does not match", s)
		}
		seen[s] = true
	}
	if len(seen) < 1990 {
		t.Errorf("only %d distinct serials of 2000", len(seen))
	}
	// Bytes 248..255 are skipped (no modulo bias); the rest map onto the alphabet in order.
	src := bytes.NewReader(append([]byte{255, 0, 248, 25, 26, 51, 52, 61, 62}, make([]byte, 7)...))
	s, err := NewSerial(src)
	if err != nil {
		t.Fatal(err)
	}
	if s != "AZaz09" {
		t.Errorf("got %q, want AZaz09", s)
	}
}

func TestIssueBatchOfTen(t *testing.T) {
	s, m := newBatchService(t)
	ctx := context.Background()
	base := time.Now()
	s.Now = func() time.Time { return base }
	req := BatchRequest{RequestID: uuid.NewString(), ItemCode: "A", Quantity: 10}

	resp, err := s.IssueBatch(ctx, "prod", "A1", req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Replayed || resp.Quantity != 10 || len(resp.Codes) != 10 || resp.GTIN != "0"+gtinA {
		t.Fatalf("resp = %+v", resp)
	}
	serials := map[string]bool{}
	for _, c := range resp.Codes {
		gtin, ok := rules.ParseCode(c.Code)
		if !ok || len(c.Code) != 24 {
			t.Fatalf("code %q does not match the mask", c.Code)
		}
		if gtin != "0"+gtinA || c.Code[:18] != "01"+"0"+gtinA+"21" {
			t.Errorf("code %q: GTIN %s, want 0%s", c.Code, gtin, gtinA)
		}
		serials[c.Code[18:]] = true
		if c.Status != rules.StatusIssued || c.ItemCode != "A" || c.LastActor != "prod" {
			t.Errorf("row = %+v", c)
		}
		if !m.codes[c.Code] {
			t.Errorf("code %s not registered in the mock", c.Code)
		}
	}
	if len(serials) != 10 {
		t.Errorf("serials not distinct: %v", serials)
	}
	if got := tableCounts(t, s); got != [6]int{10, 10, 10, 10, 1, 10} {
		t.Errorf("counts after issue = %v", got)
	}
	if m.registers.Load() != 1 || m.verifies.Load() != 10 {
		t.Errorf("mock calls: register %d, verify %d", m.registers.Load(), m.verifies.Load())
	}
	if n := count(t, s, `SELECT count(*) FROM codes WHERE status = 'issued' AND item_code = 'A'`); n != 10 {
		t.Errorf("issued codes in projection = %d", n)
	}

	// The same request again: the same batch, nothing new anywhere.
	again, err := s.IssueBatch(ctx, "prod", "A1", req)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Replayed || again.BatchID != resp.BatchID || again.Ts != resp.Ts || len(again.Codes) != 10 {
		t.Fatalf("replay = %+v", again)
	}
	for i := range resp.Codes {
		if again.Codes[i] != resp.Codes[i] {
			t.Errorf("replay code %d = %+v, want %+v", i, again.Codes[i], resp.Codes[i])
		}
	}
	if got := tableCounts(t, s); got != [6]int{10, 10, 10, 10, 1, 10} {
		t.Errorf("counts after replay = %v", got)
	}
	if m.registers.Load() != 1 {
		t.Errorf("replay registered again")
	}

	// Same key, different content.
	_, err = s.IssueBatch(ctx, "prod", "A1", BatchRequest{RequestID: req.RequestID, ItemCode: "A", Quantity: 11})
	wantBatchErr(t, err, http.StatusConflict, rules.IdempotencyConflict)

	// A replay shows the current state of the codes.
	s.Now = func() time.Time { return base.Add(time.Minute) }
	op := submit(t, s, "prod", "A1", FormRequest{OpType: rules.OpIntroduce, Code: resp.Codes[0].Code})
	expect(t, op, Accepted, "")
	again, err = s.IssueBatch(ctx, "prod", "A1", req)
	if err != nil {
		t.Fatal(err)
	}
	if again.Codes[0].Status != rules.StatusIntroduced {
		t.Errorf("replayed status = %s, want introduced", again.Codes[0].Status)
	}

	// A second batch of the same item gets new codes.
	other, err := s.IssueBatch(ctx, "prod", "A1", BatchRequest{RequestID: uuid.NewString(), ItemCode: "A", Quantity: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range other.Codes {
		for _, p := range resp.Codes {
			if c.Code == p.Code {
				t.Fatalf("code %s reused across batches", c.Code)
			}
		}
	}
}

func TestIssueBatchGTIN14(t *testing.T) {
	s, _ := newBatchService(t)
	resp, err := s.IssueBatch(context.Background(), "admin", "admin",
		BatchRequest{RequestID: uuid.NewString(), ItemCode: "B", Quantity: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.Codes[0].Code[:18]; got != "01"+gtinB+"21" {
		t.Errorf("prefix = %s", got)
	}
}

func TestIssueBatchMockDownIsAtomic(t *testing.T) {
	s, m := newBatchService(t)
	req := BatchRequest{RequestID: uuid.NewString(), ItemCode: "A", Quantity: 3}
	m.down.Store(true)
	_, err := s.IssueBatch(context.Background(), "prod", "A1", req)
	wantBatchErr(t, err, http.StatusServiceUnavailable, rules.MockUnavailable)
	if got := tableCounts(t, s); got != [6]int{} {
		t.Fatalf("partial batch stored: %v", got)
	}
	// Retrying the same request once the mock is back issues the batch.
	m.down.Store(false)
	resp, err := s.IssueBatch(context.Background(), "prod", "A1", req)
	if err != nil || resp.Replayed || len(resp.Codes) != 3 {
		t.Fatalf("retry: %+v %v", resp, err)
	}
}

func TestIssueBatchValidation(t *testing.T) {
	s, _ := newBatchService(t)
	ctx := context.Background()
	id := uuid.NewString()
	cases := []struct {
		req    BatchRequest
		status int
		reason string
	}{
		{BatchRequest{ItemCode: "A", Quantity: 1}, http.StatusBadRequest, rules.RequiredField},
		{BatchRequest{RequestID: "batch-1", ItemCode: "A", Quantity: 1}, http.StatusBadRequest, rules.ValueFormat},
		{BatchRequest{RequestID: id, Quantity: 1}, http.StatusBadRequest, rules.RequiredField},
		{BatchRequest{RequestID: id, ItemCode: "A", Quantity: 0}, http.StatusBadRequest, rules.ValueFormat},
		{BatchRequest{RequestID: id, ItemCode: "A", Quantity: MaxBatch + 1}, http.StatusBadRequest, rules.ValueFormat},
		{BatchRequest{RequestID: id, ItemCode: "NOPE", Quantity: 1}, http.StatusUnprocessableEntity, rules.ItemNotFound},
	}
	for _, c := range cases {
		_, err := s.IssueBatch(ctx, "prod", "A1", c.req)
		wantBatchErr(t, err, c.status, c.reason)
	}
	for _, role := range []string{"A2", "A3", "A4"} {
		if _, err := s.IssueBatch(ctx, "u", role, BatchRequest{RequestID: id, ItemCode: "A", Quantity: 1}); !errors.Is(err, ErrAuthDenied) {
			t.Errorf("role %s: got %v, want ErrAuthDenied", role, err)
		}
	}
	if got := tableCounts(t, s); got != [6]int{} {
		t.Errorf("rejected requests stored data: %v", got)
	}
}
