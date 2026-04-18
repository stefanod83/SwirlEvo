package deploy_agent

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cuigh/auxo/log"
	"github.com/cuigh/swirl/biz"
	"github.com/docker/docker/client"
)

// uiAssets embeds the static HTML / CSS / JS the sidekick serves on
// the recovery port. The path is package-relative; files are served
// under the root (/, /style.css, /script.js).
//
//go:embed ui/index.html ui/style.css ui/script.js
var uiAssets embed.FS

// Timeout budgets for the recovery HTTP server. Long on writes because
// operators may watch a retry run for a while; short on reads so a slow
// client can't hog a slot.
const (
	recoveryReadTimeout  = 10 * time.Second
	recoveryWriteTimeout = 60 * time.Second
	recoveryIdleTimeout  = 120 * time.Second
)

// recoveryServer owns the HTTP machinery for the recovery UI. It holds
// references to the live stateWriter (so retry attempts can append to
// the same log ring the UI is polling) and to the original job so that
// rollback can swap TargetImageTag without reloading the file.
type recoveryServer struct {
	job         *biz.SelfDeployJob
	state       *stateWriter
	allowList   []*net.IPNet
	trustProxy  bool
	csrf        string
	bindAddr    string
	indexTmpl   *template.Template
	cssBytes    []byte
	jsBytes     []byte
	dockerCli   *client.Client
	mu          sync.Mutex    // serialises retry/rollback dispatch
	inFlight    atomic.Bool   // true while a retry/rollback is running
	successOnce sync.Once     // fires the shutdown signal exactly once
	shutdownCh  chan struct{} // closed when a retry/rollback has succeeded
	srv         *http.Server  // assigned after Listen so Shutdown works
}

// progressServer wraps a running recoveryServer with its lifecycle
// primitives so callers can either drive it synchronously (recovery
// mode) or shut it down explicitly (always-on progress mode).
type progressServer struct {
	rs    *recoveryServer
	errCh chan error
}

