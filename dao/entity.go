package dao

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/cuigh/auxo/data"
	"github.com/cuigh/auxo/errors"
	"github.com/cuigh/auxo/ext/times"
	"github.com/docker/docker/api/types/registry"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/bsontype"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/x/bsonx/bsoncore"
)

type Time time.Time

func (t Time) MarshalBSONValue() (bsontype.Type, []byte, error) {
	return bson.MarshalValue(time.Time(t))
}

func (t *Time) UnmarshalBSONValue(bt bsontype.Type, data []byte) error {
	if v, _, valid := bsoncore.ReadValue(data, bt); valid {
		*t = Time(v.Time())
		return nil
	}
	return errors.Format("unmarshal failed, type: %s, data:%s", bt, data)
}

func (t Time) MarshalJSON() (b []byte, err error) {
	return strconv.AppendInt(b, times.ToUnixMilli(time.Time(t)), 10), nil
}

func (t *Time) UnmarshalJSON(data []byte) (err error) {
	i, err := strconv.ParseInt(string(data), 10, 64)
	if err == nil {
		*t = Time(times.FromUnixMilli(i))
	}
	return err
}

func (t Time) String() string {
	return time.Time(t).String()
}

type Operator struct {
	ID   string `json:"id,omitempty" bson:"id,omitempty"`
	Name string `json:"name,omitempty" bson:"name,omitempty"`
}

// Setting represents the options of swirl.
type Setting struct {
	ID        string    `json:"id" bson:"_id"`
	Options   string    `json:"options" bson:"options"`
	UpdatedAt time.Time `json:"updatedAt" bson:"updated_at"`
	UpdatedBy Operator  `json:"updatedBy" bson:"updated_by"`
}

type Role struct {
	ID          string   `json:"id,omitempty" bson:"_id"`
	Name        string   `json:"name,omitempty" bson:"name" valid:"required"`
	Description string   `json:"desc,omitempty" bson:"desc,omitempty"`
	Perms       []string `json:"perms,omitempty" bson:"perms,omitempty"`
	UpdatedAt   Time     `json:"updatedAt" bson:"updated_at"`
	CreatedAt   Time     `json:"createdAt" bson:"created_at"`
	CreatedBy   Operator `json:"createdBy" bson:"created_by"`
	UpdatedBy   Operator `json:"updatedBy" bson:"updated_by"`
}

type User struct {
	ID        string       `json:"id,omitempty" bson:"_id"`
	Name      string       `json:"name" bson:"name" valid:"required"`
	LoginName string       `json:"loginName" bson:"login_name" valid:"required"`
	Password  string       `json:"-" bson:"password"`
	Salt      string       `json:"-" bson:"salt"`
	Email     string       `json:"email" bson:"email" valid:"required"`
	Admin     bool         `json:"admin" bson:"admin"`
	Type      string       `json:"type" bson:"type"`
	Status    int32        `json:"status" bson:"status"`
	Roles     []string     `json:"roles,omitempty" bson:"roles,omitempty"`
	Tokens    data.Options `json:"tokens,omitempty" bson:"tokens,omitempty"`
	CreatedAt Time         `json:"createdAt" bson:"created_at"`
	UpdatedAt Time         `json:"updatedAt" bson:"updated_at"`
	CreatedBy Operator     `json:"createdBy" bson:"created_by"`
	UpdatedBy Operator     `json:"updatedBy" bson:"updated_by"`
}

type UserSearchArgs struct {
	Name      string
	LoginName string
	Admin     bool
	Status    int32
	PageIndex int
	PageSize  int
}

