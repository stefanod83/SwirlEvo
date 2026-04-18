package deploy_agent

import (
	"encoding/json"
	"html/template"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cuigh/swirl/biz"
)

// -- IP allow-list middleware --------------------------------------------------

func mustCIDR(t *testing.T, s string) *net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		t.Fatalf("ParseCIDR %q: %v", s, err)
	}
	return n
}

func TestIPAllowMiddleware_AllowsListed(t *testing.T) {
	allow := []*net.IPNet{mustCIDR(t, "10.0.0.0/24")}
	handler := ipAllowMiddleware(allow, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.5:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for listed IP, got %d", rec.Code)
	}
}

func TestIPAllowMiddleware_BlocksOutsider(t *testing.T) {
	allow := []*net.IPNet{mustCIDR(t, "10.0.0.0/24")}
	handler := ipAllowMiddleware(allow, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be invoked for blocked IP")
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.100:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for blocked IP, got %d", rec.Code)
	}
}

func TestIPAllowMiddleware_EmptyListDefaultsLocalhost(t *testing.T) {
	// resolveAllowList is the public entry that injects the default;
	// exercise it here so the promise "empty → 127.0.0.1/32" is covered.
	nets, err := resolveAllowList(nil)
	if err != nil {
		t.Fatalf("resolveAllowList nil: %v", err)
	}
	handler := ipAllowMiddleware(nets, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	// Localhost → OK
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for 127.0.0.1, got %d", rec.Code)
	}
	// Non-localhost → blocked
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "10.20.30.40:5678"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for remote IP with default allow-list, got %d", rec2.Code)
	}
}

func TestIPAllowMiddleware_TrustProxyUsesXFF(t *testing.T) {
	allow := []*net.IPNet{mustCIDR(t, "10.0.0.0/24")}
	handler := ipAllowMiddleware(allow, true, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.5:40000" // outside allow
	req.Header.Set("X-Forwarded-For", "10.0.0.7, 203.0.113.1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when XFF resolves to allowed IP, got %d", rec.Code)
	}
}

func TestIPAllowMiddleware_TrustProxyFalseIgnoresXFF(t *testing.T) {
	allow := []*net.IPNet{mustCIDR(t, "10.0.0.0/24")}
	handler := ipAllowMiddleware(allow, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be invoked")
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.5:40000" // outside
	req.Header.Set("X-Forwarded-For", "10.0.0.7")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when trustProxy=false, got %d", rec.Code)
	}
}

// -- CSRF middleware -----------------------------------------------------------

func TestCSRFMiddleware_ValidTokenPasses(t *testing.T) {
	token := "abcd1234"
	handler := csrfMiddleware(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/retry", nil)
	req.Header.Set("X-CSRF-Token", token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid CSRF, got %d", rec.Code)
	}
}

func TestCSRFMiddleware_ValidFormTokenPasses(t *testing.T) {
	token := "tok-form"
	handler := csrfMiddleware(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	body := url.Values{}
	body.Set("_csrf", token)
	req := httptest.NewRequest(http.MethodPost, "/retry", strings.NewReader(body.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid form CSRF, got %d", rec.Code)
	}
}

func TestCSRFMiddleware_MissingTokenFails(t *testing.T) {
	handler := csrfMiddleware("sekrit", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be invoked without CSRF")
	}))
	req := httptest.NewRequest(http.MethodPost, "/retry", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for missing CSRF, got %d", rec.Code)
	}
}

func TestCSRFMiddleware_MismatchTokenFails(t *testing.T) {
	handler := csrfMiddleware("good", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be invoked on mismatch")
	}))
	req := httptest.NewRequest(http.MethodPost, "/retry", nil)
	req.Header.Set("X-CSRF-Token", "bad")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on CSRF mismatch, got %d", rec.Code)
	}
}

func TestCSRFMiddleware_GetSkipsCheck(t *testing.T) {
	handler := csrfMiddleware("sekrit", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/status.json", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for GET without CSRF, got %d", rec.Code)
	}
}

// -- CIDR parsing --------------------------------------------------------------

func TestParseCIDRsHandlesEmpty(t *testing.T) {
	// Empty list + comments + whitespace → zero nets, no error.
	nets, err := parseCIDRs([]string{"", "   ", "# comment", "\t"})
	if err != nil {
		t.Fatalf("parseCIDRs: %v", err)
	}
	if len(nets) != 0 {
		t.Fatalf("expected 0 nets, got %d", len(nets))
	}
}

func TestParseCIDRsAcceptsBareIP(t *testing.T) {
	nets, err := parseCIDRs([]string{"10.1.2.3"})
	if err != nil {
		t.Fatalf("parseCIDRs bare IP: %v", err)
	}
	if len(nets) != 1 {
		t.Fatalf("expected 1 net, got %d", len(nets))
	}
	if nets[0].String() != "10.1.2.3/32" {
		t.Fatalf("expected /32 promotion, got %q", nets[0].String())
	}
}

func TestParseCIDRsRejectsMalformed(t *testing.T) {
	_, err := parseCIDRs([]string{"not-a-cidr"})
	if err == nil {
		t.Fatalf("expected error for malformed input")
	}
}