// startProgressServer boots the HTTP server (the same server the
// recovery UI used to spawn only on failure) at the BEGINNING of a
// deploy so the main Swirl UI can embed an iframe that shows logs +
// phase in real time. Call shutdown() on the returned server when the
// deploy succeeds and the sidekick no longer needs to serve progress.
// On a failing deploy leave the server running — it naturally enters
// the recovery role the moment the state phase becomes failed/recovery.
//
// Returns a fully-initialised *progressServer whose rs.dockerCli caller
// owns: it is Close()-d only via the server's shutdown() path. If the
// caller never calls shutdown(), awaitProgressServer() will do it upon
// exit.
func startProgressServer(ctx context.Context, j *biz.SelfDeployJob, sw *stateWriter, port int, allow []string, trustProxy bool) (*progressServer, error) {
	if j == nil {
		return nil, errors.New("deploy-agent: startProgressServer: nil job")
	}
	if sw == nil {
		return nil, errors.New("deploy-agent: startProgressServer: nil stateWriter")
	}
	if port <= 0 {
		port = 8002
	}

	logger := log.Get("deploy-agent")

	nets, err := resolveAllowList(allow)
	if err != nil {
		return nil, fmt.Errorf("deploy-agent: parse allow-list: %w", err)
	}

	// Fresh docker client for retry/rollback. Keep it alive for the
	// whole server lifetime; shutdown() releases it.
	cli, err := newDockerClient()
	if err != nil {
		return nil, err
	}
	if err := pingDocker(ctx, cli); err != nil {
		cli.Close()
		return nil, fmt.Errorf("deploy-agent: progress server: docker unreachable: %w", err)
	}

	token, err := newCSRFToken()
	if err != nil {
		cli.Close()
		return nil, fmt.Errorf("deploy-agent: generate CSRF token: %w", err)
	}

	bindHost := "127.0.0.1"
	if v := strings.TrimSpace(os.Getenv("SWIRL_RECOVERY_BIND")); v != "" {
		bindHost = v
	}
	bind := fmt.Sprintf("%s:%d", bindHost, port)

	indexBytes, err := uiAssets.ReadFile("ui/index.html")
	if err != nil {
		cli.Close()
		return nil, fmt.Errorf("deploy-agent: load index.html: %w", err)
	}
	indexTmpl, err := template.New("recovery-index").Parse(string(indexBytes))
	if err != nil {
		cli.Close()
		return nil, fmt.Errorf("deploy-agent: parse index.html: %w", err)
	}
	cssBytes, err := uiAssets.ReadFile("ui/style.css")
	if err != nil {
		cli.Close()
		return nil, fmt.Errorf("deploy-agent: load style.css: %w", err)
	}
	jsBytes, err := uiAssets.ReadFile("ui/script.js")
	if err != nil {
		cli.Close()
		return nil, fmt.Errorf("deploy-agent: load script.js: %w", err)
	}

	s := &recoveryServer{
		job:        j,
		state:      sw,
		allowList:  nets,
		trustProxy: trustProxy,
		csrf:       token,
		bindAddr:   bind,
		indexTmpl:  indexTmpl,
		cssBytes:   cssBytes,
		jsBytes:    jsBytes,
		dockerCli:  cli,
		shutdownCh: make(chan struct{}),
	}

	srv := &http.Server{
		Addr:         bind,
		Handler:      s.routes(),
		ReadTimeout:  recoveryReadTimeout,
		WriteTimeout: recoveryWriteTimeout,
		IdleTimeout:  recoveryIdleTimeout,
	}
	s.srv = srv

	ln, err := net.Listen("tcp", bind)
	if err != nil {
		cli.Close()
		return nil, fmt.Errorf("deploy-agent: progress listen on %s: %w", bind, err)
	}

	allowStrs := make([]string, 0, len(nets))
	for _, n := range nets {
		allowStrs = append(allowStrs, n.String())
	}
	logger.Infof("progress UI listening on %s (allow-list: %v, trust-proxy: %v)", bind, allowStrs, trustProxy)
	sw.Logf("progress UI listening on %s (allow-list: %s)", bind, strings.Join(allowStrs, ","))
	sw.Logf("CSRF token generated (hidden; embedded in progress page)")

	errCh := make(chan error, 1)
	go func() {
		if serveErr := srv.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
			return
		}
		errCh <- nil
	}()

	return &progressServer{rs: s, errCh: errCh}, nil
}

// shutdown stops the HTTP server gracefully and releases the docker
// client. Idempotent: safe to call more than once. Used on the happy
// path when the deploy has succeeded and the main Swirl UI is about
// to reload onto the new container.
func (ps *progressServer) shutdown() {
	if ps == nil || ps.rs == nil {
		return
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = ps.rs.srv.Shutdown(shutdownCtx)
	select {
	case <-ps.errCh:
	case <-shutdownCtx.Done():
	}
	if ps.rs.dockerCli != nil {
		ps.rs.dockerCli.Close()
		ps.rs.dockerCli = nil
	}
}

// awaitRecovery blocks (driving the recovery-mode UX) until the
// operator resolves the deploy via Retry/Rollback, the context is
// cancelled, or the server fails. Returns nil when the operator
// recovered the deploy successfully. The docker client is released on
// return.
//
// The function is used ONLY when the deploy has failed and no
// automatic rollback was applied — i.e. the main Swirl UI is no longer
// reachable so the sidekick stands in as the only control surface.
func (ps *progressServer) awaitRecovery(ctx context.Context) error {
	if ps == nil || ps.rs == nil {
		return errors.New("deploy-agent: awaitRecovery: nil progress server")
	}
	s := ps.rs
	defer func() {
		if s.dockerCli != nil {
			s.dockerCli.Close()
			s.dockerCli = nil
		}
	}()

	select {
	case <-ctx.Done():
		s.state.Logf("recovery UI shutting down: context cancelled")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.srv.Shutdown(shutdownCtx)
		<-ps.errCh
		return ctx.Err()
	case <-s.shutdownCh:
		s.state.Logf("recovery UI shutting down: deploy resolved by operator action")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.srv.Shutdown(shutdownCtx)
		<-ps.errCh
		return nil
	case err := <-ps.errCh:
		if err != nil {
			return fmt.Errorf("deploy-agent: recovery serve failed: %w", err)
		}
		return errors.New("deploy-agent: recovery server exited unexpectedly")
	}
}

// routes wires the middleware chain around the handlers.
//
// Layout:
//
//	ipAllow → csrf (POSTs only) → mux
//
// The mux itself handles static and dynamic routes.
func (s *recoveryServer) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRoot)
	mux.HandleFunc("/status.json", s.handleStatus)
	mux.HandleFunc("/logs", s.handleLogs)
	mux.HandleFunc("/retry", s.handleRetry)
	mux.HandleFunc("/rollback", s.handleRollback)
	mux.HandleFunc("/style.css", s.handleAssets)
	mux.HandleFunc("/script.js", s.handleAssets)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		// Tiny unauthenticated liveness endpoint — subject to the IP
		// allow-list like everything else, but useful for operators
		// confirming "is the recovery server up?"
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	csrfWrapped := csrfMiddleware(s.csrf, mux)
	return ipAllowMiddleware(s.allowList, s.trustProxy, csrfWrapped)
}

