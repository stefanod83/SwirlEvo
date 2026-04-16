package api

import (
	"github.com/cuigh/auxo/data"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/misc"
	"github.com/cuigh/swirl/vault"
)

// VaultHandler groups admin-facing Vault endpoints. Only Settings admins
// should have access to it — protected by the "vault.admin" permission.
type VaultHandler struct {
	Test web.HandlerFunc `path:"/test" method:"post" auth:"vault.admin" desc:"test vault connection and credentials"`
}

// NewVault wires the handler against an existing Vault client.
func NewVault(c *vault.Client) *VaultHandler {
	return &VaultHandler{
		Test: vaultTest(c),
	}
}

// vaultTest runs both an unauthenticated /sys/health probe and a full auth
// round-trip using the currently saved settings. It returns a structured
// response so the UI can show a specific reason when something fails.
func vaultTest(c *vault.Client) web.HandlerFunc {
	return func(ctx web.Context) error {
		reqCtx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		out := data.Map{"ok": false}

		sealed, initialized, version, err := c.Health(reqCtx)
		if err != nil {
			out["stage"] = "health"
			out["error"] = err.Error()
			return success(ctx, out)
		}
		out["sealed"] = sealed
		out["initialized"] = initialized
		out["version"] = version
		if sealed {
			out["stage"] = "health"
			out["error"] = "vault is sealed"
			return success(ctx, out)
		}
		if !initialized {
			out["stage"] = "health"
			out["error"] = "vault is not initialized"
			return success(ctx, out)
		}

		if err := c.TestAuth(reqCtx); err != nil {
			out["stage"] = "auth"
			out["error"] = err.Error()
			return success(ctx, out)
		}
		out["ok"] = true
		out["stage"] = "ok"
		return success(ctx, out)
	}
}
