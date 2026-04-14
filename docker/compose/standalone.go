package compose

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	composetypes "github.com/cuigh/swirl/docker/compose/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"gopkg.in/yaml.v2"
)

// Standard docker-compose labels — same naming as the official CLI so containers
// created by `docker compose` are visible here as well.
const (
	LabelProject = "com.docker.compose.project"
	LabelService = "com.docker.compose.service"
	LabelNumber  = "com.docker.compose.container-number"
	LabelOneoff  = "com.docker.compose.oneoff"
	LabelManaged = "com.swirl.compose.managed"
)

// StackInfo is a summary of a compose stack discovered on a host.
type StackInfo struct {
	Name       string
	Services   []string
	Containers int
	Running    int
	Status     string
}

// DeployOptions controls deploy behaviour.
type DeployOptions struct {
	PullImages bool // pull each service image before creating containers
}

// StandaloneEngine deploys a docker-compose file on a single Docker daemon using the SDK only.
type StandaloneEngine struct {
	cli *client.Client
}

// NewStandaloneEngine wraps a live docker client.
func NewStandaloneEngine(cli *client.Client) *StandaloneEngine {
	return &StandaloneEngine{cli: cli}
}

// Deploy applies the compose file: creates networks, volumes, pulls images and starts
// one container per service. Re-invoking Deploy for an existing stack replaces it
// (stop+remove old containers, recreate). Volumes are preserved across redeploys.
func (e *StandaloneEngine) Deploy(ctx context.Context, projectName, content string, opts DeployOptions) error {
	cfg, err := Parse(projectName, content)
	if err != nil {
		return fmt.Errorf("parse compose: %w", err)
	}

	if err := e.removeProjectContainers(ctx, projectName, false); err != nil {
		return err
	}

	if err := e.ensureNetworks(ctx, projectName, cfg.Networks); err != nil {
		return err
	}
	if err := e.ensureVolumes(ctx, projectName, cfg.Volumes); err != nil {
		return err
	}

	for i, svc := range cfg.Services {
		if opts.PullImages && svc.Image != "" {
			if err := e.pullImage(ctx, svc.Image); err != nil {
				return fmt.Errorf("pull %s: %w", svc.Image, err)
			}
		}
		if err := e.createAndStart(ctx, projectName, cfg, &cfg.Services[i]); err != nil {
			return fmt.Errorf("service %s: %w", svc.Name, err)
		}
	}
	return nil
}

// Start starts all containers belonging to a project.
func (e *StandaloneEngine) Start(ctx context.Context, projectName string) error {
	containers, err := e.listProjectContainers(ctx, projectName, true)
	if err != nil {
		return err
	}
	for _, c := range containers {
		if c.State == "running" {
			continue
		}
		if err := e.cli.ContainerStart(ctx, c.ID, container.StartOptions{}); err != nil {
			return err
		}
	}
	return nil
}

// Stop stops all running containers of a project.
func (e *StandaloneEngine) Stop(ctx context.Context, projectName string) error {
	containers, err := e.listProjectContainers(ctx, projectName, true)
	if err != nil {
		return err
	}
	for _, c := range containers {
		if c.State != "running" {
			continue
		}
		if err := e.cli.ContainerStop(ctx, c.ID, container.StopOptions{}); err != nil {
			return err
		}
	}
	return nil
}

// Remove stops and deletes all resources of a project.
// When removeVolumes is true, project-labeled volumes are removed too.
func (e *StandaloneEngine) Remove(ctx context.Context, projectName string, removeVolumes bool) error {
	if err := e.removeProjectContainers(ctx, projectName, true); err != nil {
		return err
	}
	// remove project networks
	nets, err := e.cli.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", LabelProject+"="+projectName)),
	})
	if err == nil {
		for _, n := range nets {
			_ = e.cli.NetworkRemove(ctx, n.ID)
		}
	}
	if removeVolumes {
		vols, err := e.cli.VolumeList(ctx, volume.ListOptions{
			Filters: filters.NewArgs(filters.Arg("label", LabelProject+"="+projectName)),
		})
		if err == nil {
			for _, v := range vols.Volumes {
				_ = e.cli.VolumeRemove(ctx, v.Name, false)
			}
		}
	}
	return nil
}