// handleRoot renders the embedded index.html with the CSRF token and
// the current job metadata injected. Only GET is allowed.
func (s *recoveryServer) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	allowStrs := make([]string, 0, len(s.allowList))
	for _, n := range s.allowList {
		allowStrs = append(allowStrs, n.String())
	}
	data := struct {
		CSRFToken     string
		JobID         string
		TargetImage   string
		PreviousImage string
		AllowList     string
		Bind          string
	}{
		CSRFToken:     s.csrf,
		JobID:         s.job.ID,
		TargetImage:   s.job.TargetImageTag,
		PreviousImage: s.job.PreviousImageTag,
		AllowList:     strings.Join(allowStrs, ", "),
		Bind:          s.bindAddr,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if err := s.indexTmpl.Execute(w, data); err != nil {
		log.Get("deploy-agent").Warnf("recovery: render index: %v", err)
	}
}

// handleStatus returns the current SelfDeployState snapshot as JSON.
// Used by script.js for its 3s polling cycle.
func (s *recoveryServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	snap := s.snapshotState()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(snap)
}

// handleLogs returns the log ring buffer as text/plain.
func (s *recoveryServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	snap := s.snapshotState()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	for _, line := range snap.LogTail {
		_, _ = w.Write([]byte(line))
		_, _ = w.Write([]byte("\n"))
	}
}

// snapshotState reads a deep copy of the current state under the
// writer's lock so callers never see a torn struct. LogTail is
// regenerated from the ring buffer.
func (s *recoveryServer) snapshotState() biz.SelfDeployState {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	st := s.state.st
	st.LogTail = s.state.snapshotRingLocked()
	return st
}

// handleRetry re-runs runDeploy against the original job. Guarded by
// an in-flight flag so the operator can't accidentally double-submit.
// On success we fire shutdownCh so serveRecovery returns nil and the
// sidekick exits 0.
func (s *recoveryServer) handleRetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	if s.inFlight.Load() {
		s.mu.Unlock()
		http.Error(w, "another deploy action is already running", http.StatusConflict)
		return
	}
	s.inFlight.Store(true)
	s.mu.Unlock()

	s.state.Logf("operator requested retry (job %s, target %s)", s.job.ID, s.job.TargetImageTag)
	log.Get("deploy-agent").Infof("recovery: retry dispatched for job %s", s.job.ID)

	go func() {
		defer s.inFlight.Store(false)
		ctx := context.Background()
		if err := runDeploy(ctx, s.job, s.state); err != nil {
			s.state.Logf("retry failed: %v", err)
			return
		}
		s.state.Logf("retry succeeded; shutting down recovery UI")
		s.successOnce.Do(func() { close(s.shutdownCh) })
	}()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte("retry dispatched — watch the status panel."))
}