type Registry struct {
	ID       string `json:"id" bson:"_id"`
	Name     string `json:"name" bson:"name"`
	URL      string `json:"url" bson:"url"`
	Username string `json:"username" bson:"username"`
	Password string `json:"password,omitempty" bson:"password,omitempty"`
	// SkipTLSVerify disables TLS verification for the HTTP v2 registry
	// calls Swirl issues from its own process (catalog browse, tag
	// listing). It does NOT affect the Docker daemon: for `docker push`
	// against a self-signed registry the host's daemon must still have
	// the registry in `insecure-registries`.
	SkipTLSVerify bool     `json:"skipTlsVerify,omitempty" bson:"skip_tls_verify,omitempty"`
	CreatedAt     Time     `json:"createdAt" bson:"created_at"`
	UpdatedAt     Time     `json:"updatedAt" bson:"updated_at"`
	CreatedBy     Operator `json:"createdBy" bson:"created_by"`
	UpdatedBy     Operator `json:"updatedBy" bson:"updated_by"`
}

func (r *Registry) Match(image string) bool {
	return strings.HasPrefix(image, r.URL)
}

func (r *Registry) GetEncodedAuth() string {
	cfg := &registry.AuthConfig{
		ServerAddress: r.URL,
		Username:      r.Username,
		Password:      r.Password,
	}
	if buf, e := json.Marshal(cfg); e == nil {
		return base64.URLEncoding.EncodeToString(buf)
	}
	return ""
}

type Stack struct {
	Name      string   `json:"name" bson:"_id"`
	Content   string   `json:"content" bson:"content"`
	Services  []string `json:"services,omitempty" bson:"-"`
	Internal  bool     `json:"internal" bson:"-"`
	CreatedAt Time     `json:"createdAt" bson:"created_at"`
	UpdatedAt Time     `json:"updatedAt" bson:"updated_at"`
	CreatedBy Operator `json:"createdBy" bson:"created_by"`
	UpdatedBy Operator `json:"updatedBy" bson:"updated_by"`
}

type Event struct {
	ID       primitive.ObjectID `json:"id" bson:"_id"`
	Type     string             `json:"type" bson:"type"`
	Action   string             `json:"action" bson:"action"`
	Args     data.Map           `json:"args" bson:"args"`
	UserID   string             `json:"userId" bson:"user_id"`
	Username string             `json:"username" bson:"username"`
	// OriginatingUser is populated on a Swirl swarm target when an
	// incoming API request carries the `X-Swirl-Originating-User`
	// header (set by a federated Swirl portal). `Username` is the
	// federation peer identity; OriginatingUser records the human
	// behind it — ~"peer=federation-peer-portal-1 origin=alice".
	// Empty for direct (non-federated) requests.
	OriginatingUser string `json:"originatingUser,omitempty" bson:"originating_user,omitempty"`
	Time     Time               `json:"time" bson:"time"`
}

type EventSearchArgs struct {
	Type      string `bind:"type"`
	Name      string `bind:"name"`
	PageIndex int    `bind:"pageIndex"`
	PageSize  int    `bind:"pageSize"`
}

// Chart represents a dashboard chart.
type Chart struct {
	ID          string        `json:"id" bson:"_id"` // the id of built-in charts has '$' prefix.
	Title       string        `json:"title" bson:"title" valid:"required"`
	Description string        `json:"desc" bson:"desc"`
	Metrics     []ChartMetric `json:"metrics" bson:"metrics" valid:"required"`
	Dashboard   string        `json:"dashboard" bson:"dashboard"` // home/service...
	Type        string        `json:"type" bson:"type"`           // pie/line...
	Unit        string        `json:"unit" bson:"unit"`           // bytes/milliseconds/percent:100...
	Width       int32         `json:"width" bson:"width"`         // 1-12(12 columns total)
	Height      int32         `json:"height" bson:"height"`       // default 50
	Options     data.Map      `json:"options,omitempty" bson:"options,omitempty"`
	Margin      struct {
		Left   int32 `json:"left,omitempty" bson:"left,omitempty"`
		Right  int32 `json:"right,omitempty" bson:"right,omitempty"`
		Top    int32 `json:"top,omitempty" bson:"top,omitempty"`
		Bottom int32 `json:"bottom,omitempty" bson:"bottom,omitempty"`
	} `json:"margin" bson:"margin"`
	CreatedAt Time     `json:"createdAt" bson:"created_at"`
	UpdatedAt Time     `json:"updatedAt" bson:"updated_at"`
	CreatedBy Operator `json:"createdBy" bson:"created_by"`
	UpdatedBy Operator `json:"updatedBy" bson:"updated_by"`
}