// List returns all compose stacks discovered on the host (grouped by project label).
func (e *StandaloneEngine) List(ctx context.Context) ([]StackInfo, error) {
	all, err := e.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", LabelProject)),
	})
	if err != nil {
		return nil, err
	}
	byProject := map[string]*StackInfo{}
	for _, c := range all {
		name := c.Labels[LabelProject]
		if name == "" {
			continue
		}
		s, ok := byProject[name]
		if !ok {
			s = &StackInfo{Name: name}
			byProject[name] = s
		}
		s.Containers++
		if c.State == "running" {
			s.Running++
		}
		svc := c.Labels[LabelService]
		if svc != "" && !containsStr(s.Services, svc) {
			s.Services = append(s.Services, svc)
		}
	}
	out := make([]StackInfo, 0, len(byProject))
	for _, s := range byProject {
		switch {
		case s.Running == 0:
			s.Status = "inactive"
		case s.Running == s.Containers:
			s.Status = "active"
		default:
			s.Status = "partial"
		}
		out = append(out, *s)
	}
	return out, nil
}

// ==== internal helpers ====

func (e *StandaloneEngine) listProjectContainers(ctx context.Context, project string, includeStopped bool) ([]container.Summary, error) {
	return e.cli.ContainerList(ctx, container.ListOptions{
		All:     includeStopped,
		Filters: filters.NewArgs(filters.Arg("label", LabelProject+"="+project)),
	})
}

func (e *StandaloneEngine) removeProjectContainers(ctx context.Context, project string, removeAll bool) error {
	list, err := e.listProjectContainers(ctx, project, true)
	if err != nil {
		return err
	}
	for _, c := range list {
		if c.State == "running" {
			_ = e.cli.ContainerStop(ctx, c.ID, container.StopOptions{})
		}
		if err := e.cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); err != nil {
			if !removeAll {
				return err
			}
		}
	}
	return nil
}

func (e *StandaloneEngine) ensureNetworks(ctx context.Context, project string, nets map[string]composetypes.NetworkConfig) error {
	existing, _ := e.cli.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", LabelProject+"="+project)),
	})
	existingByName := map[string]struct{}{}
	for _, n := range existing {
		existingByName[n.Name] = struct{}{}
	}
	if len(nets) == 0 {
		// implicit "default" network
		nets = map[string]composetypes.NetworkConfig{"default": {}}
	}
	for name, ncfg := range nets {
		if ncfg.External.External {
			continue
		}
		qualified := project + "_" + name
		if _, ok := existingByName[qualified]; ok {
			continue
		}
		labels := map[string]string{LabelProject: project, LabelManaged: "true"}
		for k, v := range ncfg.Labels {
			labels[k] = v
		}
		opts := network.CreateOptions{
			Driver: ncfg.Driver,
			Labels: labels,
		}
		if opts.Driver == "" {
			opts.Driver = "bridge"
		}
		if _, err := e.cli.NetworkCreate(ctx, qualified, opts); err != nil {
			return fmt.Errorf("create network %s: %w", qualified, err)
		}
	}
	return nil
}

func (e *StandaloneEngine) ensureVolumes(ctx context.Context, project string, vols map[string]composetypes.VolumeConfig) error {
	for name, vcfg := range vols {
		if vcfg.External.External {
			continue
		}
		qualified := project + "_" + name
		_, err := e.cli.VolumeInspect(ctx, qualified)
		if err == nil {
			continue
		}
		labels := map[string]string{LabelProject: project, LabelManaged: "true"}
		for k, v := range vcfg.Labels {
			labels[k] = v
		}
		if _, err := e.cli.VolumeCreate(ctx, volume.CreateOptions{
			Name:   qualified,
			Driver: vcfg.Driver,
			Labels: labels,
		}); err != nil {
			return fmt.Errorf("create volume %s: %w", qualified, err)
		}
	}
	return nil
}

func (e *StandaloneEngine) pullImage(ctx context.Context, ref string) error {
	rc, err := e.cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return err
	}
	defer rc.Close()
	_, err = io.Copy(io.Discard, rc)
	return err
}

