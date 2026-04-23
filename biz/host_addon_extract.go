package biz

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/cuigh/auxo/net/web"
)

// AddonConfigExtract is the *decoded* counterpart of the Host.AddonConfigExtract
// JSON blob. Lists are deduped and validated at the biz layer before being
// persisted. A TraefikExtract may be nil if the operator hasn't uploaded a
// config file yet — the discovery flow falls back to docker-inspect only.
type AddonConfigExtract struct {
	Traefik       *TraefikExtract       `json:"traefik,omitempty"`
	RegistryCache *RegistryCacheExtract `json:"registryCache,omitempty"`
}

// RegistryCacheExtract is the per-host opt-in state for the pull-through
// registry mirror configured in Setting.RegistryCache. Only the toggle +
// insecure-mode flag + the "applied" attestation are persisted — the
// generated daemon.json snippet and bootstrap script are recomputed on
// every read so operators always see the current mirror config / CA.
type RegistryCacheExtract struct {
	// Enabled gates compose image rewriting for this host (RewriteMode
	// = "per-host" in Setting.RegistryCache only rewrites when Enabled
	// is true). Flipping it also enables the addon panel's "Mark as
	// applied" and related UX.
	Enabled bool `json:"enabled"`
	// InsecureMode switches the generated daemon.json snippet from the
	// cert-distribution path (/etc/docker/certs.d/<host>:<port>/ca.crt)
	// to `insecure-registries` listing. Narrower blast radius by
	// default — operators opt in explicitly when they do not want to
	// distribute the CA.
	InsecureMode bool `json:"insecureMode"`
	// AppliedAt / AppliedBy record the operator attestation that the
	// generated snippet has been applied on the target daemon. Manual
	// flag — Swirl never writes daemon.json itself.
	AppliedAt time.Time `json:"appliedAt,omitempty"`
	AppliedBy string    `json:"appliedBy,omitempty"`
	// AppliedFingerprint is a copy of Setting.RegistryCache.CAFingerprint
	// at the moment the operator clicked "Mark as applied". Used in
	// Phase 5 to flag hosts whose daemon trust is stale after a CA
	// rotation.
	AppliedFingerprint string `json:"appliedFingerprint,omitempty"`
	// LastSyncAt / LastSyncBy record the federation-delegation push
	// (Phase 4) for swarm_via_swirl hosts: the portal cannot rewrite
	// daemon.json on the peer's nodes directly, so it mirrors the
	// global Setting.RegistryCache to the peer's own Settings via
	// /api/federation/registry-cache/receive. These fields are only
	// populated for swarm_via_swirl hosts; standalone hosts use
	// AppliedAt instead.
	LastSyncAt          time.Time `json:"lastSyncAt,omitempty"`
	LastSyncBy          string    `json:"lastSyncBy,omitempty"`
	LastSyncFingerprint string    `json:"lastSyncFingerprint,omitempty"`
}

// TraefikExtract carries the host-level Traefik configuration the operator
// curates in the Host edit page: lists discovered/uploaded + pointers to
// the stack or container actually running Traefik + default values the
// wizard pre-fills in the stack editor + free-form overrides. The raw
// traefik.yml is never persisted (may contain ACME keys).
type TraefikExtract struct {
	// Enabled gates the Traefik tab in the stack editor. When false the
	// stack-editor Traefik tab is hidden entirely — the operator must
	// opt in from the Host edit page before configuring services.
	Enabled bool `json:"enabled"`
	// Lists — union of docker-inspect + uploaded file extraction.
	EntryPoints   []string `json:"entryPoints"`
	CertResolvers []string `json:"certResolvers"`
	Middlewares   []string `json:"middlewares"`
	Networks      []string `json:"networks"`

	// Pointers — which managed stack and/or container actually runs
	// Traefik on this host. Informational for the operator; the stack
	// editor tab surfaces them as a badge so users know "this Traefik
	// is served by stack X on container Y". Both optional: the operator
	// can leave them blank when the Traefik deploy is managed outside
	// Swirl.
	StackID       string `json:"stackId,omitempty"`
	ContainerName string `json:"containerName,omitempty"`

	// Defaults — pre-fill values for the stack-editor Traefik tab when
	// neither docker-inspect nor the uploaded config can derive them.
	// Empty = no default; the stack editor keeps its empty form state.
	DefaultDomain       string `json:"defaultDomain,omitempty"`
	DefaultEntrypoint   string `json:"defaultEntrypoint,omitempty"`
	DefaultCertResolver string `json:"defaultCertResolver,omitempty"`

	// Overrides — free-form key/value pairs the operator can set when
	// a field of interest isn't captured by the structured fields above.
	// Displayed in the stack editor as read-only hints.
	Overrides map[string]string `json:"overrides,omitempty"`

	// Provenance of the last upload.
	SourceFile string    `json:"sourceFile,omitempty"`
	UploadedAt time.Time `json:"uploadedAt,omitempty"`
	UploadedBy string    `json:"uploadedBy,omitempty"`
}

// GetAddonConfigExtract decodes the JSON blob stored on the host record.
// Returns an empty value when the host has no extract persisted. Never
// returns an error for a missing host — callers routinely ask for hosts
// that don't yet have an extract.
func (b *hostBiz) GetAddonConfigExtract(ctx context.Context, hostID string) (*AddonConfigExtract, error) {
	if hostID == "" {
		return &AddonConfigExtract{}, nil
	}
	host, err := b.di.HostGet(ctx, hostID)
	if err != nil || host == nil {
		return &AddonConfigExtract{}, err
	}
	return decodeAddonConfigExtract(host.AddonConfigExtract), nil
}

