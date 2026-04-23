package api

import (
	"github.com/cuigh/auxo/data"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/biz"
	"github.com/cuigh/swirl/dao"
	"github.com/cuigh/swirl/misc"
)

// ComposeStackHandler exposes Portainer-style compose stack endpoints for standalone mode.
type ComposeStackHandler struct {
	Search     web.HandlerFunc `path:"/search" auth:"stack.view" desc:"search compose stacks"`
	Find       web.HandlerFunc `path:"/find" auth:"stack.view" desc:"find compose stack by id"`
	FindDetail web.HandlerFunc `path:"/find-detail" auth:"stack.view" desc:"find detail by hostId+name"`
	Save       web.HandlerFunc `path:"/save" method:"post" auth:"stack.edit" desc:"save compose stack without deploying"`
	Deploy     web.HandlerFunc `path:"/deploy" method:"post" auth:"stack.deploy" desc:"deploy compose stack"`
	// DeployByID redeploys an existing stack using the persisted YAML
	// without going through the editor payload. Used by the stack list's
	// Deploy button and by Start's fallback path.
	DeployByID web.HandlerFunc `path:"/deploy-by-id" method:"post" auth:"stack.deploy" desc:"redeploy a persisted stack"`
	Import     web.HandlerFunc `path:"/import" method:"post" auth:"stack.edit" desc:"import an external stack"`
	Start      web.HandlerFunc `path:"/start" method:"post" auth:"stack.deploy" desc:"start compose stack"`
	Stop       web.HandlerFunc `path:"/stop" method:"post" auth:"stack.shutdown" desc:"stop compose stack"`
	Remove     web.HandlerFunc `path:"/remove" method:"post" auth:"stack.delete" desc:"remove compose stack"`
	Migrate    web.HandlerFunc `path:"/migrate" method:"post" auth:"stack.edit" desc:"migrate stack to another host"`
	// HostAddons feeds the compose editor wizard tabs with runtime
	// configuration of add-on containers (Traefik entrypoints, Sablier
	// URL, Watchtower schedule, backup env) detected on the target host.
	// Auth: stack.view — the payload is public container metadata, no
	// secret values, so we reuse the same permission the editor itself
	// needs to read the stack.
	HostAddons web.HandlerFunc `path:"/host-addons" auth:"stack.view" desc:"discover add-on runtime config on a host"`

	// Version history endpoints — list/get are read-only (stack.view),
	// restore mutates the stack record (stack.edit). The list payload
	// strips Content/EnvFile bodies; clients fetch them via VersionGet
	// when rendering a diff.
	Versions       web.HandlerFunc `path:"/versions" auth:"stack.view" desc:"list content-history versions of a stack"`
	VersionGet     web.HandlerFunc `path:"/version-get" auth:"stack.view" desc:"fetch a single version with body"`
	VersionRestore web.HandlerFunc `path:"/version-restore" method:"post" auth:"stack.edit" desc:"restore a prior version of a stack"`

	// ParseAddons is the authoritative reverse-parser for the addon
	// wizard tabs: given a compose YAML it returns the AddonsConfig
	// rebuilt from `# swirl-managed` markers. Called by the editor at
	// load time + after a version restore so the tabs start in sync
	// with the persisted content. POST to keep the body out of the URL.
	ParseAddons web.HandlerFunc `path:"/parse-addons" method:"post" auth:"stack.view" desc:"reverse-parse addon wizard state from compose YAML"`
	// RegistryCachePreview runs the deploy-time image rewriter in
	// report-only mode. Given an authored compose YAML + target host,
	// returns the list of RewriteAction the deployment would emit
	// without touching any state. Used by the Stack editor to show
	// the Registry Cache preview table. Auth: stack.view — the only
	// information leaked is the UpstreamMappings table which is
	// already visible on the Settings page to anyone who can reach
	// this editor.
	RegistryCachePreview web.HandlerFunc `path:"/registry-cache-preview" method:"post" auth:"stack.view" desc:"preview deploy-time image rewrites for a compose stack"`
}

// NewComposeStack is registered in api.init.
func NewComposeStack(b biz.ComposeStackBiz, hb biz.HostBiz, ad biz.AddonDiscoveryBiz) *ComposeStackHandler {
	return &ComposeStackHandler{
		Search:     composeStackSearch(b),
		Find:       composeStackFind(b),
		FindDetail: composeStackFindDetail(b),
		Save:       composeStackSave(b),
		Deploy:     composeStackDeploy(b),
		DeployByID: composeStackDeployByID(b),
		Import:     composeStackImport(b),
		Start:      composeStackStart(b),
		Stop:       composeStackStop(b),
		Remove:         composeStackRemove(b),
		Migrate:        composeStackMigrate(b),
		HostAddons:     composeStackHostAddons(ad),
		Versions:       composeStackVersions(b),
		VersionGet:     composeStackVersionGet(b),
		VersionRestore: composeStackVersionRestore(b),
		ParseAddons:    composeStackParseAddons(b),
		RegistryCachePreview: composeStackRegistryCachePreview(hb),
	}
}