func (e *StandaloneEngine) createAndStart(ctx context.Context, project string, cfg *composetypes.Config, svc *composetypes.ServiceConfig) error {
	labels := map[string]string{
		LabelProject: project,
		LabelService: svc.Name,
		LabelNumber:  "1",
		LabelManaged: "true",
	}
	for k, v := range svc.Labels {
		labels[k] = v
	}

	env := make([]string, 0, len(svc.Environment))
	for k, v := range svc.Environment {
		if v == nil {
			env = append(env, k)
		} else {
			env = append(env, fmt.Sprintf("%s=%s", k, *v))
		}
	}

	// ports
	exposed := nat.PortSet{}
	bindings := nat.PortMap{}
	for _, p := range svc.Ports {
		proto := p.Protocol
		if proto == "" {
			proto = "tcp"
		}
		containerPort := nat.Port(fmt.Sprintf("%d/%s", p.Target, proto))
		exposed[containerPort] = struct{}{}
		if p.Published > 0 {
			bindings[containerPort] = append(bindings[containerPort], nat.PortBinding{
				HostPort: fmt.Sprintf("%d", p.Published),
			})
		}
	}

	// volumes / mounts
	var mounts []mount.Mount
	for _, v := range svc.Volumes {
		m := mount.Mount{Target: v.Target, ReadOnly: v.ReadOnly}
		switch v.Type {
		case "bind":
			m.Type = mount.TypeBind
			m.Source = v.Source
		case "tmpfs":
			m.Type = mount.TypeTmpfs
		default:
			// named volume
			m.Type = mount.TypeVolume
			if v.Source != "" {
				if _, defined := cfg.Volumes[v.Source]; defined {
					m.Source = project + "_" + v.Source
				} else {
					m.Source = v.Source
				}
			}
		}
		mounts = append(mounts, m)
	}

	// networks: first listed (alphabetical or explicit) becomes primary
	var primaryNet string
	aliases := map[string][]string{}
	networksOrder := []string{}
	for n, cfgN := range svc.Networks {
		networksOrder = append(networksOrder, n)
		if cfgN != nil {
			aliases[n] = cfgN.Aliases
		}
	}
	if len(networksOrder) == 0 {
		networksOrder = []string{"default"}
	}
	primaryNet = qualifyNetwork(project, cfg.Networks, networksOrder[0])

	restart := container.RestartPolicy{}
	switch strings.ToLower(svc.Restart) {
	case "always":
		restart.Name = container.RestartPolicyAlways
	case "on-failure":
		restart.Name = container.RestartPolicyOnFailure
	case "unless-stopped":
		restart.Name = container.RestartPolicyUnlessStopped
	case "no", "":
		restart.Name = container.RestartPolicyDisabled
	}

	containerName := svc.ContainerName
	if containerName == "" {
		containerName = project + "_" + svc.Name + "_1"
	}

	ccfg := &container.Config{
		Hostname:     svc.Hostname,
		User:         svc.User,
		Env:          env,
		Image:        svc.Image,
		Labels:       labels,
		ExposedPorts: exposed,
		WorkingDir:   svc.WorkingDir,
		Tty:          svc.Tty,
		OpenStdin:    svc.StdinOpen,
	}
	if len(svc.Entrypoint) > 0 {
		ccfg.Entrypoint = []string(svc.Entrypoint)
	}
	if len(svc.Command) > 0 {
		ccfg.Cmd = []string(svc.Command)
	}

	hcfg := &container.HostConfig{
		PortBindings:  bindings,
		Mounts:        mounts,
		RestartPolicy: restart,
		Privileged:    svc.Privileged,
		ReadonlyRootfs: svc.ReadOnly,
		NetworkMode:   container.NetworkMode(primaryNet),
		CapAdd:        svc.CapAdd,
		CapDrop:       svc.CapDrop,
		DNS:           svc.DNS,
		DNSSearch:     svc.DNSSearch,
	}

	netCfg := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			primaryNet: {Aliases: aliases[networksOrder[0]]},
		},
	}

	resp, err := e.cli.ContainerCreate(ctx, ccfg, hcfg, netCfg, nil, containerName)
	if err != nil {
		return err
	}

	// connect additional networks
	for _, n := range networksOrder[1:] {
		qn := qualifyNetwork(project, cfg.Networks, n)
		if err := e.cli.NetworkConnect(ctx, qn, resp.ID, &network.EndpointSettings{
			Aliases: aliases[n],
		}); err != nil {
			return err
		}
	}

	return e.cli.ContainerStart(ctx, resp.ID, container.StartOptions{})
}

func qualifyNetwork(project string, declared map[string]composetypes.NetworkConfig, name string) string {
	if cfg, ok := declared[name]; ok && cfg.External.External {
		if cfg.Name != "" {
			return cfg.Name
		}
		return name
	}
	return project + "_" + name
}