// UpdateAddonConfigExtract replaces the Traefik subtree of the host's
// extract. The raw file upload happens client-side; the JSON we receive here
// has already been scrubbed of any secrets.
func (b *hostBiz) UpdateAddonConfigExtract(ctx context.Context, hostID string, extract *AddonConfigExtract, user web.User) error {
	if hostID == "" {
		return errors.New("hostId is required")
	}
	host, err := b.di.HostGet(ctx, hostID)
	if err != nil {
		return err
	}
	if host == nil {
		return errors.New("host not found")
	}
	// Merge with the existing extract: callers may provide just one
	// addon subtree (e.g. only Traefik) without wanting to nuke the
	// others. Unset subtrees are left as-is. The upload timestamp /
	// SourceFile only flip when the uploaded-lists fields actually
	// changed — editing just StackID or Defaults must not rewrite the
	// "last upload" provenance.
	current := decodeAddonConfigExtract(host.AddonConfigExtract)
	if extract != nil && extract.RegistryCache != nil {
		rc := *extract.RegistryCache
		// Stamp AppliedAt / AppliedBy when the caller explicitly
		// flips the "Mark as applied" button (it sends a zero-value
		// AppliedAt so we know to set it here rather than echoing an
		// old one). Enabled-only edits preserve the previous
		// attestation.
		if rc.AppliedAt.IsZero() && current.RegistryCache != nil {
			rc.AppliedAt = current.RegistryCache.AppliedAt
			rc.AppliedBy = current.RegistryCache.AppliedBy
			rc.AppliedFingerprint = current.RegistryCache.AppliedFingerprint
		} else if !rc.AppliedAt.IsZero() && user != nil && rc.AppliedBy == "" {
			rc.AppliedBy = user.Name()
		}
		current.RegistryCache = &rc
	}
	if extract != nil && extract.Traefik != nil {
		t := *extract.Traefik
		t.EntryPoints = dedup(t.EntryPoints)
		t.CertResolvers = dedup(t.CertResolvers)
		t.Middlewares = dedup(t.Middlewares)
		t.Networks = dedup(t.Networks)
		// Only stamp upload provenance when the caller actually
		// provided a SourceFile — gives the API two calling patterns
		// (upload vs metadata edit) without a second endpoint.
		if t.SourceFile != "" {
			if t.UploadedAt.IsZero() {
				t.UploadedAt = time.Now()
			}
			if t.UploadedBy == "" && user != nil {
				t.UploadedBy = user.Name()
			}
		} else if current.Traefik != nil {
			// Metadata-only edit: preserve the previous upload's
			// provenance so the UI keeps showing "traefik.yml ·
			// 2026-04-21 · stefaweb" accurately.
			t.SourceFile = current.Traefik.SourceFile
			t.UploadedAt = current.Traefik.UploadedAt
			t.UploadedBy = current.Traefik.UploadedBy
			// Same for the list fields when the caller deliberately
			// sent them empty (nil slice = "don't touch"): a Save
			// that only mutates StackID keeps the imported lists.
			if len(t.EntryPoints) == 0 {
				t.EntryPoints = current.Traefik.EntryPoints
			}
			if len(t.CertResolvers) == 0 {
				t.CertResolvers = current.Traefik.CertResolvers
			}
			if len(t.Middlewares) == 0 {
				t.Middlewares = current.Traefik.Middlewares
			}
			if len(t.Networks) == 0 {
				t.Networks = current.Traefik.Networks
			}
		}
		current.Traefik = &t
	}
	buf, err := json.Marshal(current)
	if err != nil {
		return err
	}
	return b.di.HostUpdateAddonConfigExtract(ctx, hostID, string(buf))
}

// ClearAddonConfigExtract wipes a single addon's subtree ("traefik", …) from
// the host's extract blob. When addon is empty, the entire blob is cleared.
func (b *hostBiz) ClearAddonConfigExtract(ctx context.Context, hostID, addon string) error {
	if hostID == "" {
		return errors.New("hostId is required")
	}
	host, err := b.di.HostGet(ctx, hostID)
	if err != nil {
		return err
	}
	if host == nil {
		return errors.New("host not found")
	}
	if addon == "" {
		return b.di.HostUpdateAddonConfigExtract(ctx, hostID, "")
	}
	current := decodeAddonConfigExtract(host.AddonConfigExtract)
	switch addon {
	case "traefik":
		current.Traefik = nil
	case "registryCache":
		current.RegistryCache = nil
	}
	buf, err := json.Marshal(current)
	if err != nil {
		return err
	}
	return b.di.HostUpdateAddonConfigExtract(ctx, hostID, string(buf))
}

// decodeAddonConfigExtract tolerates an empty string (new host with no
// upload yet) by returning a zero value. Parse errors are swallowed for the
// same reason — a corrupt blob must not block the editor from opening.
func decodeAddonConfigExtract(s string) *AddonConfigExtract {
	out := &AddonConfigExtract{}
	if s == "" {
		return out
	}
	_ = json.Unmarshal([]byte(s), out)
	return out
}

// dedup preserves order and strips duplicate + empty strings. Input may be
// nil (returns nil).
func dedup(in []string) []string {
	if len(in) == 0 {
		return in
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

