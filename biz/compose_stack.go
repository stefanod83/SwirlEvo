package biz

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/cuigh/auxo/app/container"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/dao"
	"github.com/cuigh/swirl/docker"
	"github.com/cuigh/swirl/docker/compose"
	dockerclient "github.com/docker/docker/client"
)

// ComposeStackBiz exposes Portainer-style compose stack management for standalone mode.
type ComposeStackBiz interface {
	Search(ctx context.Context, args *dao.ComposeStackSearchArgs) ([]*ComposeStackSummary, int, error)
	Find(ctx context.Context, id string) (*dao.ComposeStack, error)
	// FindDetail returns the enriched detail of a stack (managed or external).
	FindDetail(ctx context.Context, hostID, name string) (*ComposeStackDetail, error)
	// Save persists the compose stack without deploying. Pass an empty ID to create a new one.
	Save(ctx context.Context, stack *dao.ComposeStack, user web.User) (string, error)
	// Deploy parses + applies the compose file. If the stack does not exist yet it is persisted first.
	Deploy(ctx context.Context, stack *dao.ComposeStack, pullImages bool, user web.User) (string, error)
	// Import promotes an external stack to managed. If stack.Content is empty,
	// the engine reconstructs a YAML from running containers. If redeploy is
	// true, the stack is (re)deployed against the imported/edited YAML.
	Import(ctx context.Context, stack *dao.ComposeStack, redeploy, pullImages bool, user web.User) (string, error)
	Start(ctx context.Context, id string, user web.User) error
	Stop(ctx context.Context, id string, user web.User) error
	Remove(ctx context.Context, id string, removeVolumes bool, user web.User) error
	// External actions — act directly on a discovered stack by (hostID, name).
	StartExternal(ctx context.Context, hostID, name string, user web.User) error
	StopExternal(ctx context.Context, hostID, name string, user web.User) error
	RemoveExternal(ctx context.Context, hostID, name string, removeVolumes bool, user web.User) error
}

// ComposeStackDetail is the enriched detail returned for a single stack.
type ComposeStackDetail struct {
	ID            string                  `json:"id,omitempty"`
	HostID        string                  `json:"hostId"`
	HostName      string                  `json:"hostName,omitempty"`
	Name          string                  `json:"name"`
	Content       string                  `json:"content,omitempty"`
	Reconstructed bool                    `json:"reconstructed"`
	Status        string                  `json:"status"`
	Managed       bool                    `json:"managed"`
	Services      []string                `json:"services"`
	Networks      []string                `json:"networks"`
	Volumes       []string                `json:"volumes"`
	Containers    []ComposeContainerBrief `json:"containers"`
	UpdatedAt     string                  `json:"updatedAt,omitempty"`
}

// ComposeContainerBrief is a lightweight container summary embedded in a stack detail.
type ComposeContainerBrief struct {
	ID      string           `json:"id"`
	Name    string           `json:"name"`
	Service string           `json:"service,omitempty"`
	Image   string           `json:"image"`
	State   string           `json:"state"`
	Status  string           `json:"status"`
	Ports   []*ContainerPort `json:"ports,omitempty"`
	Created string           `json:"created"`
}

