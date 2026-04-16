package biz

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cuigh/auxo/app"
	"github.com/cuigh/auxo/data"
	"github.com/cuigh/auxo/log"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/dao"
)

const (
	BackupSourceManual  = "manual"
	BackupSourceDaily   = "daily"
	BackupSourceWeekly  = "weekly"
	BackupSourceMonthly = "monthly"

	BackupDirEnv       = "SWIRL_BACKUP_DIR"
	backupDirDefault   = "/data/swirl/backups"
	backupFileSuffix   = ".swb"
	backupDocVersion   = "1.0"
	backupExportFormat = "2006-01-02T15-04-05Z"

	// Component keys selectable on restore.
	ComponentSettings      = "settings"
	ComponentRoles         = "roles"
	ComponentUsers         = "users"
	ComponentRegistries    = "registries"
	ComponentStacks        = "stacks"
	ComponentComposeStacks = "composeStacks"
	ComponentHosts         = "hosts"
	ComponentCharts        = "charts"
	ComponentEvents        = "events"
)

// AllBackupComponents lists every restorable component. The order matters: it
// is used for dependency-aware insert (roles before users, etc).
var AllBackupComponents = []string{
	ComponentSettings,
	ComponentRoles,
	ComponentUsers,
	ComponentRegistries,
	ComponentStacks,
	ComponentComposeStacks,
	ComponentHosts,
	ComponentCharts,
	ComponentEvents,
}

// BackupDocument is the decoded plaintext of a backup archive.
type BackupDocument struct {
	Version       string              `json:"version"`
	ExportedAt    time.Time           `json:"exportedAt"`
	SwirlVersion  string              `json:"swirlVersion"`
	Settings      []*dao.Setting      `json:"settings,omitempty"`
	Roles         []*dao.Role         `json:"roles,omitempty"`
	Users         []*userExport       `json:"users,omitempty"`
	Registries    []*dao.Registry     `json:"registries,omitempty"`
	Stacks        []*dao.Stack        `json:"stacks,omitempty"`
	ComposeStacks []*dao.ComposeStack `json:"composeStacks,omitempty"`
	Hosts         []*hostExport       `json:"hosts,omitempty"`
	Charts        []*dao.Chart        `json:"charts,omitempty"`
	Events        []*dao.Event        `json:"events,omitempty"`
}

// userExport is a JSON-only projection of dao.User that includes the
// password/salt fields (which the regular dao.User hides via `json:"-"`).
type userExport struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	LoginName string       `json:"loginName"`
	Password  string       `json:"password"`
	Salt      string       `json:"salt"`
	Email     string       `json:"email"`
	Admin     bool         `json:"admin"`
	Type      string       `json:"type"`
	Status    int32        `json:"status"`
	Roles     []string     `json:"roles,omitempty"`
	Tokens    data.Options `json:"tokens,omitempty"`
	CreatedAt dao.Time     `json:"createdAt"`
	UpdatedAt dao.Time     `json:"updatedAt"`
	CreatedBy dao.Operator `json:"createdBy"`
	UpdatedBy dao.Operator `json:"updatedBy"`
}

// hostExport is a JSON-only projection of dao.Host that includes the
// TLSKey / SSHKey fields (hidden by `json:"-"` on the regular struct).
type hostExport struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	Endpoint   string       `json:"endpoint"`
	AuthMethod string       `json:"authMethod"`
	TLSCACert  string       `json:"tlsCaCert,omitempty"`
	TLSCert    string       `json:"tlsCert,omitempty"`
	TLSKey     string       `json:"tlsKey,omitempty"`
	SSHUser    string       `json:"sshUser,omitempty"`
	SSHKey     string       `json:"sshKey,omitempty"`
	Status     string       `json:"status"`
	Error      string       `json:"error,omitempty"`
	EngineVer  string       `json:"engineVersion,omitempty"`
	OS         string       `json:"os,omitempty"`
	Arch       string       `json:"arch,omitempty"`
	CPUs       int          `json:"cpus,omitempty"`
	Memory     int64        `json:"memory,omitempty"`
	CreatedAt  dao.Time     `json:"createdAt"`
	UpdatedAt  dao.Time     `json:"updatedAt"`
	CreatedBy  dao.Operator `json:"createdBy"`
	UpdatedBy  dao.Operator `json:"updatedBy"`
}

