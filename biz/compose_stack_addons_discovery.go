package biz

import (
	"context"

	"github.com/cuigh/swirl/docker"
)

// AddonDiscoveryBiz reads runtime configuration from add-on containers/services
// (Traefik, Sablier, Watchtower, docker-backup-containers) already deployed on
// a target host, so the stack editor wizard tabs can populate dropdowns with
// real entrypoints / cert-resolvers / URLs / schedules instead of blind
// defaults. The discovery endpoint is read-only: Swirl never deploys the
// add-ons itself.
//
// Phase 1 ships an empty stub — every field is nil. Phase 3 fills Traefik,
// subsequent phases fill the rest.
type AddonDiscoveryBiz interface {
	Discover(ctx context.Context, hostID string) (*HostAddons, error)
}

// HostAddons groups the detected configuration for every known add-on on a
// single host. Fields are nil when the corresponding add-on is not detected;
// the UI must tolerate every field being nil (generic defaults + warning).
type HostAddons struct {
	Traefik    *TraefikAddon    `json:"traefik,omitempty"`
	Sablier    *SablierAddon    `json:"sablier,omitempty"`
	Watchtower *WatchtowerAddon `json:"watchtower,omitempty"`
	Backup     *BackupAddon     `json:"backup,omitempty"`
}

// TraefikAddon mirrors the subset of Traefik static configuration the wizard
// needs to populate the Traefik tab: entry points, cert resolvers, networks.
// Origin is "docker" for values extracted via ContainerInspect and "file"
// for values merged in from an uploaded traefik.yml stored in
// Host.AddonConfigExtract (Phase 3).
type TraefikAddon struct {
	ContainerName string           `json:"containerName,omitempty"`
	Image         string           `json:"image,omitempty"`
	Version       string           `json:"version,omitempty"`
	EntryPoints   []DiscoveryValue `json:"entryPoints"`
	CertResolvers []DiscoveryValue `json:"certResolvers"`
	Middlewares   []DiscoveryValue `json:"middlewares"`
	Networks      []DiscoveryValue `json:"networks"`
	DockerNetwork string           `json:"dockerNetwork,omitempty"`
	SablierPlugin bool             `json:"sablierPlugin,omitempty"`
}

// DiscoveryValue is a (name, origin) pair so the UI can badge each dropdown
// entry with its provenance ("docker" vs "file").
type DiscoveryValue struct {
	Name   string `json:"name"`
	Origin string `json:"origin"`
}

// SablierAddon / WatchtowerAddon / BackupAddon are placeholders filled by
// later phases. They are declared here so the JSON shape of /host-addons is
// stable from Phase 1 onward and the frontend types compile against it.
type SablierAddon struct {
	ContainerName string   `json:"containerName,omitempty"`
	Image         string   `json:"image,omitempty"`
	URL           string   `json:"url,omitempty"`
	Networks      []string `json:"networks,omitempty"`
}

type WatchtowerAddon struct {
	ContainerName  string `json:"containerName,omitempty"`
	Image          string `json:"image,omitempty"`
	LabelEnable    bool   `json:"labelEnable,omitempty"`
	IncludeStopped bool   `json:"includeStopped,omitempty"`
	PollInterval   int    `json:"pollInterval,omitempty"`
}

type BackupAddon struct {
	ContainerName string `json:"containerName,omitempty"`
	Image         string `json:"image,omitempty"`
	Schedule      string `json:"schedule,omitempty"`
	RetentionEnv  string `json:"retentionEnv,omitempty"`
	TargetDir     string `json:"targetDir,omitempty"`
}

type addonDiscoveryBiz struct {
	d  *docker.Docker
	hb HostBiz
}

// NewAddonDiscovery wires the biz into the DI container. The stub
// implementation returns an empty HostAddons so downstream callers don't
// have to nil-check the biz itself.
func NewAddonDiscovery(d *docker.Docker, hb HostBiz) AddonDiscoveryBiz {
	return &addonDiscoveryBiz{d: d, hb: hb}
}

// Discover returns an empty *HostAddons in Phase 1. Per-addon detection is
// wired in Phase 3 onward. The hostID is still validated so the endpoint
// round-trips realistically.
func (b *addonDiscoveryBiz) Discover(ctx context.Context, hostID string) (*HostAddons, error) {
	if hostID != "" {
		if _, err := b.hb.Find(ctx, hostID); err != nil {
			return nil, err
		}
	}
	return &HostAddons{}, nil
}