func composeStackSearch(b biz.ComposeStackBiz) web.HandlerFunc {
	return func(c web.Context) error {
		args := &dao.ComposeStackSearchArgs{}
		if err := c.Bind(args); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		items, total, err := b.Search(ctx, args)
		if err != nil {
			return err
		}
		return success(c, data.Map{"items": items, "total": total})
	}
}

func composeStackFind(b biz.ComposeStackBiz) web.HandlerFunc {
	return func(c web.Context) error {
		id := c.Query("id")
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		stack, err := b.Find(ctx, id)
		if err != nil {
			return err
		}
		return success(c, stack)
	}
}

func composeStackSave(b biz.ComposeStackBiz) web.HandlerFunc {
	type Args struct {
		dao.ComposeStack
		// AddonsConfig carries the wizard state for Traefik/Sablier/
		// Watchtower/Backup/Resources tabs. When present, the biz layer
		// mutates Content to inject the corresponding labels before
		// persisting — the DB stores the final, label-bearing YAML.
		AddonsConfig *biz.AddonsConfig `json:"addonsConfig,omitempty"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args, true); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		id, err := b.SaveWithAddons(ctx, &args.ComposeStack, args.AddonsConfig, c.User())
		if err != nil {
			return err
		}
		return success(c, data.Map{"id": id})
	}
}

func composeStackDeploy(b biz.ComposeStackBiz) web.HandlerFunc {
	type Args struct {
		dao.ComposeStack
		PullImages   bool              `json:"pullImages"`
		AddonsConfig *biz.AddonsConfig `json:"addonsConfig,omitempty"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args, true); err != nil {
			return err
		}
		// deploy may take longer than defaultTimeout due to image pulls
		ctx, cancel := misc.Context(5 * defaultTimeout)
		defer cancel()
		id, err := b.DeployWithAddons(ctx, &args.ComposeStack, args.AddonsConfig, args.PullImages, c.User())
		if err != nil {
			return err
		}
		return success(c, data.Map{"id": id})
	}
}

func composeStackDeployByID(b biz.ComposeStackBiz) web.HandlerFunc {
	type Args struct {
		ID         string `json:"id"`
		PullImages bool   `json:"pullImages"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args, true); err != nil {
			return err
		}
		// Same envelope as /deploy — the async engine run survives past
		// the HTTP response, but we still give the caller a generous
		// deadline for the synchronous self-protection check + image
		// pulls it kicks off.
		ctx, cancel := misc.Context(5 * defaultTimeout)
		defer cancel()
		id, err := b.DeployByID(ctx, args.ID, args.PullImages, c.User())
		if err != nil {
			return err
		}
		return success(c, data.Map{"id": id})
	}
}

func composeStackStart(b biz.ComposeStackBiz) web.HandlerFunc {
	type Args struct {
		ID     string `json:"id"`
		HostID string `json:"hostId"`
		Name   string `json:"name"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		if args.ID != "" {
			return ajax(c, b.Start(ctx, args.ID, c.User()))
		}
		return ajax(c, b.StartExternal(ctx, args.HostID, args.Name, c.User()))
	}
}

func composeStackStop(b biz.ComposeStackBiz) web.HandlerFunc {
	type Args struct {
		ID     string `json:"id"`
		HostID string `json:"hostId"`
		Name   string `json:"name"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		if args.ID != "" {
			return ajax(c, b.Stop(ctx, args.ID, c.User()))
		}
		return ajax(c, b.StopExternal(ctx, args.HostID, args.Name, c.User()))
	}
}

func composeStackRemove(b biz.ComposeStackBiz) web.HandlerFunc {
	type Args struct {
		ID            string `json:"id"`
		HostID        string `json:"hostId"`
		Name          string `json:"name"`
		RemoveVolumes bool   `json:"removeVolumes"`
		// Force overrides the "volumes contain data" safety check. The UI
		// obtains it by showing a second confirmation dialog with the
		// volume list returned by the unforced attempt.
		Force bool `json:"force"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		var err error
		if args.ID != "" {
			err = b.Remove(ctx, args.ID, args.RemoveVolumes, args.Force, c.User())
		} else {
			err = b.RemoveExternal(ctx, args.HostID, args.Name, args.RemoveVolumes, args.Force, c.User())
		}
		// Structured error: surface the list of non-empty volumes so the
		// UI can render a second confirmation with exact names.
		if vcd, ok := err.(*biz.VolumesContainDataError); ok {
			return success(c, data.Map{
				"volumesContainData": true,
				"volumes":            vcd.Volumes,
			})
		}
		return ajax(c, err)
	}
}