// BackupManifest is the lightweight header returned by preview — it only
// contains counts and timestamps so the UI can show stats before a commit.
type BackupManifest struct {
	Version      string         `json:"version"`
	ExportedAt   time.Time      `json:"exportedAt"`
	SwirlVersion string         `json:"swirlVersion"`
	Stats        map[string]int `json:"stats"`
}

type BackupBiz interface {
	KeyConfigured() bool
	List(ctx context.Context) ([]*dao.Backup, error)
	Find(ctx context.Context, id string) (*dao.Backup, error)
	Manifest(ctx context.Context, id string) (*BackupManifest, error)
	Create(ctx context.Context, source string, user web.User) (*dao.Backup, error)
	Delete(ctx context.Context, id string, user web.User) error
	Open(ctx context.Context, id string, mode, password string, user web.User) (filename string, data []byte, err error)
	Restore(ctx context.Context, id string, components []string, user web.User) (map[string]int, error)
	PreviewUpload(ctx context.Context, archive []byte, password string) (*BackupManifest, error)
	RestoreUpload(ctx context.Context, archive []byte, password string, components []string, user web.User) (map[string]int, error)

	Schedules(ctx context.Context) ([]*dao.BackupSchedule, error)
	SaveSchedule(ctx context.Context, schedule *dao.BackupSchedule, user web.User) error
	DeleteSchedule(ctx context.Context, id string, user web.User) error

	// RunScheduled executes a scheduled backup and applies retention.
	// Called by the backup package scheduler.
	RunScheduled(ctx context.Context, schedule *dao.BackupSchedule) error
	// ApplyRetention removes old backups beyond max, keeping the newest.
	// Returns the number of archives actually deleted.
	ApplyRetention(ctx context.Context, source string, max int) (int, error)
}

func NewBackup(d dao.Interface, eb EventBiz) BackupBiz {
	return &backupBiz{d: d, eb: eb, logger: log.Get("backup")}
}

type backupBiz struct {
	d      dao.Interface
	eb     EventBiz
	logger log.Logger
}

// --- public surface -------------------------------------------------------

func (b *backupBiz) KeyConfigured() bool {
	return backupKeyConfigured()
}

func (b *backupBiz) List(ctx context.Context) ([]*dao.Backup, error) {
	return b.d.BackupGetAll(ctx)
}

func (b *backupBiz) Find(ctx context.Context, id string) (*dao.Backup, error) {
	return b.d.BackupGet(ctx, id)
}

func (b *backupBiz) Manifest(ctx context.Context, id string) (*BackupManifest, error) {
	rec, err := b.d.BackupGet(ctx, id)
	if err != nil || rec == nil {
		return nil, err
	}
	stats := rec.Stats
	if stats == nil {
		stats = map[string]int{}
	}
	return &BackupManifest{
		Version:    backupDocVersion,
		ExportedAt: rec.CreatedAt,
		Stats:      stats,
	}, nil
}

func (b *backupBiz) Create(ctx context.Context, source string, user web.User) (*dao.Backup, error) {
	if !backupKeyConfigured() {
		return nil, errMissingKey
	}
	if source == "" {
		source = BackupSourceManual
	}

	doc, err := b.exportDocument(ctx)
	if err != nil {
		return nil, fmt.Errorf("export failed: %w", err)
	}

	plaintext, err := marshalGzip(doc)
	if err != nil {
		return nil, err
	}

	archive, err := encryptAtRest(plaintext)
	if err != nil {
		return nil, err
	}

	dir := backupDir()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("cannot create backup dir: %w", err)
	}

	id := createId()
	path := filepath.Join(dir, id+backupFileSuffix)
	if err := writeFileAtomic(path, archive); err != nil {
		return nil, err
	}

	sum := sha256.Sum256(plaintext)
	record := &dao.Backup{
		ID:        id,
		Name:      fmt.Sprintf("%s-%s", source, time.Now().UTC().Format(backupExportFormat)),
		Source:    source,
		Size:      int64(len(archive)),
		Checksum:  hex.EncodeToString(sum[:]),
		Path:      path,
		Includes:  AllBackupComponents,
		Stats:     statsFromDocument(doc),
		CreatedAt: time.Now(),
	}
	if user != nil {
		record.CreatedBy = dao.Operator{ID: user.ID(), Name: user.Name()}
	}

	if err := b.d.BackupCreate(ctx, record); err != nil {
		_ = os.Remove(path)
		return nil, err
	}

	if user != nil {
		b.eb.CreateBackup(EventActionCreate, record.ID, record.Name, user)
	}
	return record, nil
}