func NewChart(dashboard, id, title, legend, query, unit string, left int32) *Chart {
	c := &Chart{
		ID:          id,
		Title:       title,
		Description: title,
		Metrics: []ChartMetric{
			{Legend: legend, Query: query},
		},
		Dashboard: dashboard,
		Type:      "line",
		Unit:      unit,
		Width:     12,
		Height:    200,
	}
	c.Margin.Left = left
	return c
}

type ChartMetric struct {
	Legend string `json:"legend"`
	Query  string `json:"query"`
}

type ChartSearchArgs struct {
	Title     string `bind:"title"`
	Dashboard string `bind:"dashboard"`
	PageIndex int    `bind:"pageIndex"`
	PageSize  int    `bind:"pageSize"`
}

type Dashboard struct {
	Name      string      `json:"name" bson:"name"`
	Key       string      `json:"key,omitempty" bson:"key,omitempty"`
	Period    int32       `json:"period,omitempty" bson:"period,omitempty"`     // data range in minutes
	Interval  int32       `json:"interval,omitempty" bson:"interval,omitempty"` // refresh interval in seconds, 0 means disabled.
	Charts    []ChartInfo `json:"charts,omitempty" bson:"charts,omitempty"`
	UpdatedAt Time        `json:"-" bson:"updated_at"`
	UpdatedBy Operator    `json:"-" bson:"updated_by"`
}

type ChartInfo struct {
	ID     string `json:"id" bson:"id"`
	Width  int32  `json:"width,omitempty" bson:"width,omitempty"`
	Height int32  `json:"height,omitempty" bson:"height,omitempty"`
	Title  string `json:"title" bson:"-"`
	Type   string `json:"type" bson:"-"`
	Unit   string `json:"unit" bson:"-"`
	Margin struct {
		Left   int32 `json:"left,omitempty" bson:"-"`
		Right  int32 `json:"right,omitempty" bson:"-"`
		Top    int32 `json:"top,omitempty" bson:"-"`
		Bottom int32 `json:"bottom,omitempty" bson:"-"`
	} `json:"margin" bson:"-"`
}

func (cd *Dashboard) ID() string {
	if cd.Key == "" {
		return cd.Name
	}
	return cd.Name + ":" + cd.Key
}

