package vault

import (
	"github.com/cuigh/auxo/app/container"
	"github.com/cuigh/swirl/misc"
)

// The client is registered lazily so that Settings changes are picked up
// on every call (the loader closure captures the live *misc.Setting pointer
// injected by main.loadSetting).
func init() {
	container.Put(func(s *misc.Setting) *Client {
		return NewClient(func() *misc.Setting { return s })
	}, container.Name("vault-client"))
}
