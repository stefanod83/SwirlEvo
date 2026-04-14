package compose

import (
	"context"
	"fmt"
	"io"
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