// Host represents a standalone Docker host managed by Swirl.
type Host struct {
	ID        string   `json:"id" bson:"_id"`
	Name      string   `json:"name" bson:"name" valid:"required"`
	// Endpoint accepts unix://, tcp://, tcp+tls://, ssh://, and
	// https://... (the latter for `swarm_via_swirl` federation
	// hosts — see `Type` below).
	Endpoint  string   `json:"endpoint" bson:"endpoint" valid:"required"`
	AuthMethod string  `json:"authMethod" bson:"auth_method"` // socket, tcp, tcp+tls, ssh, swirl
	TLSCACert string   `json:"tlsCaCert,omitempty" bson:"tls_ca_cert,omitempty"`
	TLSCert   string   `json:"tlsCert,omitempty" bson:"tls_cert,omitempty"`
	TLSKey    string   `json:"-" bson:"tls_key,omitempty"`
	SSHUser   string   `json:"sshUser,omitempty" bson:"ssh_user,omitempty"`
	SSHKey    string   `json:"-" bson:"ssh_key,omitempty"`
	Status    string   `json:"status" bson:"status"`       // connected, disconnected, error
	Error     string   `json:"error,omitempty" bson:"error,omitempty"`
	// Color is an optional hex string (e.g. "#4b91ff") the operator can
	// associate with the host so the UI renders a visible marker (a
	// horizontal bar under the page header) whenever the host is the
	// active selection. Empty string means "no colour" — the UI draws
	// no marker. Validated client-side; server stores whatever string
	// is supplied (the UI constrains the picker output).
	Color     string   `json:"color,omitempty" bson:"color,omitempty"`
	// Type classifies how Swirl talks to this host:
	//   - "standalone"      — direct Docker daemon (unix/tcp/tcp+tls/ssh)
	//   - "swarm_via_swirl" — federation: HTTPS call to a remote Swirl
	//                         running in MODE=swarm. Swirl proxies every
	//                         request to the remote Swirl's REST API,
	//                         authenticated by SwirlToken.
	// Auto-detected by `probeHost` at Save/Sync time. Empty string on
	// legacy records — a background migration upgrades them.
	Type      string   `json:"type,omitempty" bson:"type,omitempty"`
	// SwirlURL is the base URL of the federated Swirl swarm target
	// (only when Type == "swarm_via_swirl"). Trailing slash stripped.
	SwirlURL  string   `json:"swirlUrl,omitempty" bson:"swirl_url,omitempty"`
	// SwirlToken is the long-lived API token of the federation peer
	// user registered on the remote Swirl swarm instance. Never sent
	// in cleartext to the UI (GET returns the mask placeholder) and
	// never written to audit events. NEVER persisted to backup
	// archives unmasked — sanitised like other secret fields.
	SwirlToken string  `json:"swirlToken,omitempty" bson:"swirl_token,omitempty"`
	// TokenExpiresAt is the informational expiry date of the
	// federation token. Soft-expiry: operations continue to work past
	// the date; a global UI banner warns the operator to rotate.
	// Zero value means "no expiry tracked".
	TokenExpiresAt Time  `json:"tokenExpiresAt,omitempty" bson:"token_expires_at,omitempty"`
	// TokenAutoRefresh, when true, lets the portal Swirl call the
	// federated Swirl's renewal endpoint periodically to extend the
	// expiry without operator intervention.
	TokenAutoRefresh bool `json:"tokenAutoRefresh,omitempty" bson:"token_auto_refresh,omitempty"`
	// Immutable is set on system-managed entries (most notably the
	// auto-registered `local` host pointing at Swirl's own docker
	// socket). Prevents Edit/Delete via the API — returned as 403.
	Immutable bool      `json:"immutable,omitempty" bson:"immutable,omitempty"`
	EngineVer string   `json:"engineVersion,omitempty" bson:"engine_ver,omitempty"`
	OS        string   `json:"os,omitempty" bson:"os,omitempty"`
	Arch      string   `json:"arch,omitempty" bson:"arch,omitempty"`
	CPUs      int      `json:"cpus,omitempty" bson:"cpus,omitempty"`
	Memory    int64    `json:"memory,omitempty" bson:"memory,omitempty"`
	CreatedAt Time     `json:"createdAt" bson:"created_at"`
	UpdatedAt Time     `json:"updatedAt" bson:"updated_at"`
	CreatedBy Operator `json:"createdBy" bson:"created_by"`
	UpdatedBy Operator `json:"updatedBy" bson:"updated_by"`
	// AddonConfigExtract is a JSON blob (kept as a string so the DAO
	// layer never has to know the schema) holding lists extracted from
	// uploaded add-on config files — e.g. Traefik static config ->
	// entryPoints, certResolvers, middlewares, networks. Structure is
	// defined at the biz layer and consumed by AddonDiscoveryBiz to
	// augment the dropdowns of the stack-editor wizard tabs. The raw
	// file is never persisted (may contain ACME keys).
	AddonConfigExtract string `json:"addonConfigExtract,omitempty" bson:"addon_config_extract,omitempty"`
}

type HostSearchArgs struct {
	Name      string `bind:"name"`
	Status    string `bind:"status"`
	PageIndex int    `bind:"pageIndex"`
	PageSize  int    `bind:"pageSize"`
}