func (b *backupBiz) Delete(ctx context.Context, id string, user web.User) error {
	rec, err := b.d.BackupGet(ctx, id)
	if err != nil || rec == nil {
		if rec == nil && err == nil {
			return errors.New("backup not found")
		}
		return err
	}
	if err := b.d.BackupDelete(ctx, id); err != nil {
		return err
	}
	if rec.Path != "" {
		if rmErr := os.Remove(rec.Path); rmErr != nil && !os.IsNotExist(rmErr) {
			b.logger.Warnf("failed to remove backup file %s: %v", rec.Path, rmErr)
		}
	}
	if user != nil {
		b.eb.CreateBackup(EventActionDelete, rec.ID, rec.Name, user)
	}
	return nil
}

func (b *backupBiz) Open(ctx context.Context, id, mode, password string, user web.User) (string, []byte, error) {
	rec, err := b.d.BackupGet(ctx, id)
	if err != nil {
		return "", nil, err
	}
	if rec == nil {
		return "", nil, errors.New("backup not found")
	}
	raw, err := os.ReadFile(rec.Path)
	if err != nil {
		return "", nil, fmt.Errorf("cannot read backup archive: %w", err)
	}

	switch mode {
	case "", "raw":
		if user != nil {
			b.eb.CreateBackup(EventActionDownload, rec.ID, rec.Name, user)
		}
		return rec.Name + backupFileSuffix, raw, nil
	case "portable":
		plaintext, err := decryptAtRest(raw)
		if err != nil {
			return "", nil, err
		}
		out, err := encryptPortable(plaintext, password)
		if err != nil {
			return "", nil, err
		}
		if user != nil {
			b.eb.CreateBackup(EventActionDownload, rec.ID, rec.Name, user)
		}
		return rec.Name + ".enc", out, nil
	default:
		return "", nil, fmt.Errorf("unknown download mode: %s", mode)
	}
}

func (b *backupBiz) Restore(ctx context.Context, id string, components []string, user web.User) (map[string]int, error) {
	rec, err := b.d.BackupGet(ctx, id)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, errors.New("backup not found")
	}
	raw, err := os.ReadFile(rec.Path)
	if err != nil {
		return nil, fmt.Errorf("cannot read backup archive: %w", err)
	}
	plaintext, err := decryptAtRest(raw)
	if err != nil {
		return nil, err
	}
	doc, err := unmarshalGzip(plaintext)
	if err != nil {
		return nil, err
	}
	counts, err := b.importDocument(ctx, doc, components)
	if err != nil {
		return counts, err
	}
	if user != nil {
		b.eb.CreateBackup(EventActionRestore, rec.ID, rec.Name, user)
	}
	return counts, nil
}

func (b *backupBiz) PreviewUpload(ctx context.Context, archive []byte, password string) (*BackupManifest, error) {
	plaintext, err := decryptAny(archive, password)
	if err != nil {
		return nil, err
	}
	doc, err := unmarshalGzip(plaintext)
	if err != nil {
		return nil, err
	}
	return &BackupManifest{
		Version:      doc.Version,
		ExportedAt:   doc.ExportedAt,
		SwirlVersion: doc.SwirlVersion,
		Stats:        statsFromDocument(doc),
	}, nil
}

