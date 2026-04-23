package biz

import (
	"context"
	"fmt"
	"io"
	"strings"

	dockerimage "github.com/docker/docker/api/types/image"
	dockerclient "github.com/docker/docker/client"
)

// Registry Cache image mirror.
//
// RewriteImages computes the rewritten image refs for a stack (where the
// cache wants them to point to). The mirror step below actively pulls
// each original image from its upstream origin, retags it with the cache
// ref, and pushes it to the mirror — so by the time the engine asks
// Docker to pull the rewritten ref, the cache already has the bytes.
//
// Runs at Deploy time only. On any pull/tag/push failure the whole
// deploy is aborted (D3a: hard fail) so operators see the problem
// immediately rather than discovering it at runtime via Docker's own
// error surface.
//
// Behaviour:
//   - pullImages=true  → always pull upstream, retag, push. Docker's
//     layer cache makes this cheap when nothing changed. Digest drift
//     upstream → the cache is refreshed automatically.
//   - pullImages=false → skip mirror for services whose cache ref is
//     already available locally (we pushed it previously, so the
//     cache has it too). Pull+push only when the cache ref is absent.

// MirrorAction is an audit entry for one mirrored image. Returned to
// the caller so the Deploy flow can log / persist what happened.
type MirrorAction struct {
	Service  string `json:"service"`
	Upstream string `json:"upstream"`
	CacheRef string `json:"cacheRef"`
	// Outcome: "pulled" (did the full pull+tag+push), "already-present"
	// (pullImages=false and cache ref was locally available, skipped),
	// "skipped-local-ref" (the original ref already targets the mirror
	// host — nothing to mirror), "no-rewrite" (rewriter did not flag
	// this service, cache disabled for it).
	Outcome string `json:"outcome"`
	Error   string `json:"error,omitempty"`
}

// mirrorActionsToCache takes the RewriteAction list produced by
// RewriteImages and performs the actual pull→tag→push on the target
// daemon. The `actions` slice carries every service the rewriter
// touched; services that were not rewritten (no upstream match, digest
// pinned, scope opt-out) never appear here so the mirror leaves them
// alone.
func mirrorActionsToCache(
	ctx context.Context,
	cli *dockerclient.Client,
	rb RegistryBiz,
	actions []RewriteAction,
	pullImages bool,
) ([]MirrorAction, error) {
	if len(actions) == 0 {
		return nil, nil
	}
	out := make([]MirrorAction, 0, len(actions))
	for _, a := range actions {
		// Rewriter emits entries for services it evaluated even when it
		// did NOT rewrite (digest-preserved, invalid-ref, no-match).
		// Only services carrying a Rewritten target need mirroring.
		if a.Rewritten == "" {
			out = append(out, MirrorAction{
				Service:  a.Service,
				Upstream: a.Original,
				Outcome:  "no-rewrite",
			})
			continue
		}
		if strings.EqualFold(a.Original, a.Rewritten) {
			// Original already matches the cache (shouldn't normally
			// happen but defend against regex drift in the rewriter).
			out = append(out, MirrorAction{
				Service:  a.Service,
				Upstream: a.Original,
				CacheRef: a.Rewritten,
				Outcome:  "skipped-local-ref",
			})
			continue
		}

		act := MirrorAction{
			Service:  a.Service,
			Upstream: a.Original,
			CacheRef: a.Rewritten,
		}

		needSync := true
		if !pullImages {
			// Assume "present locally" implies "pushed previously" —
			// cheap heuristic that avoids a dedicated registry HEAD.
			if _, _, iErr := cli.ImageInspectWithRaw(ctx, a.Rewritten); iErr == nil {
				act.Outcome = "already-present"
				out = append(out, act)
				needSync = false
			}
		}
		if !needSync {
			continue
		}

		// Auth resolution: look up Registry entities by host URL. Empty
		// auth is fine for public upstreams (Docker SDK accepts it) and
		// for unauth'd local caches.
		upstreamHost := originHost(a.Original)
		upstreamAuth := resolveRegistryAuth(ctx, rb, upstreamHost)
		cacheHost := originHost(a.Rewritten)
		cacheAuth := resolveRegistryAuth(ctx, rb, cacheHost)

		if err := pullImage(ctx, cli, a.Original, upstreamAuth); err != nil {
			act.Outcome = "error"
			act.Error = fmt.Sprintf("pull %s: %v", a.Original, err)
			out = append(out, act)
			return out, fmt.Errorf("service %s: %s", a.Service, act.Error)
		}
		if err := cli.ImageTag(ctx, a.Original, a.Rewritten); err != nil {
			act.Outcome = "error"
			act.Error = fmt.Sprintf("tag %s→%s: %v", a.Original, a.Rewritten, err)
			out = append(out, act)
			return out, fmt.Errorf("service %s: %s", a.Service, act.Error)
		}
		if err := pushImage(ctx, cli, a.Rewritten, cacheAuth); err != nil {
			act.Outcome = "error"
			act.Error = fmt.Sprintf("push %s: %v", a.Rewritten, err)
			out = append(out, act)
			return out, fmt.Errorf("service %s: %s", a.Service, act.Error)
		}
		act.Outcome = "pulled"
		out = append(out, act)
	}
	return out, nil
}

// originHost strips the repo path + tag off an image reference and
// returns the registry hostname:port. For `ghcr.io/pixlcore/xysat:latest`
// returns `ghcr.io`; for `registry.devarch.local:443/ghcr.io/...` returns
// `registry.devarch.local:443`. Fallback: full ref when it doesn't
// contain a slash (unlikely for rewritten refs).
func originHost(ref string) string {
	if slash := strings.Index(ref, "/"); slash > 0 {
		return ref[:slash]
	}
	return ref
}

// resolveRegistryAuth looks up a Registry entity by URL and returns the
// encoded AuthConfig, or "" when no entity matches (anonymous access).
// Silently swallows errors — failure to resolve auth is the same
// observable state as no-auth, and the subsequent pull/push will
// produce the canonical error if credentials are actually required.
func resolveRegistryAuth(ctx context.Context, rb RegistryBiz, host string) string {
	if rb == nil || host == "" {
		return ""
	}
	for _, scheme := range []string{"https://", "http://"} {
		if a, err := rb.GetAuth(ctx, scheme+host); err == nil && a != "" {
			return a
		}
	}
	return ""
}

// pullImage / pushImage are thin wrappers around the SDK calls that
// drain the progress stream so errors surface via io.Copy's final read.
// Kept local to this file so the mirror doesn't depend on docker
// package helpers that take `node` (the daemon is already acquired).

func pullImage(ctx context.Context, cli *dockerclient.Client, ref, auth string) error {
	rc, err := cli.ImagePull(ctx, ref, dockerimage.PullOptions{RegistryAuth: auth})
	if err != nil {
		return err
	}
	defer rc.Close()
	_, err = io.Copy(io.Discard, rc)
	return err
}

func pushImage(ctx context.Context, cli *dockerclient.Client, ref, auth string) error {
	rc, err := cli.ImagePush(ctx, ref, dockerimage.PushOptions{RegistryAuth: auth})
	if err != nil {
		return err
	}
	defer rc.Close()
	_, err = io.Copy(io.Discard, rc)
	return err
}