// ComposeStack represents a docker-compose style stack deployed on a standalone host.
// Swirl persists the compose YAML so the stack can be redeployed / reconciled.
type ComposeStack struct {
	ID           string   `json:"id" bson:"_id"`
	HostID       string   `json:"hostId" bson:"host_id"`
	Name         string   `json:"name" bson:"name" valid:"required"`
	Content      string   `json:"content,omitempty" bson:"content"`
	// EnvFile holds key=value lines (one per line, like a .env file).
	// Variables are substituted into the compose YAML at deploy time
	// via os.Setenv so the Docker SDK's built-in variable expansion
	// picks them up.
	EnvFile      string   `json:"envFile,omitempty" bson:"env_file,omitempty"`
	Status       string   `json:"status" bson:"status"` // active, inactive, partial, error
	ErrorMessage string   `json:"errorMessage,omitempty" bson:"error_message,omitempty"`
	// LastWarnings captures non-fatal observations from the most recent
	// deploy attempt (e.g. compose fields that were silently ignored
	// because they only make sense in Swarm mode). Cleared on a clean
	// deploy, overwritten on every redeploy. Never surfaces as an error.
	LastWarnings []string `json:"lastWarnings,omitempty" bson:"last_warnings,omitempty"`
	// DisableRegistryCache opts this stack OUT of deploy-time image
	// rewriting when the global Registry Cache mirror is enabled. Set
	// for stacks that must talk directly to an upstream (e.g. the
	// registry:2 cache itself, CI runners). Default false = follow the
	// global RewriteMode policy.
	DisableRegistryCache bool `json:"disableRegistryCache,omitempty" bson:"disable_registry_cache,omitempty"`
	CreatedAt    Time     `json:"createdAt" bson:"created_at"`
	UpdatedAt    Time     `json:"updatedAt" bson:"updated_at"`
	CreatedBy    Operator `json:"createdBy" bson:"created_by"`
	UpdatedBy    Operator `json:"updatedBy" bson:"updated_by"`
}

type ComposeStackSearchArgs struct {
	HostID    string `bind:"hostId"`
	Name      string `bind:"name"`
	PageIndex int    `bind:"pageIndex"`
	PageSize  int    `bind:"pageSize"`
}

// ComposeStackVersion is a point-in-time snapshot of a compose stack's
// Content + EnvFile captured before any mutating Save. Revisions are
// monotonic per StackID (not per install) so the UI can show "rev 7"
// regardless of DB cardinality. Retention is enforced by the biz layer
// (Prune), not here — the DAO stores whatever the biz writes.
type ComposeStackVersion struct {
	ID        string   `json:"id" bson:"_id"`
	StackID   string   `json:"stackId" bson:"stack_id"`
	Revision  int      `json:"revision" bson:"revision"`
	Content   string   `json:"content,omitempty" bson:"content,omitempty"`
	EnvFile   string   `json:"envFile,omitempty" bson:"env_file,omitempty"`
	// Reason is a short tag describing why the snapshot was taken:
	//   "save"          — plain save / deploy that changed Content
	//   "addon-inject"  — save triggered the addon wizard label injection
	//   "restore:rev<N>" — snapshot taken before restoring revision N
	// The UI surfaces it in the History dropdown.
	Reason    string   `json:"reason" bson:"reason"`
	CreatedAt Time     `json:"createdAt" bson:"created_at"`
	CreatedBy Operator `json:"createdBy" bson:"created_by"`
}

type Session struct {
	ID        string    `json:"id" bson:"_id"` // token
	UserID    string    `json:"userId" bson:"user_id"`
	Username  string    `json:"username" bson:"username"`
	Admin     bool      `json:"admin" bson:"admin"`
	Roles     []string  `json:"roles" bson:"roles"`
	Perm      uint64    `json:"perm" bson:"perm"`
	Perms     []string  `json:"-" bson:"-"`
	Dirty     bool      `json:"dirty" bson:"dirty"`
	Expiry    time.Time `json:"expiry" bson:"expiry"`
	MaxExpiry time.Time `json:"maxExpiry" bson:"max_expiry"`
	CreatedAt time.Time `json:"createdAt" bson:"created_at"`
	UpdatedAt time.Time `json:"updatedAt" bson:"updated_at"`
}

