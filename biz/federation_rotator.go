package biz

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/cuigh/auxo/log"
	"github.com/cuigh/swirl/dao"
	"github.com/cuigh/swirl/misc"
)

// FederationRotator is the portal-side background ticker that keeps
// `swarm_via_swirl` host tokens fresh. Every `rotatorInterval` it
// enumerates hosts with `TokenAutoRefresh=true` and, for those whose
// token expiry is within `rotatorThreshold` of the current time,
// calls the target's `/api/federation/rotate-self` endpoint to mint
// a new token — then persists the new token + expiry back on the
// host record.
//
// The rotator NEVER mints tokens for hosts without AutoRefresh — the
// operator explicitly opts in per-host. Failures are logged but
// non-fatal: the token keeps working (soft-expiry) until the operator
// intervenes.
//
// Runs only when MODE=standalone (the target-side doesn't need a
// rotator — its tokens are rotated by whoever holds them).
type FederationRotator struct {
	di         dao.Interface
	httpClient *http.Client
}

// NewFederationRotator is the DI constructor. The rotator shares the
// dao.Interface with the rest of the biz layer so it reads/writes
// host records through the same path as the UI.
func NewFederationRotator(di dao.Interface) *FederationRotator {
	return &FederationRotator{
		di: di,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        16,
				IdleConnTimeout:     60 * time.Second,
				TLSClientConfig:     &tls.Config{},
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
			},
		},
	}
}

// How often the rotator scans hosts. 30 min is aggressive enough that
// a token expiring in a few hours is caught within one cycle, yet
// lightweight for a long-running Swirl.
const rotatorInterval = 30 * time.Minute

// Refresh threshold: rotate when the remaining validity is less than
// this. 7 days matches the UI "expiring soon" banner, keeping the
// signals coherent.
const rotatorThreshold = 7 * 24 * time.Hour

// Default TTL requested from the target when rotating. The target
// enforces its own cap; we just ask for a sane value so operators
// don't have to configure this per-host.
const rotatorDefaultTTLDays = 90

// Start launches the rotator in a background goroutine. No-op when
// `MODE != standalone` — the rotator is a portal concern. Cancel the
// parent context to stop. Logs to the `federation-rotator` logger.
func (r *FederationRotator) Start(ctx context.Context) {
	if !misc.IsStandalone() {
		return
	}
	go r.loop(ctx)
}

func (r *FederationRotator) loop(ctx context.Context) {
	logger := log.Get("federation-rotator")
	logger.Infof("federation rotator started (interval=%s, threshold=%s)", rotatorInterval, rotatorThreshold)

	// Fire once shortly after boot so a host whose token expires in a
	// few hours is not left untouched for the full interval.
	select {
	case <-ctx.Done():
		return
	case <-time.After(30 * time.Second):
	}
	r.tick(ctx, logger)

	ticker := time.NewTicker(rotatorInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("federation rotator stopping")
			return
		case <-ticker.C:
			r.tick(ctx, logger)
		}
	}
}

func (r *FederationRotator) tick(ctx context.Context, logger log.Logger) {
	hosts, err := r.di.HostGetAll(ctx)
	if err != nil {
		logger.Warnf("federation rotator: could not enumerate hosts: %v", err)
		return
	}
	for _, h := range hosts {
		if h.Type != "swarm_via_swirl" || !h.TokenAutoRefresh {
			continue
		}
		if !shouldRotate(h) {
			continue
		}
		if rerr := r.rotateHost(ctx, h); rerr != nil {
			logger.Warnf("federation rotator: host %q rotate failed: %v", h.Name, rerr)
			continue
		}
		logger.Infof("federation rotator: host %q token rotated", h.Name)
	}
}

// shouldRotate returns true when the host's token expiry is missing,
// already elapsed, or within `rotatorThreshold` of now. Zero expiry
// (treat as "unknown") triggers a refresh too — keeps legacy records
// in sync.
func shouldRotate(h *dao.Host) bool {
	t := time.Time(h.TokenExpiresAt)
	if t.IsZero() {
		return true
	}
	return time.Until(t) < rotatorThreshold
}

// rotateHost calls the remote Swirl's self-rotate endpoint with the
// host's current token and persists the response.
func (r *FederationRotator) rotateHost(ctx context.Context, h *dao.Host) error {
	if h.SwirlURL == "" || h.SwirlToken == "" {
		return fmt.Errorf("host %q missing SwirlURL or SwirlToken", h.Name)
	}
	body, _ := json.Marshal(map[string]int{"ttlDays": rotatorDefaultTTLDays})
	reqURL := h.SwirlURL + "/api/federation/rotate-self"
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+h.SwirlToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("rotate-self returned %d: %s", resp.StatusCode, string(buf))
	}

	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Token     string    `json:"token"`
			ExpiresAt time.Time `json:"expiresAt"`
		} `json:"data"`
		Info string `json:"info"`
	}
	if derr := json.NewDecoder(resp.Body).Decode(&envelope); derr != nil {
		return fmt.Errorf("decode rotate-self response: %w", derr)
	}
	if envelope.Code != 0 || envelope.Data.Token == "" {
		return fmt.Errorf("rotate-self envelope error: code=%d info=%q", envelope.Code, envelope.Info)
	}
	h.SwirlToken = envelope.Data.Token
	h.TokenExpiresAt = dao.Time(envelope.Data.ExpiresAt)
	h.UpdatedAt = now()
	return r.di.HostUpdate(ctx, h)
}