func (b *backupBiz) RestoreUpload(ctx context.Context, archive []byte, password string, components []string, user web.User) (map[string]int, error) {
	plaintext, err := decryptAny(archive, password)
	if err != nil {
		return nil, err
	}
	doc, err := unmarshalGzip(plaintext)
	if err != nil {
		return nil, err
	}
	counts, err := b.importDocument(ctx, doc, components)
	if err != nil {
		return counts, err
	}
	if user != nil {
		name := "uploaded-" + time.Now().UTC().Format(backupExportFormat)
		b.eb.CreateBackup(EventActionRestore, "", name, user)
	}
	return counts, nil
}

// --- schedules ------------------------------------------------------------

func (b *backupBiz) Schedules(ctx context.Context) ([]*dao.BackupSchedule, error) {
	return b.d.BackupScheduleGetAll(ctx)
}

func (b *backupBiz) SaveSchedule(ctx context.Context, schedule *dao.BackupSchedule, user web.User) error {
	if err := validateSchedule(schedule); err != nil {
		return err
	}
	existing, err := b.d.BackupScheduleGet(ctx, schedule.ID)
	if err != nil {
		return err
	}
	now := time.Now()
	schedule.UpdatedAt = now
	if existing == nil {
		schedule.CreatedAt = now
	} else {
		schedule.CreatedAt = existing.CreatedAt
		if schedule.LastRunAt == nil {
			schedule.LastRunAt = existing.LastRunAt
		}
	}
	if err := b.d.BackupScheduleUpsert(ctx, schedule); err != nil {
		return err
	}
	if user != nil {
		b.eb.CreateBackup(EventActionUpdate, schedule.ID, "schedule:"+schedule.ID, user)
	}
	return nil
}

func (b *backupBiz) DeleteSchedule(ctx context.Context, id string, user web.User) error {
	if err := b.d.BackupScheduleDelete(ctx, id); err != nil {
		return err
	}
	if user != nil {
		b.eb.CreateBackup(EventActionDelete, id, "schedule:"+id, user)
	}
	return nil
}

func (b *backupBiz) RunScheduled(ctx context.Context, schedule *dao.BackupSchedule) error {
	if _, err := b.Create(ctx, schedule.ID, nil); err != nil {
		return err
	}
	if err := b.d.BackupScheduleTouch(ctx, schedule.ID, time.Now()); err != nil {
		b.logger.Warnf("backup schedule %s: cannot update lastRunAt: %v", schedule.ID, err)
	}
	if schedule.Retention > 0 {
		if _, err := b.ApplyRetention(ctx, schedule.ID, schedule.Retention); err != nil {
			b.logger.Warnf("backup retention for %s failed: %v", schedule.ID, err)
		}
	}
	return nil
}

func (b *backupBiz) ApplyRetention(ctx context.Context, source string, max int) (int, error) {
	if max <= 0 {
		return 0, nil
	}
	records, err := b.d.BackupGetBySource(ctx, source)
	if err != nil {
		return 0, err
	}
	if len(records) <= max {
		return 0, nil
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].CreatedAt.After(records[j].CreatedAt)
	})
	var deleted int
	for _, r := range records[max:] {
		if err := b.Delete(ctx, r.ID, nil); err != nil {
			b.logger.Warnf("retention: cannot delete backup %s: %v", r.ID, err)
			continue
		}
		deleted++
	}
	return deleted, nil
}

// --- export ---------------------------------------------------------------

