package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"marktrace/backend/internal/rules"
	"marktrace/backend/internal/testdb"
)

const (
	gtin = "4601234567893"
	tsA  = "2026-10-01T09:00:00+03:00"
	tsB  = "2026-10-02T09:00:00+03:00"
)

var (
	codeOK  = "01" + rules.NormalizeGTIN(gtin) + "21" + "API001"
	codeNew = "01" + rules.NormalizeGTIN(gtin) + "21" + "API002"
)

type client struct {
	t    *testing.T
	base string
	http *http.Client
}

func newClient(t *testing.T, base string) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, base: base, http: &http.Client{Jar: jar}}
}

func (c *client) do(method, path string, body any) (int, []byte) {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

func (c *client) login(user string) {
	c.t.Helper()
	if st, body := c.do("POST", "/api/auth/login", map[string]string{"username": user, "password": user}); st != 200 {
		c.t.Fatalf("login %s: %d %s", user, st, body)
	}
}

func (c *client) upload(files map[string]string) (int, map[string]any) {
	c.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for field, content := range files {
		fw, _ := mw.CreateFormFile(field, field+".csv")
		fw.Write([]byte(content))
	}
	mw.Close()
	req, _ := http.NewRequest("POST", c.base+"/api/imports", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func decode(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("bad json %s: %v", data, err)
	}
	return m
}

func TestAPIEndToEnd(t *testing.T) {
	pool := testdb.New(t)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"is_valid":true}`))
	}))
	defer mock.Close()

	golden := t.TempDir()
	os.WriteFile(filepath.Join(golden, "status_summary.csv"),
		[]byte("status,count\nissued,0\nintroduced,1\nsold,0\nreturned,0\ndisposed,0\n"), 0o644)
	os.WriteFile(filepath.Join(golden, "violations.csv"),
		[]byte("code,reason,source,ref\n,GTIN_CHECKSUM,items,I9\n"+codeOK+",TRANSITION_NOT_ALLOWED,operations,o3\n"), 0o644)

	srv := httptest.NewServer(New(pool, mock.URL, golden).Handler())
	defer srv.Close()

	anon := newClient(t, srv.URL)
	if st, body := anon.do("GET", "/api/codes", nil); st != 401 || decode(t, body)["error"] != "401" {
		t.Fatalf("anon: %d %s", st, body)
	}
	if st, _ := anon.do("POST", "/api/auth/login", map[string]string{"username": "prod", "password": "x"}); st != 401 {
		t.Fatalf("bad password: %d", st)
	}

	// Auditor: read-only everywhere.
	aud := newClient(t, srv.URL)
	aud.login("auditor")
	st, body := aud.do("POST", "/api/operations", map[string]string{"op_type": "issue", "code": codeOK, "item_code": "I1"})
	if st != 403 || decode(t, body)["error"] != rules.AuthDenied {
		t.Fatalf("auditor operation: %d %s", st, body)
	}
	if st, _ := aud.upload(map[string]string{"items": "item_code,name,gtin\n"}); st != 403 {
		t.Fatalf("auditor import: %d", st)
	}
	if st, body := aud.do("GET", "/api/auth/me", nil); st != 200 || decode(t, body)["role"] != "A4" {
		t.Fatalf("me: %d %s", st, body)
	}

	// Import as qc (A3).
	qc := newClient(t, srv.URL)
	qc.login("qc")
	firstSet := map[string]string{
		"items": "item_code,name,gtin\nI1,Товар,4601234567893\nI9,Плохой,4601234567890\n",
		"codes": "code,item_code,status,ts\n" + codeOK + ",I1,introduced,2026-10-02T06:00:00Z\n",
		"operations": "op_id,code,op_type,ts,actor\n" +
			"o1," + codeOK + ",issue," + tsA + ",prod\n" +
			"o2," + codeOK + ",introduce," + tsB + ",prod\n" +
			"o3," + codeOK + ",return,2026-10-03T09:00:00Z,shop\n",
	}
	st, sess := qc.upload(firstSet)
	if st != 200 || sess["status"] != "completed" {
		t.Fatalf("import: %d %v", st, sess)
	}
	if rej := sess["rejected_rows"].([]any); len(rej) != 2 {
		t.Fatalf("rejected rows %v", rej)
	}
	counters := sess["counters"].(map[string]any)["operations"].(map[string]any)
	if counters["accepted"].(float64) != 2 || counters["rejected"].(float64) != 1 {
		t.Fatalf("counters %v", counters)
	}

	// Golden compare matches the fixture.
	st, body = qc.do("GET", "/api/golden/compare", nil)
	g := decode(t, body)
	if st != 200 || g["percent"].(float64) != 100 {
		t.Fatalf("golden: %s", body)
	}

	// Status report and exports.
	st, body = aud.do("GET", "/api/codes?status=introduced&q=API", nil)
	codes := decode(t, body)
	if st != 200 || codes["total"].(float64) != 1 || codes["counters"].(map[string]any)["introduced"].(float64) != 1 {
		t.Fatalf("codes: %s", body)
	}
	// Counters ignore the status filter (tiles keep the breakdown); total follows it.
	st, body = aud.do("GET", "/api/codes?status=sold", nil)
	codes = decode(t, body)
	if st != 200 || codes["total"].(float64) != 0 || codes["counters"].(map[string]any)["introduced"].(float64) != 1 {
		t.Fatalf("codes status=sold: %s", body)
	}
	// Violation search also matches the message text (GTIN only appears in the I9 message).
	st, body = aud.do("GET", "/api/violations?q=4601234567890", nil)
	if st != 200 || decode(t, body)["total"].(float64) != 1 {
		t.Fatalf("violations q by message: %s", body)
	}
	if st, body := aud.do("GET", "/api/codes?from=2026-10-03", nil); st != 200 || decode(t, body)["total"].(float64) != 0 {
		t.Fatalf("codes from filter: %s", body)
	}
	if st, _ := aud.do("GET", "/api/codes?from=garbage", nil); st != 400 {
		t.Fatalf("bad filter: %d", st)
	}
	_, body = aud.do("GET", "/api/codes/export.csv", nil)
	if string(body) != "code,item_code,status,last_ts\n"+codeOK+",I1,introduced,2026-10-02T06:00:00Z\n" {
		t.Fatalf("export: %q", body)
	}
	_, body = aud.do("GET", "/api/status-summary.csv", nil)
	if string(body) != "status,count\nissued,0\nintroduced,1\nsold,0\nreturned,0\ndisposed,0\n" {
		t.Fatalf("summary: %q", body)
	}
	_, body = aud.do("GET", "/api/violations/export.csv", nil)
	if string(body) != "code,reason,source,ref\n,GTIN_CHECKSUM,items,I9\n"+codeOK+",TRANSITION_NOT_ALLOWED,operations,o3\n" {
		t.Fatalf("violations export: %q", body)
	}

	// Code history.
	st, body = aud.do("GET", "/api/codes/"+codeOK, nil)
	d := decode(t, body)
	if st != 200 || len(d["operations"].([]any)) != 2 || len(d["attempts"].([]any)) != 3 || len(d["violations"].([]any)) != 1 {
		t.Fatalf("detail: %s", body)
	}
	if st, _ := aud.do("GET", "/api/codes/unknown", nil); st != 404 {
		t.Fatalf("unknown code: %d", st)
	}

	// Form operation as prod (A1): issue a new code through the form.
	prod := newClient(t, srv.URL)
	prod.login("prod")
	st, body = prod.do("POST", "/api/operations", map[string]string{"op_type": "issue", "code": codeNew, "item_code": "I1"})
	if r := decode(t, body); st != 200 || r["decision"] != "accepted" || r["code"].(map[string]any)["status"] != "issued" {
		t.Fatalf("form issue: %d %s", st, body)
	}
	st, body = prod.do("POST", "/api/operations", map[string]string{"op_type": "sell", "code": codeNew})
	if st != 403 || decode(t, body)["error"] != rules.AuthDenied {
		t.Fatalf("prod sell: %d %s", st, body)
	}

	// Violations list and resolutions.
	_, body = aud.do("GET", "/api/violations?source=operations&state=open", nil)
	vl := decode(t, body)
	rows := vl["rows"].([]any)
	if len(rows) != 1 || vl["counts"].(map[string]any)[rules.TransitionNotAllowed].(float64) != 1 {
		t.Fatalf("violations: %s", body)
	}
	vid := rows[0].(map[string]any)["violation_id"].(string)
	if st, _ := aud.do("POST", "/api/violations/"+vid+"/resolutions", map[string]string{"action": "close", "comment": "x"}); st != 403 {
		t.Fatalf("auditor resolve: %d", st)
	}
	if st, _ := qc.do("POST", "/api/violations/"+vid+"/resolutions", map[string]string{"action": "close", "comment": ""}); st != 400 {
		t.Fatalf("empty comment: %d", st)
	}
	if st, body := qc.do("POST", "/api/violations/"+vid+"/resolutions", map[string]string{"action": "close", "comment": "Проверено"}); st != 201 {
		t.Fatalf("close: %d %s", st, body)
	}
	if st, _ := qc.do("POST", "/api/violations/"+vid+"/resolutions", map[string]string{"action": "close", "comment": "again"}); st != 409 {
		t.Fatalf("second close: %d", st)
	}
	_, body = aud.do("GET", "/api/violations?state=closed", nil)
	if rows := decode(t, body)["rows"].([]any); len(rows) != 1 || len(rows[0].(map[string]any)["resolutions"].([]any)) != 1 {
		t.Fatalf("closed: %s", body)
	}

	// KPI.
	st, body = aud.do("GET", "/api/kpi", nil)
	k := decode(t, body)
	k1 := k["k1"].(map[string]any)
	if st != 200 || k1["accepted"].(float64) != 3 || k1["rejected"].(float64) != 1 || k1["percent"].(float64) != 75 {
		t.Fatalf("kpi: %s", body)
	}
	if k["k2"].(map[string]any)["percent"].(float64) != 100 || k["k4"].(map[string]any)["last_ms"] == nil {
		t.Fatalf("kpi k2/k4: %s", body)
	}

	// Imports list/detail, rules, items, verify, logout.
	_, body = aud.do("GET", "/api/imports", nil)
	var list []map[string]any
	json.Unmarshal(body, &list)
	if len(list) != 1 {
		t.Fatalf("imports: %s", body)
	}
	if st, _ := aud.do("GET", "/api/imports/"+list[0]["session_id"].(string), nil); st != 200 {
		t.Fatalf("import detail: %d", st)
	}

	// K3: a different full file set is not a re-import; the same set again is (0 new rows).
	k3 := func() map[string]any {
		_, body := aud.do("GET", "/api/kpi", nil)
		return decode(t, body)["k3"].(map[string]any)
	}
	if k := k3(); k["reimport"] != false {
		t.Fatalf("k3 after first import: %v", k)
	}
	if st, sess := qc.upload(map[string]string{
		"items":      "item_code,name,gtin\nI1,Товар,4601234567893\n",
		"codes":      "code,item_code,status,ts\n",
		"operations": "op_id,code,op_type,ts,actor\n",
	}); st != 200 || sess["status"] != "completed" {
		t.Fatalf("second set: %d %v", st, sess)
	}
	if k := k3(); k["reimport"] != false {
		t.Fatalf("k3 after a different set: %v", k)
	}
	if st, sess := qc.upload(firstSet); st != 200 || sess["rows_created"].(float64) != 0 {
		t.Fatalf("re-import: %d %v", st, sess)
	}
	if k := k3(); k["reimport"] != true || k["rows_created"].(float64) != 0 {
		t.Fatalf("k3 after re-import: %v", k)
	}

	if st, body := aud.do("GET", "/api/rules", nil); st != 200 || decode(t, body)["rules_version"] != rules.Version {
		t.Fatalf("rules: %s", body)
	}
	if _, body := aud.do("GET", "/api/items", nil); !strings.Contains(string(body), `"item_code":"I1"`) {
		t.Fatalf("items: %s", body)
	}
	if _, body := aud.do("POST", "/api/verify", map[string]string{"code": codeOK}); decode(t, body)["is_valid"] != true {
		t.Fatalf("verify: %s", body)
	}
	mock.Close()
	if _, body := aud.do("POST", "/api/verify", map[string]string{"code": codeOK}); decode(t, body)["reason"] != rules.MockUnavailable {
		t.Fatalf("verify unavailable: %s", body)
	}
	aud.do("POST", "/api/auth/logout", nil)
	if st, _ := aud.do("GET", "/api/auth/me", nil); st != 401 {
		t.Fatalf("after logout: %d", st)
	}
	if st, body := aud.do("GET", "/api/nope", nil); st != 404 || decode(t, body)["error"] != "404" {
		t.Fatalf("unknown path: %d %s", st, body)
	}
}
