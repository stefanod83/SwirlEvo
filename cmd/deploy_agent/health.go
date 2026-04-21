package deploy_agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// healthPollInterval is the delay between two health check probes.
// 2s matches the planning document and keeps the handshake responsive
// without hammering the daemon.
const healthPollInterval = 2 * time.Second

// healthRequestTimeout is the per-request timeout for the GET against
// /api/system/ready. Kept well below healthPollInterval so a slow
// response never stacks against the next tick.
const healthRequestTimeout = 1500 * time.Millisecond

// minHealthTimeout is the floor applied by runDeploy when the caller's
// remaining budget is smaller than this. Gives the new Swirl a minimum
// chance to answer at least one probe even after a slow pull.
const minHealthTimeout = 30 * time.Second

// waitHealthy polls the supplied URL until it returns 2xx or the
// total timeout elapses. The context is honoured — cancelling it aborts
// the poll loop immediately with ctx.Err().
//
// Expected endpoint: /api/system/ready (public, auth:"*"). A 200 OK
// means the new Swirl process has wired up its DB client, Docker
// client and settings snapshot — i.e. the home page is actually
// usable. A 503 means "not yet ready, keep polling" (the endpoint
// returns 503 when any dependency check fails).
//
// /api/system/ready replaced /api/system/mode here after the
// "UI redirects too early → broken home page, need F5" bug: /mode
// answers as soon as the HTTP server starts (before DB connect).
//
// Returns nil on first 2xx. Returns a descriptive error on timeout,
// including the last observed HTTP status (or network error) so the
// operator has context about why the new container never came up.
func waitHealthy(ctx context.Context, url string, total time.Duration) error {
	return waitHealthyResolver(ctx, func(context.Context) (string, error) { return url, nil }, total)
}

// waitHealthyResolver is the name-based variant: the URL is re-resolved
// on every probe attempt so a container that restarts mid-loop (and
// thus picks up a new IP) is still reachable. Used by runDeploy so the
// health check survives transient container churn without requiring
// the YAML to publish any port on the host.
func waitHealthyResolver(ctx context.Context, resolve func(context.Context) (string, error), total time.Duration) error {
	if resolve == nil {
		return errors.New("deploy-agent: waitHealthy: nil resolver")
	}
	if total < minHealthTimeout {
		total = minHealthTimeout
	}
	deadline := time.Now().Add(total)

	client := &http.Client{Timeout: healthRequestTimeout}
	var lastErr error
	var lastStatus int
	var lastURL string
	attempts := 0

	// Loop: probe → wait → repeat until deadline or ctx cancelled.
	for {
		attempts++
		url, rerr := resolve(ctx)
		if rerr != nil || url == "" {
			lastErr = rerr
			lastStatus = 0
			goto wait
		}
		lastURL = url
		{
			status, err := probeOnce(ctx, client, url)
			if err == nil && status >= 200 && status < 300 {
				return nil
			}
			lastErr = err
			lastStatus = status
		}
	wait:

		// Has the deadline passed? (Do the check AFTER the probe so we
		// always try at least once even with a zero-ish timeout.)
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}

		// Wait until the next tick or the deadline, whichever comes
		// first. Using min() avoids sleeping past the total budget.
		wait := healthPollInterval
		if wait > remaining {
			wait = remaining
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}

	// Format the failure with the most informative of (last HTTP
	// status, last network error).
	if lastErr != nil {
		return fmt.Errorf("deploy-agent: health check %q did not succeed within %s after %d attempts: %w", lastURL, total, attempts, lastErr)
	}
	return fmt.Errorf("deploy-agent: health check %q did not return 2xx within %s after %d attempts (last status=%d)", lastURL, total, attempts, lastStatus)
}

// probeOnce performs a single GET, returning the HTTP status (or 0 on
// transport error) and any network error. The body is drained so the
// underlying connection can be reused.
func probeOnce(ctx context.Context, client *http.Client, url string) (int, error) {
	reqCtx, cancel := context.WithTimeout(ctx, healthRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

