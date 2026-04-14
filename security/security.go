package security

import (
	"github.com/cuigh/auxo/app/container"
	"github.com/cuigh/swirl/biz"
	"github.com/cuigh/swirl/misc"
)

const PkgName = "security"

func init() {
	container.Put(NewIdentifier, container.Name("identifier"))
	container.Put(NewAuthorizer, container.Name("authorizer"))
	container.Put(func(s *misc.Setting, ub biz.UserBiz) *KeycloakClient {
		return NewKeycloakClient(func() *misc.Setting { return s }, ub)
	}, container.Name("keycloak-client"))
}