// Backup represents a stored backup archive. Archive bytes live on disk
// under /data/swirl/backups/<id>.swb; this struct holds only metadata.
type Backup struct {
	ID        string         `json:"id" bson:"_id"`
	Name      string         `json:"name" bson:"name"`
	Source    string         `json:"source" bson:"source"` // manual | daily | weekly | monthly
	Size      int64          `json:"size" bson:"size"`
	Checksum  string         `json:"checksum" bson:"checksum"` // sha256 of plaintext JSON
	Path      string         `json:"path" bson:"path"`
	Includes  []string       `json:"includes,omitempty" bson:"includes,omitempty"`
	Stats     map[string]int `json:"stats,omitempty" bson:"stats,omitempty"`
	// KeyFingerprint is the HMAC tag of the master key the archive was
	// encrypted with. Empty for backups created before this field was added
	// (treated as "unverified" by the compatibility check).
	KeyFingerprint string `json:"keyFingerprint,omitempty" bson:"key_fingerprint,omitempty"`
	// VerifiedAt records when this backup was last successfully verified
	// against the current master key.
	VerifiedAt *time.Time `json:"verifiedAt,omitempty" bson:"verified_at,omitempty"`
	// KeyStatus is computed at read time and never persisted. One of
	// "compatible" | "incompatible" | "unverified" | "missing" | "unknown".
	KeyStatus string   `json:"keyStatus,omitempty" bson:"-"`
	CreatedAt time.Time `json:"createdAt" bson:"created_at"`
	CreatedBy Operator  `json:"createdBy" bson:"created_by"`
}

// BackupSchedule describes a recurring backup job.
// The ID is the schedule type itself ("daily" | "weekly" | "monthly") so
// there is at most one row per type.
type BackupSchedule struct {
	ID        string     `json:"id" bson:"_id"` // daily | weekly | monthly
	Enabled   bool       `json:"enabled" bson:"enabled"`
	DayConfig string     `json:"dayConfig" bson:"day_config"` // daily: "1,2,3,4,5" weekly: "1" monthly: "15"
	Time      string     `json:"time" bson:"time"`            // HH:MM
	Retention int        `json:"retention" bson:"retention"`  // 0 = unlimited
	LastRunAt *time.Time `json:"lastRunAt,omitempty" bson:"last_run_at,omitempty"`
	CreatedAt time.Time  `json:"createdAt" bson:"created_at"`
	UpdatedAt time.Time  `json:"updatedAt" bson:"updated_at"`
}

// VaultSecret is a catalog entry that points at a secret stored inside
// HashiCorp Vault. The catalog is used to emulate Docker Swarm secrets on
// standalone Docker hosts: stacks reference these entries by ID and Swirl
// resolves the current value from Vault at deploy time.
//
// Only references are persisted. The actual secret value never hits the
// Swirl database and is never included in backups.
type VaultSecret struct {
	ID          string            `json:"id" bson:"_id"`
	Name        string            `json:"name" bson:"name" valid:"required"`
	Description string            `json:"desc,omitempty" bson:"desc,omitempty"`
	// Path is the entry name below the configured global prefix
	// (Settings.Vault.KVPrefix). For example, if prefix = "swirl/" and
	// Path = "myapp/db", the fetch URL is <mount>/data/swirl/myapp/db.
	Path string `json:"path" bson:"path" valid:"required"`
	// Field selects a single field inside the KVv2 entry. When empty the
	// *entire* JSON object is marshalled as the secret value (useful for
	// consumers that expect to parse a JSON blob).
	Field  string            `json:"field,omitempty" bson:"field,omitempty"`
	Labels map[string]string `json:"labels,omitempty" bson:"labels,omitempty"`
	CreatedAt Time     `json:"createdAt" bson:"created_at"`
	UpdatedAt Time     `json:"updatedAt" bson:"updated_at"`
	CreatedBy Operator `json:"createdBy" bson:"created_by"`
	UpdatedBy Operator `json:"updatedBy" bson:"updated_by"`
}