// ComposeStackSummary is returned in list responses.
type ComposeStackSummary struct {
	ID         string `json:"id"`
	HostID     string `json:"hostId"`
	HostName   string `json:"hostName,omitempty"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Containers int    `json:"containers"`
	Running    int    `json:"running"`
	Services   int    `json:"services"`
	Managed    bool   `json:"managed"`
	UpdatedAt  string `json:"updatedAt,omitempty"`
}

type composeStackBiz struct {
	d   *docker.Docker
	hb  HostBiz
	eb  EventBiz
	di  dao.Interface
	sec ComposeStackSecretBiz
}

// NewComposeStack is registered in biz.init.
func NewComposeStack(d *docker.Docker, hb HostBiz, eb EventBiz, di dao.Interface, sec ComposeStackSecretBiz) ComposeStackBiz {
	return &composeStackBiz{d: d, hb: hb, eb: eb, di: di, sec: sec}
}

func (b *composeStackBiz) Search(ctx context.Context, args *dao.ComposeStackSearchArgs) ([]*ComposeStackSummary, int, error) {
	// Persisted stacks
	persisted, _, err := b.di.ComposeStackSearch(ctx, args)
	if err != nil {
		return nil, 0, err
	}

	// Build host name lookup
	hosts, err := b.hb.GetAll(ctx)
	if err != nil {
		return nil, 0, err
	}
	hostName := map[string]string{}
	for _, h := range hosts {
		hostName[h.ID] = h.Name
	}

	// Discover live compose projects on each host (to catch stacks created outside Swirl too)
	discovered := map[string]*compose.StackInfo{} // key: hostID + "|" + project
	for _, h := range hosts {
		if args.HostID != "" && h.ID != args.HostID {
			continue
		}
		if h.Status != "connected" {
			continue
		}
		cli, cErr := b.d.Hosts.GetClient(h.ID, h.Endpoint)
		if cErr != nil {
			continue
		}
		engine := compose.NewStandaloneEngine(cli)
		info, lErr := engine.List(ctx)
		if lErr != nil {
			continue
		}
		for i := range info {
			discovered[h.ID+"|"+info[i].Name] = &info[i]
		}
	}

	summaries := make([]*ComposeStackSummary, 0, len(persisted)+len(discovered))
	seen := map[string]bool{}

	for _, s := range persisted {
		key := s.HostID + "|" + s.Name
		seen[key] = true
		sum := &ComposeStackSummary{
			ID:        s.ID,
			HostID:    s.HostID,
			HostName:  hostName[s.HostID],
			Name:      s.Name,
			Status:    s.Status,
			Managed:   true,
			UpdatedAt: formatTime(time.Time(s.UpdatedAt)),
		}
		if info, ok := discovered[key]; ok {
			sum.Status = info.Status
			sum.Containers = info.Containers
			sum.Running = info.Running
			sum.Services = len(info.Services)
		}
		summaries = append(summaries, sum)
	}

	// Un-managed stacks discovered on hosts (created with docker compose CLI externally)
	for key, info := range discovered {
		if seen[key] {
			continue
		}
		parts := split2(key, "|")
		sum := &ComposeStackSummary{
			HostID:     parts[0],
			HostName:   hostName[parts[0]],
			Name:       info.Name,
			Status:     info.Status,
			Containers: info.Containers,
			Running:    info.Running,
			Services:   len(info.Services),
			Managed:    false,
		}
		if args.Name != "" && !containsIgnoreCase(sum.Name, args.Name) {
			continue
		}
		summaries = append(summaries, sum)
	}

	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].HostName != summaries[j].HostName {
			return summaries[i].HostName < summaries[j].HostName
		}
		return summaries[i].Name < summaries[j].Name
	})
	return summaries, len(summaries), nil
}

func (b *composeStackBiz) Find(ctx context.Context, id string) (*dao.ComposeStack, error) {
	return b.di.ComposeStackGet(ctx, id)
}

func (b *composeStackBiz) Save(ctx context.Context, stack *dao.ComposeStack, user web.User) (string, error) {
	if stack.HostID == "" || stack.Name == "" {
		return "", errors.New("hostId and name are required")
	}
	stack.UpdatedAt = now()
	stack.UpdatedBy = newOperator(user)
	if stack.ID == "" {
		stack.ID = createId()
		stack.CreatedAt = stack.UpdatedAt
		stack.CreatedBy = stack.UpdatedBy
		if stack.Status == "" {
			stack.Status = "inactive"
		}
		if err := b.di.ComposeStackCreate(ctx, stack); err != nil {
			return "", err
		}
		b.eb.CreateStack(EventActionCreate, stack.HostID, stack.Name, user)
	} else {
		if err := b.di.ComposeStackUpdate(ctx, stack); err != nil {
			return "", err
		}
		b.eb.CreateStack(EventActionUpdate, stack.HostID, stack.Name, user)
	}
	return stack.ID, nil
}

func (b *composeStackBiz) Deploy(ctx context.Context, stack *dao.ComposeStack, pullImages bool, user web.User) (string, error) {
	id, err := b.Save(ctx, stack, user)
	if err != nil {
		return "", err
	}

	host, err := b.hb.Find(ctx, stack.HostID)
	if err != nil {
		return "", err
	}
	if host == nil {
		return "", errors.New("host not found")
	}
	cli, err := b.d.Hosts.GetClient(host.ID, host.Endpoint)
	if err != nil {
		return "", err
	}
	engine := compose.NewStandaloneEngine(cli)
	// Build a deploy hook that materializes VaultSecret bindings (if any).
	// A nil hook is perfectly valid — the engine skips over it.
	hook, err := b.sec.NewHook(ctx, id)
	if err != nil {
		errMsg := fmt.Sprintf("prepare secrets: %v", err)
		_ = b.di.ComposeStackUpdateStatus(ctx, id, "error")
		_ = b.di.ComposeStackUpdateError(ctx, id, errMsg)
		return id, fmt.Errorf("%s", errMsg)
	}
	if err := engine.Deploy(ctx, stack.Name, stack.Content, compose.DeployOptions{
		PullImages: pullImages,
		Hook:       hook,
	}); err != nil {
		_ = b.di.ComposeStackUpdateStatus(ctx, id, "error")
		_ = b.di.ComposeStackUpdateError(ctx, id, err.Error())
		return id, err
	}
	_ = b.di.ComposeStackUpdateStatus(ctx, id, "active")
	_ = b.di.ComposeStackUpdateError(ctx, id, "") // clear previous error on success
	b.eb.CreateStack(EventActionDeploy, stack.HostID, stack.Name, user)
	return id, nil
}

func (b *composeStackBiz) Start(ctx context.Context, id string, user web.User) error {
	stack, err := b.di.ComposeStackGet(ctx, id)
	if err != nil || stack == nil {
		if stack == nil {
			return errors.New("stack not found")
		}
		return err
	}
	cli, err := b.clientForStack(ctx, stack)
	if err != nil {
		return err
	}
	engine := compose.NewStandaloneEngine(cli)
	if err := engine.Start(ctx, stack.Name); err != nil {
		return err
	}
	_ = b.di.ComposeStackUpdateStatus(ctx, id, "active")
	b.eb.CreateStack(EventActionStart, stack.HostID, stack.Name, user)
	return nil
}

func (b *composeStackBiz) Stop(ctx context.Context, id string, user web.User) error {
	stack, err := b.di.ComposeStackGet(ctx, id)
	if err != nil || stack == nil {
		if stack == nil {
			return errors.New("stack not found")
		}
		return err
	}
	cli, err := b.clientForStack(ctx, stack)
	if err != nil {
		return err
	}
	engine := compose.NewStandaloneEngine(cli)
	if err := engine.Stop(ctx, stack.Name); err != nil {
		return err
	}
	_ = b.di.ComposeStackUpdateStatus(ctx, id, "inactive")
	b.eb.CreateStack(EventActionShutdown, stack.HostID, stack.Name, user)
	return nil
}

func (b *composeStackBiz) Remove(ctx context.Context, id string, removeVolumes bool, user web.User) error {
	stack, err := b.di.ComposeStackGet(ctx, id)
	if err != nil || stack == nil {
		if stack == nil {
			return errors.New("stack not found")
		}
		return err
	}
	cli, err := b.clientForStack(ctx, stack)
	if err == nil {
		engine := compose.NewStandaloneEngine(cli)
		// Cleanup hook drops helper containers + secret volumes by label —
		// no Vault lookup needed, so stack removal still works when Vault
		// is unreachable.
		_ = engine.Remove(ctx, stack.Name, removeVolumes, b.sec.NewCleanupHook())
	}
	// Drop persisted bindings — the values live in Vault and are unaffected.
	_ = b.di.ComposeStackSecretBindingDeleteByStack(ctx, id)
	if err := b.di.ComposeStackDelete(ctx, id); err != nil {
		return err
	}
	b.eb.CreateStack(EventActionDelete, stack.HostID, stack.Name, user)
	return nil
}

func (b *composeStackBiz) clientForStack(ctx context.Context, stack *dao.ComposeStack) (*dockerclient.Client, error) {
	host, err := b.hb.Find(ctx, stack.HostID)
	if err != nil {
		return nil, err
	}
	if host == nil {
		return nil, errors.New("host not found")
	}
	return b.d.Hosts.GetClient(host.ID, host.Endpoint)
}

func split2(s, sep string) []string {
	for i := 0; i < len(s); i++ {
		if i+len(sep) <= len(s) && s[i:i+len(sep)] == sep {
			return []string{s[:i], s[i+len(sep):]}
		}
	}
	return []string{s, ""}
}

func containsIgnoreCase(haystack, needle string) bool {
	return indexFoldCase(haystack, needle) >= 0
}

func indexFoldCase(s, substr string) int {
	if substr == "" {
		return 0
	}
	lenS, lenSub := len(s), len(substr)
	for i := 0; i+lenSub <= lenS; i++ {
		match := true
		for j := 0; j < lenSub; j++ {
			if toLower(s[i+j]) != toLower(substr[j]) {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func toLower(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + 32
	}
	return b
}

// FindDetail returns a ComposeStackDetail for the given project on a host,
// merging persisted metadata (if any) with live discovery and optional YAML
// reconstruction for external stacks.
func (b *composeStackBiz) FindDetail(ctx context.Context, hostID, name string) (*ComposeStackDetail, error) {
	host, err := b.hb.Find(ctx, hostID)
	if err != nil {
		return nil, err
	}
	if host == nil {
		return nil, errors.New("host not found")
	}
	if host.Status != "connected" {
		return nil, errors.New("host is not connected")
	}
	cli, err := b.d.Hosts.GetClient(host.ID, host.Endpoint)
	if err != nil {
		return nil, err
	}
	engine := compose.NewStandaloneEngine(cli)

	pd, err := engine.GetProject(ctx, name)
	if err != nil {
		return nil, err
	}

	detail := &ComposeStackDetail{
		HostID:   host.ID,
		HostName: host.Name,
		Name:     name,
		Status:   pd.Status,
		Services: pd.Services,
		Networks: pd.Networks,
		Volumes:  pd.Volumes,
	}

	// Map container summaries to brief form.
	for _, c := range pd.Containers {
		cname := ""
		if len(c.Names) > 0 {
			cname = c.Names[0]
		}
		svc := c.Labels[compose.LabelService]
		brief := ComposeContainerBrief{
			ID:      c.ID,
			Name:    cname,
			Service: svc,
			Image:   c.Image,
			State:   c.State,
			Status:  c.Status,
			Created: formatTime(time.Unix(c.Created, 0)),
		}
		for _, p := range c.Ports {
			brief.Ports = append(brief.Ports, &ContainerPort{
				IP:          p.IP,
				PrivatePort: p.PrivatePort,
				PublicPort:  p.PublicPort,
				Type:        p.Type,
			})
		}
		detail.Containers = append(detail.Containers, brief)
	}

	// Overlay persisted record when present.
	persisted, err := b.di.ComposeStackGetByName(ctx, hostID, name)
	if err != nil {
		return nil, err
	}
	if persisted != nil {
		detail.ID = persisted.ID
		detail.Managed = true
		detail.Content = persisted.Content
		detail.UpdatedAt = formatTime(time.Time(persisted.UpdatedAt))
		return detail, nil
	}

	// External stack: best-effort YAML reconstruction.
	if yaml, rErr := engine.ReconstructCompose(ctx, name); rErr == nil {
		detail.Content = yaml
		detail.Reconstructed = true
	}
	return detail, nil
}

// Import promotes an external (discovered) stack to managed. If the content is
// empty the engine reconstructs a YAML from the running containers.
// When redeploy is true the imported stack is (re)deployed with the YAML,
// fully recreating its containers. When false the record is just persisted and
// the running containers remain untouched.
func (b *composeStackBiz) Import(ctx context.Context, stack *dao.ComposeStack, redeploy, pullImages bool, user web.User) (string, error) {
	if stack.HostID == "" || stack.Name == "" {
		return "", errors.New("hostId and name are required")
	}

	// Prevent duplicates.
	if existing, err := b.di.ComposeStackGetByName(ctx, stack.HostID, stack.Name); err != nil {
		return "", err
	} else if existing != nil {
		return "", errors.New("stack already managed")
	}

	if stack.Content == "" {
		host, err := b.hb.Find(ctx, stack.HostID)
		if err != nil {
			return "", err
		}
		if host == nil {
			return "", errors.New("host not found")
		}
		cli, err := b.d.Hosts.GetClient(host.ID, host.Endpoint)
		if err != nil {
			return "", err
		}
		engine := compose.NewStandaloneEngine(cli)
		yaml, err := engine.ReconstructCompose(ctx, stack.Name)
		if err != nil {
			return "", err
		}
		stack.Content = yaml
	}

	if redeploy {
		id, err := b.Deploy(ctx, stack, pullImages, user)
		if err == nil {
			b.eb.CreateStack(EventActionImport, stack.HostID, stack.Name, user)
		}
		return id, err
	}
	// no redeploy: just persist. Status reflects current live state.
	stack.Status = "active"
	id, err := b.Save(ctx, stack, user)
	if err == nil {
		b.eb.CreateStack(EventActionImport, stack.HostID, stack.Name, user)
	}
	return id, err
}

// StartExternal starts all containers of an unmanaged project on a host.
func (b *composeStackBiz) StartExternal(ctx context.Context, hostID, name string, user web.User) error {
	cli, err := b.hostClient(ctx, hostID)
	if err != nil {
		return err
	}
	engine := compose.NewStandaloneEngine(cli)
	if err := engine.Start(ctx, name); err != nil {
		return err
	}
	b.eb.CreateStack(EventActionStart, hostID, name, user)
	return nil
}

// StopExternal stops all running containers of an unmanaged project on a host.
func (b *composeStackBiz) StopExternal(ctx context.Context, hostID, name string, user web.User) error {
	cli, err := b.hostClient(ctx, hostID)
	if err != nil {
		return err
	}
	engine := compose.NewStandaloneEngine(cli)
	if err := engine.Stop(ctx, name); err != nil {
		return err
	}
	b.eb.CreateStack(EventActionShutdown, hostID, name, user)
	return nil
}

// RemoveExternal removes all containers of an unmanaged project on a host.
func (b *composeStackBiz) RemoveExternal(ctx context.Context, hostID, name string, removeVolumes bool, user web.User) error {
	cli, err := b.hostClient(ctx, hostID)
	if err != nil {
		return err
	}
	engine := compose.NewStandaloneEngine(cli)
	if err := engine.Remove(ctx, name, removeVolumes, b.sec.NewCleanupHook()); err != nil {
		return err
	}
	b.eb.CreateStack(EventActionDelete, hostID, name, user)
	return nil
}

func (b *composeStackBiz) hostClient(ctx context.Context, hostID string) (*dockerclient.Client, error) {
	host, err := b.hb.Find(ctx, hostID)
	if err != nil {
		return nil, err
	}
	if host == nil {
		return nil, errors.New("host not found")
	}
	return b.d.Hosts.GetClient(host.ID, host.Endpoint)
}

func init() {
	container.Put(NewComposeStack)
}