func (b *backupBiz) exportDocument(ctx context.Context) (*BackupDocument, error) {
	doc := &BackupDocument{
		Version:      backupDocVersion,
		ExportedAt:   time.Now().UTC(),
		SwirlVersion: app.Version,
	}

	settings, err := b.d.SettingGetAll(ctx)
	if err != nil {
		return nil, err
	}
	doc.Settings = settings

	roles, err := b.d.RoleSearch(ctx, "")
	if err != nil {
		return nil, err
	}
	doc.Roles = roles

	users, _, err := b.d.UserSearch(ctx, &dao.UserSearchArgs{Status: -1, PageIndex: 1, PageSize: 10000})
	if err != nil {
		return nil, err
	}
	doc.Users = make([]*userExport, 0, len(users))
	for _, u := range users {
		doc.Users = append(doc.Users, toUserExport(u))
	}

	registries, err := b.d.RegistryGetAll(ctx)
	if err != nil {
		return nil, err
	}
	doc.Registries = registries

	stacks, err := b.d.StackGetAll(ctx)
	if err != nil {
		return nil, err
	}
	doc.Stacks = stacks

	composeStacks, _, err := b.d.ComposeStackSearch(ctx, &dao.ComposeStackSearchArgs{PageIndex: 1, PageSize: 10000})
	if err != nil {
		return nil, err
	}
	doc.ComposeStacks = composeStacks

	hosts, err := b.d.HostGetAll(ctx)
	if err != nil {
		return nil, err
	}
	doc.Hosts = make([]*hostExport, 0, len(hosts))
	for _, h := range hosts {
		doc.Hosts = append(doc.Hosts, toHostExport(h))
	}

	charts, _, err := b.d.ChartSearch(ctx, &dao.ChartSearchArgs{PageIndex: 1, PageSize: 10000})
	if err != nil {
		return nil, err
	}
	// Skip built-in charts (ID starts with "$") — they're re-created automatically.
	filtered := make([]*dao.Chart, 0, len(charts))
	for _, c := range charts {
		if !strings.HasPrefix(c.ID, "$") {
			filtered = append(filtered, c)
		}
	}
	doc.Charts = filtered

	return doc, nil
}

// --- import ---------------------------------------------------------------

func (b *backupBiz) importDocument(ctx context.Context, doc *BackupDocument, components []string) (map[string]int, error) {
	if doc.Version != backupDocVersion {
		return nil, fmt.Errorf("unsupported backup version: %s", doc.Version)
	}
	selected := make(map[string]bool, len(components))
	for _, c := range components {
		selected[c] = true
	}
	if len(selected) == 0 {
		for _, c := range AllBackupComponents {
			// Events must remain opt-in.
			if c == ComponentEvents {
				continue
			}
			selected[c] = true
		}
	}

	counts := map[string]int{}

	// Iterate in dependency order.
	for _, comp := range AllBackupComponents {
		if !selected[comp] {
			continue
		}
		n, err := b.restoreComponent(ctx, comp, doc)
		if err != nil {
			return counts, fmt.Errorf("restore %s: %w", comp, err)
		}
		counts[comp] = n
	}
	return counts, nil
}

