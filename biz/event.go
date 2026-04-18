package biz

import (
	"context"
	"time"

	"github.com/cuigh/auxo/data"
	"github.com/cuigh/auxo/ext/times"
	"github.com/cuigh/auxo/log"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/dao"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type EventType string

const (
	EventTypeRegistry  EventType = "Registry"
	EventTypeNode      EventType = "Node"
	EventTypeNetwork   EventType = "Network"
	EventTypeService   EventType = "Service"
	EventTypeStack     EventType = "Stack"
	EventTypeConfig    EventType = "Config"
	EventTypeSecret    EventType = "Secret"
	EventTypeImage     EventType = "Image"
	EventTypeContainer EventType = "Container"
	EventTypeVolume    EventType = "Volume"
	EventTypeUser      EventType = "User"
	EventTypeRole      EventType = "Role"
	EventTypeChart     EventType = "Chart"
	EventTypeSetting   EventType = "Setting"
	EventTypeHost      EventType = "Host"
	EventTypeBackup      EventType = "Backup"
	EventTypeVaultSecret EventType = "VaultSecret"
	EventTypeSelfDeploy  EventType = "SelfDeploy"
)

type EventAction string

const (
	EventActionLogin      EventAction = "Login"
	EventActionCreate     EventAction = "Create"
	EventActionDelete     EventAction = "Delete"
	EventActionUpdate     EventAction = "Update"
	EventActionScale      EventAction = "Scale"
	EventActionRollback   EventAction = "Rollback"
	EventActionRestart    EventAction = "Restart"
	EventActionDisconnect EventAction = "Disconnect"
	EventActionDeploy     EventAction = "Deploy"
	EventActionShutdown   EventAction = "Shutdown"
	EventActionPrune      EventAction = "Prune"
	EventActionStart      EventAction = "Start"
	EventActionStop       EventAction = "Stop"
	EventActionKill       EventAction = "Kill"
	EventActionPause      EventAction = "Pause"
	EventActionUnpause    EventAction = "Unpause"
	EventActionRename     EventAction = "Rename"
	EventActionImport     EventAction = "Import"
	EventActionRestore    EventAction = "Restore"
	EventActionDownload   EventAction = "Download"
	EventActionMigrate    EventAction = "Migrate"
	EventActionCleanup    EventAction = "Cleanup"
	// Self-deploy lifecycle actions. Emitted by SelfDeployBiz.TriggerDeploy
	// (Start at the moment the sidekick is spawned) and by the sidekick
	// itself (Success/Failure) — the latter wires in during Phase 4.
	EventActionSelfDeployStart   EventAction = "Start"
	EventActionSelfDeploySuccess EventAction = "Success"
	EventActionSelfDeployFailure EventAction = "Failure"
)

type EventBiz interface {
	Search(ctx context.Context, args *dao.EventSearchArgs) (events []*dao.Event, total int, err error)
	Prune(ctx context.Context, days int32) (err error)
	CreateRegistry(action EventAction, id, name string, user web.User)
	CreateNode(action EventAction, id, name string, user web.User)
	CreateNetwork(action EventAction, node, id, name string, user web.User)
	CreateService(action EventAction, name string, user web.User)
	CreateConfig(action EventAction, id, name string, user web.User)
	CreateSecret(action EventAction, id, name string, user web.User)
	CreateStack(action EventAction, node, name string, user web.User)
	CreateImage(action EventAction, node, id string, user web.User)
	CreateContainer(action EventAction, node, id, name string, user web.User)
	CreateVolume(action EventAction, node, name string, user web.User)
	CreateUser(action EventAction, id, name string, user web.User)
	CreateRole(action EventAction, id, name string, user web.User)
	CreateChart(action EventAction, id, title string, user web.User)
	CreateSetting(action EventAction, user web.User)
	CreateHost(action EventAction, id, name string, user web.User)
	CreateBackup(action EventAction, id, name string, user web.User)
	CreateVaultSecret(action EventAction, id, name string, user web.User)
	CreateSelfDeploy(action EventAction, jobID, imageTag string, user web.User)
}

func NewEvent(d dao.Interface) EventBiz {
	return &eventBiz{d: d}
}

type eventBiz struct {
	d dao.Interface
}

func (b *eventBiz) Search(ctx context.Context, args *dao.EventSearchArgs) (events []*dao.Event, total int, err error) {
	return b.d.EventSearch(ctx, args)
}

func (b *eventBiz) Prune(ctx context.Context, days int32) (err error) {
	return b.d.EventPrune(ctx, time.Now().Add(-times.Days(days)))
}

func (b *eventBiz) create(et EventType, ea EventAction, args data.Map, user web.User) {
	var uid, uname string
	if user != nil {
		uid = user.ID()
		uname = user.Name()
	}
	event := &dao.Event{
		ID:       primitive.NewObjectID(),
		Type:     string(et),
		Action:   string(ea),
		Args:     args,
		UserID:   uid,
		Username: uname,
		Time:     now(),
	}
	err := b.d.EventCreate(context.TODO(), event)
	if err != nil {
		log.Get("event").Errorf("failed to create event `%+v`: %s", event, err)
	}
}

func (b *eventBiz) CreateRegistry(action EventAction, id, name string, user web.User) {
	args := data.Map{"id": id, "name": name}
	b.create(EventTypeRegistry, action, args, user)
}

func (b *eventBiz) CreateService(action EventAction, name string, user web.User) {
	args := data.Map{"name": name}
	b.create(EventTypeService, action, args, user)
}

func (b *eventBiz) CreateNetwork(action EventAction, node, id, name string, user web.User) {
	args := data.Map{"id": id, "name": name}
	if node != "" {
		args["node"] = node
	}
	b.create(EventTypeNetwork, action, args, user)
}

func (b *eventBiz) CreateNode(action EventAction, id, name string, user web.User) {
	args := data.Map{"id": id, "name": name}
	b.create(EventTypeNode, action, args, user)
}

func (b *eventBiz) CreateImage(action EventAction, node, id string, user web.User) {
	args := data.Map{"node": node}
	if id != "" {
		args["id"] = id
	}
	b.create(EventTypeImage, action, args, user)
}

func (b *eventBiz) CreateContainer(action EventAction, node, id, name string, user web.User) {
	args := data.Map{"node": node}
	if id != "" {
		args["id"] = id
	}
	if name != "" {
		args["name"] = name
	}
	b.create(EventTypeContainer, action, args, user)
}

func (b *eventBiz) CreateVolume(action EventAction, node, name string, user web.User) {
	args := data.Map{"node": node}
	if name != "" {
		args["name"] = name
	}
	b.create(EventTypeVolume, action, args, user)
}

func (b *eventBiz) CreateStack(action EventAction, node, name string, user web.User) {
	args := data.Map{"name": name}
	if node != "" {
		args["node"] = node
	}
	b.create(EventTypeStack, action, args, user)
}

func (b *eventBiz) CreateSecret(action EventAction, id, name string, user web.User) {
	args := data.Map{"id": id, "name": name}
	b.create(EventTypeSecret, action, args, user)
}

func (b *eventBiz) CreateConfig(action EventAction, id, name string, user web.User) {
	args := data.Map{"id": id, "name": name}
	b.create(EventTypeConfig, action, args, user)
}

func (b *eventBiz) CreateRole(action EventAction, id, name string, user web.User) {
	args := data.Map{"id": id, "name": name}
	b.create(EventTypeRole, action, args, user)
}

func (b *eventBiz) CreateSetting(action EventAction, user web.User) {
	b.create(EventTypeSetting, action, nil, user)
}

func (b *eventBiz) CreateUser(action EventAction, id, name string, user web.User) {
	args := data.Map{"id": id, "name": name}
	b.create(EventTypeUser, action, args, user)
}

func (b *eventBiz) CreateChart(action EventAction, id, title string, user web.User) {
	args := data.Map{"id": id, "name": title}
	b.create(EventTypeChart, action, args, user)
}

func (b *eventBiz) CreateHost(action EventAction, id, name string, user web.User) {
	args := data.Map{"id": id, "name": name}
	b.create(EventTypeHost, action, args, user)
}

func (b *eventBiz) CreateBackup(action EventAction, id, name string, user web.User) {
	args := data.Map{"id": id, "name": name}
	b.create(EventTypeBackup, action, args, user)
}

func (b *eventBiz) CreateVaultSecret(action EventAction, id, name string, user web.User) {
	args := data.Map{"id": id, "name": name}
	b.create(EventTypeVaultSecret, action, args, user)
}

// CreateSelfDeploy records a self-deploy lifecycle transition. `jobID` is
// the uuid that ties the three events (start → success|failure) together
// in the audit trail; `imageTag` is the target image reference so
// operators can audit what was attempted without joining on external
// state. Either may be empty (Failure before PrepareJob fully resolved
// the target, for instance) — the args map elides missing keys.
//
// Emission model (Phase 7 hardening):
//   - `Start` is emitted by the main Swirl's TriggerDeploy right after
//     the sidekick container has been spawned successfully (the sidekick
//     may still fail to run, but the deploy *intent* has been recorded).
//   - `Success` / `Failure` are emitted by the main Swirl's Status
//     handler when it polls state.json and observes a terminal phase
//     with EventPublished=false. The sidekick has no DB access so it
//     cannot emit these directly; main-side publishing with an
//     idempotency flag on state.json avoids any event duplication
//     regardless of how many times Status is polled.
//   - The `user` argument for `Success` / `Failure` events is nil
//     (the sidekick runs without a logged-in session); this is
//     supported by eventBiz.create which nil-checks web.User.
func (b *eventBiz) CreateSelfDeploy(action EventAction, jobID, imageTag string, user web.User) {
	args := data.Map{}
	if jobID != "" {
		args["jobId"] = jobID
	}
	if imageTag != "" {
		args["imageTag"] = imageTag
	}
	b.create(EventTypeSelfDeploy, action, args, user)
}
