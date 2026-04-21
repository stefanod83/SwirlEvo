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
	Traefik *TraefikExtract `json:"traefik,omitempty"`
}

// TraefikExtract carries the subset of Traefik static configuration the
// editor wizard needs, stripped of ACME keys / secrets. What lands in the DB
// is only the NAMES (of entrypoints, cert resolvers, etc.) — the raw file is
// never persisted.
type TraefikExtract struct {
	EntryPoints   []string  `json:"entryPoints"`
	CertResolvers []string  `json:"certResolvers"`
	Middlewares   []string  `json:"middlewares"`
	Networks      []string  `json:"networks"`
	// SourceFile is the filename the operator uploaded (display-only,
	// e.g. "traefik.yml"). No path, just the basename.
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
	// others. Unset subtrees are left as-is.
	current := decodeAddonConfigExtract(host.AddonConfigExtract)
	if extract != nil && extract.Traefik != nil {
		t := *extract.Traefik
		t.EntryPoints = dedup(t.EntryPoints)
		t.CertResolvers = dedup(t.CertResolvers)
		t.Middlewares = dedup(t.Middlewares)
		t.Networks = dedup(t.Networks)
		if t.UploadedAt.IsZero() {
			t.UploadedAt = time.Now()
		}
		if t.UploadedBy == "" && user != nil {
			t.UploadedBy = user.Name()
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