// handleRollback flips the target image tag to the previous one and
// re-runs runDeploy. Returns 422 when the job carries no previous tag.
func (s *recoveryServer) handleRollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if strings.TrimSpace(s.job.PreviousImageTag) == "" {
		http.Error(w, "rollback unavailable: previous image tag unknown", http.StatusUnprocessableEntity)
		return
	}
	s.mu.Lock()
	if s.inFlight.Load() {
		s.mu.Unlock()
		http.Error(w, "another deploy action is already running", http.StatusConflict)
		return
	}
	s.inFlight.Store(true)
	s.mu.Unlock()

	// Build a one-shot job variant targeting the previous image. The
	// sidekick does not carry the template used to render the YAML, so
	// we do a best-effort string substitution: any occurrence of the
	// original TargetImageTag in the YAML becomes PreviousImageTag.
	// For the typical self-deploy template (one service, one image:
	// line) this is sufficient. Operators with unusual templates can
	// still retry manually from the primary UI once it's back up.
	j := *s.job
	j.TargetImageTag = s.job.PreviousImageTag
	if s.job.TargetImageTag != "" {
		j.ComposeYAML = strings.ReplaceAll(s.job.ComposeYAML, s.job.TargetImageTag, s.job.PreviousImageTag)
	}
	j.Placeholders.ImageTag = s.job.PreviousImageTag

	s.state.Logf("operator requested rollback (job %s: %s -> %s)", s.job.ID, s.job.TargetImageTag, s.job.PreviousImageTag)
	log.Get("deploy-agent").Infof("recovery: rollback dispatched for job %s (target %s)", s.job.ID, j.TargetImageTag)

	go func() {
		defer s.inFlight.Store(false)
		ctx := context.Background()
		if err := runDeploy(ctx, &j, s.state); err != nil {
			s.state.Logf("rollback-deploy failed: %v", err)
			return
		}
		s.state.Logf("rollback succeeded; shutting down recovery UI")
		s.successOnce.Do(func() { close(s.shutdownCh) })
	}()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte("rollback dispatched — watch the status panel."))
}

// handleAssets serves the embedded CSS/JS. Paths match the exact
// filenames; anything else is a 404.
func (s *recoveryServer) handleAssets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	switch r.URL.Path {
	case "/style.css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=60")
		_, _ = w.Write(s.cssBytes)
	case "/script.js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=60")
		_, _ = w.Write(s.jsBytes)
	default:
		http.NotFound(w, r)
	}
}

// newCSRFToken returns a hex-encoded 32-byte random token.
func newCSRFToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// selectRecoveryPort resolves the port the sidekick should bind the
// recovery UI to. Precedence: env > job > 8002.
func selectRecoveryPort(j *biz.SelfDeployJob) int {
	if raw := strings.TrimSpace(os.Getenv(EnvRecoveryPort)); raw != "" {
		if p, err := strconv.Atoi(raw); err == nil && p > 0 && p < 65536 {
			return p
		}
	}
	if j != nil && j.RecoveryPort > 0 {
		return j.RecoveryPort
	}
	return 8002
}

// selectRecoveryAllow resolves the CIDR allow-list. Precedence:
// env > job > ["127.0.0.1/32"].
func selectRecoveryAllow(j *biz.SelfDeployJob) []string {
	if raw := strings.TrimSpace(os.Getenv(EnvRecoveryAllow)); raw != "" {
		parts := strings.Split(raw, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if j != nil && len(j.RecoveryAllow) > 0 {
		return j.RecoveryAllow
	}
	return []string{"127.0.0.1/32"}
}

// selectRecoveryTrustProxy reads SWIRL_RECOVERY_TRUST_PROXY and
// normalises the input. Empty or unrecognised values → false.
func selectRecoveryTrustProxy() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(EnvRecoveryTrustProxy)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}