func (b *backupBiz) restoreComponent(ctx context.Context, comp string, doc *BackupDocument) (int, error) {
	switch comp {
	case ComponentSettings:
		for _, s := range doc.Settings {
			if s == nil {
				continue
			}
			if err := b.d.SettingUpdate(ctx, s); err != nil {
				return 0, err
			}
		}
		return len(doc.Settings), nil

	case ComponentRoles:
		existing, err := b.d.RoleSearch(ctx, "")
		if err != nil {
			return 0, err
		}
		for _, r := range existing {
			if err := b.d.RoleDelete(ctx, r.ID); err != nil {
				return 0, err
			}
		}
		for _, r := range doc.Roles {
			if err := b.d.RoleCreate(ctx, r); err != nil {
				return 0, err
			}
		}
		return len(doc.Roles), nil

	case ComponentUsers:
		existing, _, err := b.d.UserSearch(ctx, &dao.UserSearchArgs{Status: -1, PageIndex: 1, PageSize: 10000})
		if err != nil {
			return 0, err
		}
		for _, u := range existing {
			if err := b.d.UserDelete(ctx, u.ID); err != nil {
				return 0, err
			}
		}
		for _, u := range doc.Users {
			if err := b.d.UserCreate(ctx, fromUserExport(u)); err != nil {
				return 0, err
			}
		}
		return len(doc.Users), nil

	case ComponentRegistries:
		existing, err := b.d.RegistryGetAll(ctx)
		if err != nil {
			return 0, err
		}
		for _, r := range existing {
			if err := b.d.RegistryDelete(ctx, r.ID); err != nil {
				return 0, err
			}
		}
		for _, r := range doc.Registries {
			if err := b.d.RegistryCreate(ctx, r); err != nil {
				return 0, err
			}
		}
		return len(doc.Registries), nil

	case ComponentStacks:
		existing, err := b.d.StackGetAll(ctx)
		if err != nil {
			return 0, err
		}
		for _, s := range existing {
			if err := b.d.StackDelete(ctx, s.Name); err != nil {
				return 0, err
			}
		}
		for _, s := range doc.Stacks {
			if err := b.d.StackCreate(ctx, s); err != nil {
				return 0, err
			}
		}
		return len(doc.Stacks), nil

	case ComponentComposeStacks:
		existing, _, err := b.d.ComposeStackSearch(ctx, &dao.ComposeStackSearchArgs{PageIndex: 1, PageSize: 10000})
		if err != nil {
			return 0, err
		}
		for _, s := range existing {
			if err := b.d.ComposeStackDelete(ctx, s.ID); err != nil {
				return 0, err
			}
		}
		for _, s := range doc.ComposeStacks {
			if err := b.d.ComposeStackCreate(ctx, s); err != nil {
				return 0, err
			}
		}
		return len(doc.ComposeStacks), nil

	case ComponentHosts:
		existing, err := b.d.HostGetAll(ctx)
		if err != nil {
			return 0, err
		}
		for _, h := range existing {
			if err := b.d.HostDelete(ctx, h.ID); err != nil {
				return 0, err
			}
		}
		for _, h := range doc.Hosts {
			if err := b.d.HostCreate(ctx, fromHostExport(h)); err != nil {
				return 0, err
			}
		}
		return len(doc.Hosts), nil

	case ComponentCharts:
		existing, _, err := b.d.ChartSearch(ctx, &dao.ChartSearchArgs{PageIndex: 1, PageSize: 10000})
		if err != nil {
			return 0, err
		}
		for _, c := range existing {
			if strings.HasPrefix(c.ID, "$") {
				continue // leave built-in charts alone
			}
			if err := b.d.ChartDelete(ctx, c.ID); err != nil {
				return 0, err
			}
		}
		for _, c := range doc.Charts {
			if err := b.d.ChartCreate(ctx, c); err != nil {
				return 0, err
			}
		}
		return len(doc.Charts), nil

	case ComponentEvents:
		// Append-only: don't wipe, just import.
		for _, e := range doc.Events {
			if err := b.d.EventCreate(ctx, e); err != nil {
				return 0, err
			}
		}
		return len(doc.Events), nil
	}
	return 0, nil
}

// --- utilities ------------------------------------------------------------

func validateSchedule(s *dao.BackupSchedule) error {
	if s == nil {
		return errors.New("schedule is required")
	}
	switch s.ID {
	case BackupSourceDaily, BackupSourceWeekly, BackupSourceMonthly:
	default:
		return fmt.Errorf("invalid schedule type: %s", s.ID)
	}
	hour, min, err := parseHM(s.Time)
	if err != nil {
		return err
	}
	s.Time = fmt.Sprintf("%02d:%02d", hour, min)
	if s.DayConfig == "" {
		return errors.New("dayConfig is required")
	}
	if s.Retention < 0 {
		s.Retention = 0
	}
	return nil
}

func parseHM(t string) (hour, minute int, err error) {
	parts := strings.Split(t, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid time: %s", t)
	}
	hour, err = strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("invalid hour in time: %s", t)
	}
	minute, err = strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("invalid minute in time: %s", t)
	}
	return hour, minute, nil
}

func backupDir() string {
	if d := os.Getenv(BackupDirEnv); d != "" {
		return d
	}
	return backupDirDefault
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "backup-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func marshalGzip(doc *BackupDocument) ([]byte, error) {
	jsonBytes, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(jsonBytes); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func unmarshalGzip(data []byte) (*BackupDocument, error) {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}
	defer gr.Close()
	raw, err := io.ReadAll(gr)
	if err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}
	doc := &BackupDocument{}
	if err := json.Unmarshal(raw, doc); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return doc, nil
}

func statsFromDocument(doc *BackupDocument) map[string]int {
	return map[string]int{
		ComponentSettings:      len(doc.Settings),
		ComponentRoles:         len(doc.Roles),
		ComponentUsers:         len(doc.Users),
		ComponentRegistries:    len(doc.Registries),
		ComponentStacks:        len(doc.Stacks),
		ComponentComposeStacks: len(doc.ComposeStacks),
		ComponentHosts:         len(doc.Hosts),
		ComponentCharts:        len(doc.Charts),
		ComponentEvents:        len(doc.Events),
	}
}