func composeStackFindDetail(b biz.ComposeStackBiz) web.HandlerFunc {
	return func(c web.Context) error {
		hostID := c.Query("hostId")
		name := c.Query("name")
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		detail, err := b.FindDetail(ctx, hostID, name)
		if err != nil {
			return err
		}
		return success(c, detail)
	}
}

func composeStackMigrate(b biz.ComposeStackBiz) web.HandlerFunc {
	type Args struct {
		ID           string `json:"id"`
		TargetHostID string `json:"targetHostId"`
		Redeploy     bool   `json:"redeploy"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args, true); err != nil {
			return err
		}
		// Migration may redeploy on the target host, which can include
		// image pulls — reuse the deploy timeout envelope.
		ctx, cancel := misc.Context(5 * defaultTimeout)
		defer cancel()
		return ajax(c, b.Migrate(ctx, args.ID, args.TargetHostID, args.Redeploy, c.User()))
	}
}

func composeStackVersions(b biz.ComposeStackBiz) web.HandlerFunc {
	return func(c web.Context) error {
		stackID := c.Query("stackId")
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		items, err := b.ListVersions(ctx, stackID)
		if err != nil {
			return err
		}
		return success(c, data.Map{"items": items})
	}
}

func composeStackVersionGet(b biz.ComposeStackBiz) web.HandlerFunc {
	return func(c web.Context) error {
		versionID := c.Query("id")
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		v, err := b.GetVersion(ctx, versionID)
		if err != nil {
			return err
		}
		return success(c, v)
	}
}

func composeStackVersionRestore(b biz.ComposeStackBiz) web.HandlerFunc {
	type Args struct {
		StackID   string `json:"stackId"`
		VersionID string `json:"versionId"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args, true); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		return ajax(c, b.RestoreVersion(ctx, args.StackID, args.VersionID, c.User()))
	}
}

func composeStackParseAddons(b biz.ComposeStackBiz) web.HandlerFunc {
	type Args struct {
		Content string `json:"content"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args, true); err != nil {
			return err
		}
		cfg, err := b.ParseAddons(args.Content)
		if err != nil {
			return err
		}
		return success(c, cfg)
	}
}

// composeStackRegistryCachePreview runs the deploy-time image rewriter
// in report-only mode against an authored compose YAML + target host,
// returning the RewriteAction list without mutating anything. The UI
// calls this whenever the operator flips into the Registry Cache tab
// of the Stack editor, and again after YAML edits.
//
// Response shape:
//
//	{
//	  "mirrorEnabled": bool,
//	  "effectivelyDisabled": bool,   // true when the global + per-host
//	                                 // decision results in a no-op
//	  "actions": [RewriteAction]
//	}
func composeStackRegistryCachePreview(hb biz.HostBiz) web.HandlerFunc {
	type Args struct {
		HostID               string `json:"hostId"`
		Content              string `json:"content"`
		DisableRegistryCache bool   `json:"disableRegistryCache"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args, true); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		var hostExtract *biz.AddonConfigExtract
		if args.HostID != "" {
			if ext, err := hb.GetAddonConfigExtract(ctx, args.HostID); err == nil {
				hostExtract = ext
			}
		}

		// Build a throw-away stack carrying the per-request opt-out so
		// BuildRewriteInput resolves the same scope the Deploy path
		// would. We do NOT round-trip through the DB here.
		stack := &dao.ComposeStack{
			HostID:               args.HostID,
			DisableRegistryCache: args.DisableRegistryCache,
		}
		in := biz.BuildRewriteInput(stack, hostExtract, biz.LiveSettingsSnapshot())
		_, actions, _ := biz.RewriteImages(args.Content, in)

		return success(c, data.Map{
			"mirrorEnabled":       biz.LiveRegistryCacheParams() != nil,
			"effectivelyDisabled": !biz.WillRewrite(in),
			"actions":             actions,
		})
	}
}

func composeStackHostAddons(ad biz.AddonDiscoveryBiz) web.HandlerFunc {
	return func(c web.Context) error {
		hostID := c.Query("hostId")
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		addons, err := ad.Discover(ctx, hostID)
		if err != nil {
			return err
		}
		return success(c, addons)
	}
}

func composeStackImport(b biz.ComposeStackBiz) web.HandlerFunc {
	type Args struct {
		dao.ComposeStack
		Redeploy   bool `json:"redeploy"`
		PullImages bool `json:"pullImages"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args, true); err != nil {
			return err
		}
		ctx, cancel := misc.Context(5 * defaultTimeout)
		defer cancel()
		id, err := b.Import(ctx, &args.ComposeStack, args.Redeploy, args.PullImages, c.User())
		if err != nil {
			return err
		}
		return success(c, data.Map{"id": id})
	}
}