func TestResolveAllowListEmptyDefaultsLoopback(t *testing.T) {
	nets, err := resolveAllowList([]string{})
	if err != nil {
		t.Fatalf("resolveAllowList: %v", err)
	}
	if len(nets) != 1 || nets[0].String() != "127.0.0.1/32" {
		t.Fatalf("expected [127.0.0.1/32], got %v", nets)
	}
}

// -- End-to-end handlers with a fake state -------------------------------------

func newTestRecoveryServer(t *testing.T, job *biz.SelfDeployJob) *recoveryServer {
	t.Helper()
	statePath := filepath.Join(t.TempDir(), "state.json")
	sw, err := newStateWriter(statePath, &biz.SelfDeployState{
		JobID: job.ID,
		Phase: biz.SelfDeployPhaseRecovery,
	})
	if err != nil {
		t.Fatalf("newStateWriter: %v", err)
	}
	t.Cleanup(sw.Close)

	sw.Logf("test setup line 1")
	sw.Logf("test setup line 2")

	// Preload the embedded assets so tests don't hit disk.
	indexBytes, err := uiAssets.ReadFile("ui/index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	// We reuse html/template indirectly via recoveryServer's existing
	// code path, but to keep this test self-contained we do the parse
	// inline.
	tmpl, err := template.New("recovery-index").Parse(string(indexBytes))
	if err != nil {
		t.Fatalf("parse index.html: %v", err)
	}
	s := &recoveryServer{
		job:        job,
		state:      sw,
		allowList:  []*net.IPNet{mustCIDR(t, "127.0.0.1/32")},
		trustProxy: false,
		csrf:       "test-csrf-token",
		bindAddr:   "127.0.0.1:9999",
		indexTmpl:  tmpl,
		cssBytes:   []byte("/* test */"),
		jsBytes:    []byte("/* test */"),
		shutdownCh: make(chan struct{}),
	}
	return s
}

func TestRecoveryHandleStatus_ReturnsCurrentState(t *testing.T) {
	job := &biz.SelfDeployJob{ID: "job-status", TargetImageTag: "cuigh/swirl:v2", PreviousImageTag: "cuigh/swirl:v1"}
	s := newTestRecoveryServer(t, job)

	req := httptest.NewRequest(http.MethodGet, "/status.json", nil)
	rec := httptest.NewRecorder()
	s.handleStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("expected JSON content-type, got %q", ct)
	}
	var got biz.SelfDeployState
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode status: %v (body=%q)", err, rec.Body.String())
	}
	if got.JobID != "job-status" {
		t.Fatalf("expected job-status, got %q", got.JobID)
	}
	if got.Phase != biz.SelfDeployPhaseRecovery {
		t.Fatalf("expected phase=recovery, got %q", got.Phase)
	}
	if len(got.LogTail) < 2 {
		t.Fatalf("expected at least 2 log lines, got %d", len(got.LogTail))
	}
}

func TestRecoveryHandleRoot_RendersCSRFToken(t *testing.T) {
	job := &biz.SelfDeployJob{ID: "job-root", TargetImageTag: "cuigh/swirl:v2", PreviousImageTag: "cuigh/swirl:v1"}
	s := newTestRecoveryServer(t, job)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	s.handleRoot(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%q)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "test-csrf-token") {
		t.Fatalf("expected CSRF token in body, got %q", truncate(body, 300))
	}
	if !strings.Contains(body, "job-root") {
		t.Fatalf("expected job ID in body, got %q", truncate(body, 300))
	}
	if !strings.Contains(body, "cuigh/swirl:v2") {
		t.Fatalf("expected target image in body, got %q", truncate(body, 300))
	}
	if !strings.Contains(body, "cuigh/swirl:v1") {
		t.Fatalf("expected previous image in body, got %q", truncate(body, 300))
	}
}

func TestRecoveryHandleLogs_ReturnsPlainText(t *testing.T) {
	job := &biz.SelfDeployJob{ID: "job-logs"}
	s := newTestRecoveryServer(t, job)

	req := httptest.NewRequest(http.MethodGet, "/logs", nil)
	rec := httptest.NewRecorder()
	s.handleLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("expected text/plain, got %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "test setup line") {
		t.Fatalf("expected log content, got %q", rec.Body.String())
	}
}

func TestRecoveryHandleRollback_NoPreviousImage422(t *testing.T) {
	job := &biz.SelfDeployJob{ID: "job-noprev", TargetImageTag: "cuigh/swirl:v2"} // PreviousImageTag empty
	s := newTestRecoveryServer(t, job)

	req := httptest.NewRequest(http.MethodPost, "/rollback", nil)
	req.Header.Set("X-CSRF-Token", s.csrf)
	rec := httptest.NewRecorder()
	s.handleRollback(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 without previous image, got %d", rec.Code)
	}
}

func TestRecoveryHandleRoot_RejectsWrongPath(t *testing.T) {
	job := &biz.SelfDeployJob{ID: "job-path"}
	s := newTestRecoveryServer(t, job)
	req := httptest.NewRequest(http.MethodGet, "/nope", nil)
	rec := httptest.NewRecorder()
	s.handleRoot(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown path, got %d", rec.Code)
	}
}

// -- helpers --------------------------------------------------------------------

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