type VaultSecretSearchArgs struct {
	Name      string `bind:"name"`
	PageIndex int    `bind:"pageIndex"`
	PageSize  int    `bind:"pageSize"`
}

// ComposeStackSecretBinding attaches a VaultSecret catalog entry to a compose
// stack deployed on a standalone host, describing how the secret value should
// be materialized inside a container at deploy time.
//
// The binding never stores the secret value. At deploy time Swirl resolves the
// value from Vault, materializes it according to StorageMode, and records a
// SHA-256 hash (DeployedHash) for drift detection on the next restart.
type ComposeStackSecretBinding struct {
	ID            string `json:"id" bson:"_id"`
	StackID       string `json:"stackId" bson:"stack_id" valid:"required"`
	VaultSecretID string `json:"vaultSecretId" bson:"vault_secret_id" valid:"required"`
	// Field overrides the VaultSecret catalog entry's Field selector
	// for this binding. When non-empty, the materializer uses this
	// instead of rec.Field — allows N bindings from the same catalog
	// entry to point at different KVv2 fields (e.g. DB_HOST, DB_PASS).
	Field string `json:"field,omitempty" bson:"field,omitempty"`
	// Service name inside the compose file this binding applies to.
	// Empty string means "all services" (useful when a stack has a single
	// service and the user doesn't want to type the name).
	Service string `json:"service,omitempty" bson:"service,omitempty"`
	// TargetType selects the injection mechanism: "file" mounts the secret
	// value as a file inside the container; "env" exposes it as an
	// environment variable. Environment injection is convenient but the
	// value shows up in `docker inspect` output, so "file" is preferred
	// for production.
	TargetType string `json:"targetType" bson:"target_type" valid:"required"` // file | env
	// TargetPath is the path inside the container where the secret file is
	// mounted (used when TargetType == "file"). Typical value:
	// "/run/secrets/db_password".
	TargetPath string `json:"targetPath,omitempty" bson:"target_path,omitempty"`
	// EnvName is the environment variable name (used when TargetType == "env").
	EnvName string `json:"envName,omitempty" bson:"env_name,omitempty"`
	// File mode / ownership — applied only when TargetType == "file".
	UID  int    `json:"uid,omitempty" bson:"uid,omitempty"`
	GID  int    `json:"gid,omitempty" bson:"gid,omitempty"`
	Mode string `json:"mode,omitempty" bson:"mode,omitempty"` // octal, e.g. "0400"
	// StorageMode controls where the materialized secret lives on the host:
	//   - "tmpfs":   a tmpfs mount backing a single file in the container
	//     (cleanest, value never touches the host disk).
	//   - "volume":  a project-scoped named volume populated before start
	//     (persists across restarts, preserved on redeploy).
	//   - "init":    a sidecar init container writes the file into a shared
	//     volume before the service container starts.
	// Ignored when TargetType == "env".
	StorageMode string `json:"storageMode" bson:"storage_mode"` // tmpfs | volume | init
	// DeployedHash is the SHA-256 of the value that was materialized at the
	// last successful Deploy. Used by the drift check at restart.
	DeployedHash string `json:"deployedHash,omitempty" bson:"deployed_hash,omitempty"`
	DeployedAt   Time   `json:"deployedAt,omitempty" bson:"deployed_at,omitempty"`
	CreatedAt    Time   `json:"createdAt" bson:"created_at"`
	UpdatedAt    Time   `json:"updatedAt" bson:"updated_at"`
	CreatedBy    Operator `json:"createdBy" bson:"created_by"`
	UpdatedBy    Operator `json:"updatedBy" bson:"updated_by"`
}
