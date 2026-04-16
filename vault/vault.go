// Package vault provides a minimal HashiCorp Vault client tailored to Swirl's
// two integration use cases:
//
//  1. Reading a single secret (e.g. the SWIRL_BACKUP_KEY fallback) from a KVv2
//     Secrets Engine mount.
//  2. Resolving per-stack secrets at deploy-time so that standalone Docker
//     hosts can emulate Swarm secret injection.
//
// The client deliberately avoids the full github.com/hashicorp/vault/api SDK
// to keep the dependency surface small. Vault's HTTP API for KVv2 reads,
// token/AppRole logins, and health probes is stable and trivial to wire up
// directly.
package vault

const PkgName = "vault"
