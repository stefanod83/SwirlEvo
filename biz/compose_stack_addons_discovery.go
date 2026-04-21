package biz

import (
	"context"
	"regexp"
	"strings"

	"github.com/cuigh/swirl/dao"
	"github.com/cuigh/swirl/docker"
	dockercontainer "github.com/docker/docker/api/types/container"
)

// AddonDiscoveryBiz reads runtime configuration from add-on containers/services
// (Traefik, Sablier, Watchtower, docker-backup-containers) already deployed on
// a target host, so the stack editor wizard tabs can populate dropdowns with
// real entrypoints / cert-resolvers / URLs / schedules instead of blind
// defaults. The discovery endpoint is read-only: Swirl never deploys the
// add-ons itself.
//
// Docker-inspect results are merged with the per-host AddonConfigExtract
// (persisted lists extracted from uploaded config files) so values defined
// in file provider mode (traefik.yml) still appear in the UI dropdowns.
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
// Host.AddonConfigExtract.
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

// NewAddonDiscovery wires the biz into the DI container.
func NewAddonDiscovery(d *docker.Docker, hb HostBiz) AddonDiscoveryBiz {
	return &addonDiscoveryBiz{d: d, hb: hb}
}

// Discover inspects the running containers on hostID and produces the set of
// add-on configurations the stack editor wizard can use. Never returns an
// error when the host is reachable but no add-ons are detected — it returns
// an empty *HostAddons instead, so the UI falls through to generic defaults.
func (b *addonDiscoveryBiz) Discover(ctx context.Context, hostID string) (*HostAddons, error) {
	if hostID == "" {
		return &HostAddons{}, nil
	}
	host, err := b.hb.Find(ctx, hostID)
	if err != nil {
		return nil, err
	}
	if host == nil {
		return &HostAddons{}, nil
	}
	// Federation hosts are not inspected locally — the live container
	// set lives on the remote Swirl. Callers that need the data make a
	// second round-trip through the federation proxy. Return an empty
	// shape + let the editor fall back to the persisted extract.
	if host.Type == "swarm_via_swirl" {
		return mergeHostExtract(host, &HostAddons{}), nil
	}

	// Client acquisition failures are silently degraded to "no discovery"
	// rather than bubbling up — a transient host outage must not block
	// the editor.
	cli, _, cerr := resolveHostClient(ctx, b.d, b.hb, hostID)
	if cerr != nil {
		return mergeHostExtract(host, &HostAddons{}), nil
	}

	// Enumerate all containers (including stopped so `docker stop traefik`
	// doesn't hide the config from the wizard when nothing's running).
	containers, err := cli.ContainerList(ctx, dockercontainer.ListOptions{All: true})
	if err != nil {
		return mergeHostExtract(host, &HostAddons{}), nil
	}

	out := &HostAddons{}
	for _, c := range containers {
		img := normalizeImage(c.Image)
		if out.Traefik == nil && isTraefikImage(img) {
			if detail, _, iErr := cli.ContainerInspectWithRaw(ctx, c.ID, false); iErr == nil {
				out.Traefik = parseTraefikAddon(detail.Config.Image, detail.Name, detail.Config.Cmd, detail.Args, detail.Config.Env)
			}
		}
	}
	return mergeHostExtract(host, out), nil
}

// isTraefikImage matches common Traefik image references — the official
// `traefik:vX` tag and the namespaced `traefik/traefik:vX` form are both
// accepted. Forks (e.g. `ghcr.io/mycompany/traefik:custom`) are not matched
// automatically; operators relying on those can populate the extract via
// file upload.
func isTraefikImage(img string) bool {
	img = strings.ToLower(img)
	// strip registry prefix if any (docker.io/, ghcr.io/... + /)
	if i := strings.LastIndex(img, "/"); i >= 0 {
		if strings.Contains(img[i+1:], "traefik") {
			return strings.HasPrefix(img[i+1:], "traefik")
		}
	}
	return strings.HasPrefix(img, "traefik:") || strings.HasPrefix(img, "traefik/traefik")
}