func toUserExport(u *dao.User) *userExport {
	return &userExport{
		ID:        u.ID,
		Name:      u.Name,
		LoginName: u.LoginName,
		Password:  u.Password,
		Salt:      u.Salt,
		Email:     u.Email,
		Admin:     u.Admin,
		Type:      u.Type,
		Status:    u.Status,
		Roles:     u.Roles,
		Tokens:    u.Tokens,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
		CreatedBy: u.CreatedBy,
		UpdatedBy: u.UpdatedBy,
	}
}

func fromUserExport(e *userExport) *dao.User {
	return &dao.User{
		ID:        e.ID,
		Name:      e.Name,
		LoginName: e.LoginName,
		Password:  e.Password,
		Salt:      e.Salt,
		Email:     e.Email,
		Admin:     e.Admin,
		Type:      e.Type,
		Status:    e.Status,
		Roles:     e.Roles,
		Tokens:    e.Tokens,
		CreatedAt: e.CreatedAt,
		UpdatedAt: e.UpdatedAt,
		CreatedBy: e.CreatedBy,
		UpdatedBy: e.UpdatedBy,
	}
}

func toHostExport(h *dao.Host) *hostExport {
	return &hostExport{
		ID:         h.ID,
		Name:       h.Name,
		Endpoint:   h.Endpoint,
		AuthMethod: h.AuthMethod,
		TLSCACert:  h.TLSCACert,
		TLSCert:    h.TLSCert,
		TLSKey:     h.TLSKey,
		SSHUser:    h.SSHUser,
		SSHKey:     h.SSHKey,
		Status:     h.Status,
		Error:      h.Error,
		EngineVer:  h.EngineVer,
		OS:         h.OS,
		Arch:       h.Arch,
		CPUs:       h.CPUs,
		Memory:     h.Memory,
		CreatedAt:  h.CreatedAt,
		UpdatedAt:  h.UpdatedAt,
		CreatedBy:  h.CreatedBy,
		UpdatedBy:  h.UpdatedBy,
	}
}

func fromHostExport(e *hostExport) *dao.Host {
	return &dao.Host{
		ID:         e.ID,
		Name:       e.Name,
		Endpoint:   e.Endpoint,
		AuthMethod: e.AuthMethod,
		TLSCACert:  e.TLSCACert,
		TLSCert:    e.TLSCert,
		TLSKey:     e.TLSKey,
		SSHUser:    e.SSHUser,
		SSHKey:     e.SSHKey,
		Status:     e.Status,
		Error:      e.Error,
		EngineVer:  e.EngineVer,
		OS:         e.OS,
		Arch:       e.Arch,
		CPUs:       e.CPUs,
		Memory:     e.Memory,
		CreatedAt:  e.CreatedAt,
		UpdatedAt:  e.UpdatedAt,
		CreatedBy:  e.CreatedBy,
		UpdatedBy:  e.UpdatedBy,
	}
}

// ShouldRun decides whether a given schedule should fire at time `now`.
// Exposed for use by the backup package scheduler.
func ShouldRun(schedule *dao.BackupSchedule, now time.Time) bool {
	if schedule == nil || !schedule.Enabled {
		return false
	}
	hour, _, err := parseHM(schedule.Time)
	if err != nil {
		return false
	}
	if now.Hour() != hour {
		return false
	}
	// Guard against running twice in the same day.
	if schedule.LastRunAt != nil {
		last := schedule.LastRunAt.In(now.Location())
		if last.Year() == now.Year() && last.YearDay() == now.YearDay() {
			return false
		}
	}
	days := parseDays(schedule.DayConfig)
	if len(days) == 0 {
		return false
	}
	switch schedule.ID {
	case BackupSourceDaily:
		for _, d := range days {
			if int(now.Weekday()) == d {
				return true
			}
		}
	case BackupSourceWeekly:
		for _, d := range days {
			if int(now.Weekday()) == d {
				return true
			}
		}
	case BackupSourceMonthly:
		for _, d := range days {
			if now.Day() == d {
				return true
			}
		}
	}
	return false
}

func parseDays(s string) []int {
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	return out
}