func containsStr(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// ProjectDetail collects the live state of a compose project on a host.
type ProjectDetail struct {
	Name       string
	Status     string
	Services   []string
	Containers []container.Summary
	Networks   []string
	Volumes    []string
}

// GetProject returns the full project detail by scanning containers with the
// matching com.docker.compose.project label.
func (e *StandaloneEngine) GetProject(ctx context.Context, projectName string) (*ProjectDetail, error) {
	containers, err := e.listProjectContainers(ctx, projectName, true)
	if err != nil {
		return nil, err
	}
	pd := &ProjectDetail{Name: projectName, Containers: containers}

	serviceSet := map[string]struct{}{}
	netSet := map[string]struct{}{}
	volSet := map[string]struct{}{}
	running := 0

	for _, c := range containers {
		if c.State == "running" {
			running++
		}
		if svc, ok := c.Labels[LabelService]; ok && svc != "" {
			serviceSet[svc] = struct{}{}
		}
		for _, m := range c.Mounts {
			if m.Type == mount.TypeVolume && m.Name != "" {
				volSet[m.Name] = struct{}{}
			}
		}
		if c.NetworkSettings != nil {
			for n := range c.NetworkSettings.Networks {
				netSet[n] = struct{}{}
			}
		}
	}

	switch {
	case len(containers) == 0:
		pd.Status = "inactive"
	case running == 0:
		pd.Status = "inactive"
	case running == len(containers):
		pd.Status = "active"
	default:
		pd.Status = "partial"
	}

	pd.Services = sortedSetKeys(serviceSet)
	pd.Networks = sortedSetKeys(netSet)
	pd.Volumes = sortedSetKeys(volSet)
	return pd, nil
}

func sortedSetKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ReconstructCompose inspects each container of a project and emits a best-effort
// docker-compose v3 YAML that can be fed back to Deploy. It covers the subset of
// fields actually supported by StandaloneEngine.createAndStart. Fields not
// derivable at runtime (build, healthcheck definition, secrets, configs,
// deploy, depends_on) are omitted — the caller is expected to surface a
// warning in the UI.
func (e *StandaloneEngine) ReconstructCompose(ctx context.Context, projectName string) (string, error) {
	containers, err := e.listProjectContainers(ctx, projectName, true)
	if err != nil {
		return "", err
	}
	if len(containers) == 0 {
		return "", fmt.Errorf("project %q has no containers", projectName)
	}

	type portMapping struct {
		Target    uint32 `yaml:"target"`
		Published uint32 `yaml:"published,omitempty"`
		Protocol  string `yaml:"protocol,omitempty"`
	}
	type volumeMapping struct {
		Type     string `yaml:"type,omitempty"`
		Source   string `yaml:"source,omitempty"`
		Target   string `yaml:"target"`
		ReadOnly bool   `yaml:"read_only,omitempty"`
	}
	type serviceSpec struct {
		Image       string            `yaml:"image,omitempty"`
		Entrypoint  []string          `yaml:"entrypoint,omitempty"`
		Command     []string          `yaml:"command,omitempty"`
		Environment []string          `yaml:"environment,omitempty"`
		Labels      map[string]string `yaml:"labels,omitempty"`
		Ports       []portMapping     `yaml:"ports,omitempty"`
		Volumes     []volumeMapping   `yaml:"volumes,omitempty"`
		Networks    []string          `yaml:"networks,omitempty"`
		Restart     string            `yaml:"restart,omitempty"`
		User        string            `yaml:"user,omitempty"`
		WorkingDir  string            `yaml:"working_dir,omitempty"`
		Hostname    string            `yaml:"hostname,omitempty"`
		Tty         bool              `yaml:"tty,omitempty"`
		StdinOpen   bool              `yaml:"stdin_open,omitempty"`
		Privileged  bool              `yaml:"privileged,omitempty"`
		ReadOnly    bool              `yaml:"read_only,omitempty"`
		CapAdd      []string          `yaml:"cap_add,omitempty"`
		CapDrop     []string          `yaml:"cap_drop,omitempty"`
		DNS         []string          `yaml:"dns,omitempty"`
		DNSSearch   []string          `yaml:"dns_search,omitempty"`
	}
	type spec struct {
		Version  string                     `yaml:"version,omitempty"`
		Services map[string]*serviceSpec    `yaml:"services"`
		Networks map[string]map[string]any  `yaml:"networks,omitempty"`
		Volumes  map[string]map[string]any  `yaml:"volumes,omitempty"`
	}

	out := spec{
		Version:  "3.8",
		Services: map[string]*serviceSpec{},
		Networks: map[string]map[string]any{},
		Volumes:  map[string]map[string]any{},
	}

	for _, sum := range containers {
		// prefer the compose service label; fall back to a cleaned container name
		svcName := sum.Labels[LabelService]
		if svcName == "" {
			svcName = strings.TrimPrefix(sum.Names[0], "/")
			svcName = strings.TrimPrefix(svcName, projectName+"_")
			svcName = strings.TrimSuffix(svcName, "_1")
		}
		if _, dup := out.Services[svcName]; dup {
			continue // one service may have multiple containers; reuse the first
		}

		full, err := e.cli.ContainerInspect(ctx, sum.ID)
		if err != nil {
			return "", fmt.Errorf("inspect %s: %w", sum.ID[:12], err)
		}

		s := &serviceSpec{
			Image:      normalizeImage(full.Config.Image),
			WorkingDir: full.Config.WorkingDir,
			User:       full.Config.User,
			Hostname:   full.Config.Hostname,
			Tty:        full.Config.Tty,
			StdinOpen:  full.Config.OpenStdin,
			Labels:     stripInternalLabels(full.Config.Labels),
		}
		if len(full.Config.Entrypoint) > 0 {
			s.Entrypoint = []string(full.Config.Entrypoint)
		}
		if len(full.Config.Cmd) > 0 {
			s.Command = []string(full.Config.Cmd)
		}
		for _, e := range full.Config.Env {
			s.Environment = append(s.Environment, e)
		}

		// Ports
		if full.HostConfig != nil {
			for port, bindings := range full.HostConfig.PortBindings {
				tgt := uint32(port.Int())
				proto := port.Proto()
				if len(bindings) == 0 {
					s.Ports = append(s.Ports, portMapping{Target: tgt, Protocol: cleanProto(proto)})
					continue
				}
				for _, b := range bindings {
					var pub uint32
					fmt.Sscanf(b.HostPort, "%d", &pub)
					s.Ports = append(s.Ports, portMapping{Target: tgt, Published: pub, Protocol: cleanProto(proto)})
				}
			}
			// Mounts
			for _, m := range full.HostConfig.Mounts {
				vm := volumeMapping{Target: m.Target, ReadOnly: m.ReadOnly}
				switch m.Type {
				case mount.TypeBind:
					vm.Type = "bind"
					vm.Source = m.Source
				case mount.TypeTmpfs:
					vm.Type = "tmpfs"
				case mount.TypeVolume:
					vm.Type = "volume"
					vm.Source = strings.TrimPrefix(m.Source, projectName+"_")
					if strings.HasPrefix(m.Source, projectName+"_") {
						out.Volumes[vm.Source] = map[string]any{}
					} else {
						vm.Source = m.Source
						out.Volumes[m.Source] = map[string]any{"external": true, "name": m.Source}
					}
				}
				s.Volumes = append(s.Volumes, vm)
			}
			// Restart
			switch full.HostConfig.RestartPolicy.Name {
			case container.RestartPolicyAlways:
				s.Restart = "always"
			case container.RestartPolicyOnFailure:
				s.Restart = "on-failure"
			case container.RestartPolicyUnlessStopped:
				s.Restart = "unless-stopped"
			}
			s.Privileged = full.HostConfig.Privileged
			s.ReadOnly = full.HostConfig.ReadonlyRootfs
			s.CapAdd = append(s.CapAdd, full.HostConfig.CapAdd...)
			s.CapDrop = append(s.CapDrop, full.HostConfig.CapDrop...)
			s.DNS = append(s.DNS, full.HostConfig.DNS...)
			s.DNSSearch = append(s.DNSSearch, full.HostConfig.DNSSearch...)
		}

		// Networks
		if full.NetworkSettings != nil {
			for n := range full.NetworkSettings.Networks {
				short := strings.TrimPrefix(n, projectName+"_")
				s.Networks = append(s.Networks, short)
				if strings.HasPrefix(n, projectName+"_") {
					out.Networks[short] = map[string]any{}
				} else {
					out.Networks[short] = map[string]any{"external": true, "name": n}
				}
			}
			sort.Strings(s.Networks)
		}

		out.Services[svcName] = s
	}

	buf, err := yaml.Marshal(&out)
	if err != nil {
		return "", err
	}
	header := "# Reconstructed from running containers. Review before deploying.\n" +
		"# Fields not derivable at runtime (build, healthcheck, secrets, configs,\n" +
		"# deploy, depends_on) are omitted.\n\n"
	return header + string(buf), nil
}

func normalizeImage(ref string) string {
	if i := strings.Index(ref, "@sha256:"); i > 0 {
		return ref[:i]
	}
	return ref
}

func cleanProto(p string) string {
	if p == "tcp" {
		return ""
	}
	return p
}

// stripInternalLabels removes labels added by Swirl/docker-compose internals so
// the reconstructed YAML doesn't echo them back.
func stripInternalLabels(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := map[string]string{}
	for k, v := range in {
		if strings.HasPrefix(k, "com.docker.compose.") || strings.HasPrefix(k, "com.swirl.compose.") {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