// parseTraefikAddon extracts entry points, cert resolvers, networks from a
// container's Cmd/Args/Env. All three sources are valid config surfaces:
//   - CLI args on `traefik` binary: `--entrypoints.web.address=:80`
//   - Env form:    `TRAEFIK_ENTRYPOINTS_WEB_ADDRESS=:80`
//   - Docker `CMD` vs `Args` split: ContainerInspect returns them separately.
//
// All sources are merged + deduped. When nothing is found the function
// returns a non-nil struct with empty lists so the UI can still render the
// "detected container name / image" block.
func parseTraefikAddon(image, name string, cmd []string, args []string, env []string) *TraefikAddon {
	addon := &TraefikAddon{
		ContainerName: strings.TrimPrefix(name, "/"),
		Image:         image,
		Version:       traefikVersion(image),
	}
	entryPoints := map[string]struct{}{}
	certResolvers := map[string]struct{}{}
	networks := map[string]struct{}{}

	combined := make([]string, 0, len(cmd)+len(args)+len(env))
	combined = append(combined, cmd...)
	combined = append(combined, args...)
	for _, e := range env {
		combined = append(combined, envToArgForm(e))
	}

	epRe := regexp.MustCompile(`(?i)^--entrypoints\.([A-Za-z0-9_-]+)\.`)
	crRe := regexp.MustCompile(`(?i)^--certificatesresolvers\.([A-Za-z0-9_-]+)\.`)
	netRe := regexp.MustCompile(`(?i)^--providers\.docker\.network=(.+)$`)
	sablierRe := regexp.MustCompile(`(?i)^--experimental\.plugins\.sablier\.`)

	for _, raw := range combined {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if m := epRe.FindStringSubmatch(s); len(m) > 1 {
			entryPoints[m[1]] = struct{}{}
			continue
		}
		if m := crRe.FindStringSubmatch(s); len(m) > 1 {
			certResolvers[m[1]] = struct{}{}
			continue
		}
		if m := netRe.FindStringSubmatch(s); len(m) > 1 {
			addon.DockerNetwork = m[1]
			networks[m[1]] = struct{}{}
			continue
		}
		if sablierRe.MatchString(s) {
			addon.SablierPlugin = true
		}
	}

	addon.EntryPoints = setToDiscoveryValues(entryPoints, "docker")
	addon.CertResolvers = setToDiscoveryValues(certResolvers, "docker")
	addon.Networks = setToDiscoveryValues(networks, "docker")
	return addon
}

// envToArgForm maps `TRAEFIK_FOO_BAR=value` to `--foo.bar=value` so the same
// regexes can parse both forms. The Traefik binary itself implements the
// inverse mapping; we mirror just enough of it for detection.
func envToArgForm(env string) string {
	parts := strings.SplitN(env, "=", 2)
	if len(parts) < 2 {
		return ""
	}
	key := strings.ToUpper(parts[0])
	if !strings.HasPrefix(key, "TRAEFIK_") {
		return ""
	}
	tail := strings.ToLower(strings.TrimPrefix(key, "TRAEFIK_"))
	tail = strings.ReplaceAll(tail, "_", ".")
	return "--" + tail + "=" + parts[1]
}

// traefikVersion extracts the X.Y major from an image tag. Accepts `vN` and
// plain `N` forms. Returns "" when the image is untagged or the tag doesn't
// parse.
var traefikVersionRe = regexp.MustCompile(`:v?(\d+)`)

func traefikVersion(image string) string {
	m := traefikVersionRe.FindStringSubmatch(image)
	if len(m) < 2 {
		return ""
	}
	return "v" + m[1]
}

func setToDiscoveryValues(set map[string]struct{}, origin string) []DiscoveryValue {
	if len(set) == 0 {
		return []DiscoveryValue{}
	}
	out := make([]DiscoveryValue, 0, len(set))
	for k := range set {
		out = append(out, DiscoveryValue{Name: k, Origin: origin})
	}
	return out
}

// mergeHostExtract overlays the persisted AddonConfigExtract (lists parsed
// from an uploaded traefik.yml) onto the live docker-derived addon data.
// Missing lists are appended; duplicates (by name) are skipped so an entry
// known from both sources is surfaced once with origin="docker".
func mergeHostExtract(host *dao.Host, live *HostAddons) *HostAddons {
	if host == nil || host.AddonConfigExtract == "" {
		return live
	}
	extract := decodeAddonConfigExtract(host.AddonConfigExtract)
	if extract.Traefik == nil {
		return live
	}
	if live.Traefik == nil {
		live.Traefik = &TraefikAddon{
			EntryPoints:   []DiscoveryValue{},
			CertResolvers: []DiscoveryValue{},
			Middlewares:   []DiscoveryValue{},
			Networks:      []DiscoveryValue{},
		}
	}
	live.Traefik.EntryPoints = mergeDiscoveryValues(live.Traefik.EntryPoints, extract.Traefik.EntryPoints)
	live.Traefik.CertResolvers = mergeDiscoveryValues(live.Traefik.CertResolvers, extract.Traefik.CertResolvers)
	live.Traefik.Middlewares = mergeDiscoveryValues(live.Traefik.Middlewares, extract.Traefik.Middlewares)
	live.Traefik.Networks = mergeDiscoveryValues(live.Traefik.Networks, extract.Traefik.Networks)
	return live
}

// mergeDiscoveryValues appends file-origin entries that aren't already in
// the docker-origin list. Order: docker-origin first (user-trusted live
// state), file-origin second.
func mergeDiscoveryValues(docker []DiscoveryValue, fileNames []string) []DiscoveryValue {
	seen := make(map[string]struct{}, len(docker)+len(fileNames))
	for _, v := range docker {
		seen[v.Name] = struct{}{}
	}
	out := append([]DiscoveryValue{}, docker...)
	for _, name := range fileNames {
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, DiscoveryValue{Name: name, Origin: "file"})
	}
	return out
}

