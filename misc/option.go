package misc

import (
	"strings"
	"time"

	"github.com/cuigh/auxo/config"
	"github.com/cuigh/auxo/errors"
)

// Options holds custom options of Swirl.
var Options = &struct {
	DockerEndpoint   string
	DockerAPIVersion string
	DBType           string
	DBAddress        string
	TokenKey         string
	TokenExpiry      time.Duration
	Agents           []string
	Mode             string // "swarm" or "standalone"
}{
	DBType:      "mongo",
	DBAddress:   "mongodb://localhost:27017/swirl",
	TokenExpiry: 30 * time.Minute,
	Mode:        "swarm",
}

func bindOptions() {
	var keys = []string{
		"docker_endpoint",
		"docker_api_version",
		"db_type",
		"db_address",
		"token_key",
		"token_expiry",
		"agents",
		"mode",
	}
	for _, key := range keys {
		config.BindEnv("swirl."+key, strings.ToUpper(key))
	}
}

func LoadOptions() (err error) {
	err = config.UnmarshalOption("swirl", &Options)
	if err != nil {
		err = errors.Wrap(err, "failed to load options")
	}
	return
}

// Setting represents the settings of Swirl.
type Setting struct {
	System struct {
		Version string `json:"version"`
	} `json:"system"`
	LDAP struct {
		Enabled        bool   `json:"enabled"`
		Address        string `json:"address"`
		Security       int32  `json:"security"` // 0, 1, 2
		Authentication string `json:"auth"`     // simple, bind
		BindDN         string `json:"bind_dn"`
		BindPassword   string `json:"bind_pwd"`    // Bind DN password
		BaseDN         string `json:"base_dn"`     // Base search path for users
		UserDN         string `json:"user_dn"`     // Template for the DN of the user for simple auth
		UserFilter     string `json:"user_filter"` // Search filter for user
		NameAttr       string `json:"name_attr"`
		EmailAttr      string `json:"email_attr"`
	} `json:"ldap"`
	Keycloak struct {
		Enabled        bool              `json:"enabled"`
		IssuerURL      string            `json:"issuer_url"`   // e.g. https://kc.example.com/realms/swirl
		ClientID       string            `json:"client_id"`
		ClientSecret   string            `json:"client_secret"`
		RedirectURI    string            `json:"redirect_uri"` // https://swirl.example.com/api/auth/keycloak/callback
		Scopes         string            `json:"scopes"`       // "openid profile email"
		UsernameClaim  string            `json:"username_claim"`
		EmailClaim     string            `json:"email_claim"`
		GroupsClaim    string            `json:"groups_claim"`
		AutoCreateUser bool              `json:"auto_create_user"`
		GroupRoleMap   map[string]string `json:"group_role_map"` // group name -> Swirl role id
		EnableLogout   bool              `json:"enable_logout"`
	} `json:"keycloak"`
	Metric struct {
		Prometheus string `json:"prometheus"`
	} `json:"metric"`
	Vault struct {
		Enabled            bool   `json:"enabled"`
		Address            string `json:"address"`              // e.g. https://vault.example.com:8200
		Namespace          string `json:"namespace"`            // optional, Vault Enterprise
		AuthMethod         string `json:"auth_method"`          // "token" | "approle"
		Token              string `json:"token"`                // used when AuthMethod == "token"
		AppRolePath        string `json:"approle_path"`         // auth mount path for AppRole (default "approle")
		RoleID             string `json:"role_id"`              // used when AuthMethod == "approle"
		SecretID           string `json:"secret_id"`            // used when AuthMethod == "approle"
		KVMount            string `json:"kv_mount"`             // KVv2 mount point (default "secret")
		KVPrefix           string `json:"kv_prefix"`            // logical prefix inside the mount (e.g. "swirl/")
		BackupKeyPath      string `json:"backup_key_path"`      // path (under prefix) to the SWIRL_BACKUP_KEY secret (default "backup-key")
		BackupKeyField     string `json:"backup_key_field"`     // field name inside the KV entry (default "value")
		DefaultStorageMode string `json:"default_storage_mode"` // "tmpfs" | "volume" | "init" (default "tmpfs")
		TLSSkipVerify      bool   `json:"tls_skip_verify"`      // dev only
		CACert             string `json:"ca_cert"`              // PEM-encoded, optional
		RequestTimeout     int    `json:"request_timeout"`      // seconds, default 10
	} `json:"vault"`
	// Backup storage policy. The archive format (AES-256-GCM with
	// SWIRL_BACKUP_KEY) does not depend on the storage location; only the
	// destination of the encrypted bytes does.
	Backup struct {
		StorageMode string `json:"storage_mode"` // "fs" (default) | "vault" | "db"
		// VaultPrefix is appended to the configured Vault.KVPrefix when
		// storage_mode=vault. Defaults to "backups" so the full KVv2
		// path becomes `<mount>/data/<kv_prefix><vault_prefix>/<id>`.
		VaultPrefix string `json:"vault_prefix"`
	} `json:"backup"`
	// SelfDeploy carries the self-deploy feature configuration (v3
	// paradigm: flag + sidekick options, no template/placeholders,
	// no recovery UI). The biz layer loads + saves this sub-struct
	// through the Setting id "self_deploy". The YAML to deploy is
	// read verbatim from the ComposeStack identified by SourceStackID.
	//
	// Retrocompat: older records with `template`, `placeholders`,
	// `recoveryPort`, `recoveryAllow` fields unmarshal cleanly thanks
	// to json.Unmarshal ignoring unknown keys. LoadConfig fills
	// zero-valued fields with safe defaults (AutoRollback=true,
	// DeployTimeout=300).
	SelfDeploy struct {
		Enabled       bool   `json:"enabled"`
		SourceStackID string `json:"sourceStackId"`
		AutoRollback  bool   `json:"autoRollback"`
		DeployTimeout int    `json:"deployTimeout"` // seconds
	} `json:"self_deploy"`
}

// IsStandalone returns true when Swirl is running in standalone mode.
func IsStandalone() bool {
	return Options.Mode == "standalone"
}

func init() {
	bindOptions()
}
