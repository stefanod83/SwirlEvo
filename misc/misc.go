package misc

import (
	"context"
	"time"

	"github.com/cuigh/auxo/errors"
)

const (
	ErrInvalidToken          = 1001
	ErrAccountDisabled       = 1002
	ErrOldPasswordIncorrect  = 1003
	ErrExternalStack         = 1004
	ErrSystemInitialized     = 1005
	ErrPasswordNotModifiable = 1006
	// ErrSelfDeployBlocked is raised when a compose stack deploy would
	// replace the very container Swirl is running inside. The operator
	// would lose the API mid-request — a classic self-destruct scenario.
	ErrSelfDeployBlocked = 1007
	// ErrVolumesContainData is raised when a Remove(removeVolumes=true)
	// would delete project volumes that carry data. The API response
	// includes the list of volumes so the UI can ask for a second
	// confirmation with force=true.
	ErrVolumesContainData = 1008
	// ErrMigrateRequiresStopped is raised when a stack migration is
	// attempted on a stack that is not in the "inactive" state.
	ErrMigrateRequiresStopped = 1009
	// ErrStackNameConflict is raised when a stack migration is attempted
	// but a stack with the same name already exists on the target host.
	ErrStackNameConflict = 1010
)

func Error(code int32, err error) error {
	return errors.Coded(code, err.Error())
}

func Page(count, pageIndex, pageSize int) (start, end int) {
	start = pageSize * (pageIndex - 1)
	end = pageSize * pageIndex
	if count < start {
		start, end = 0, 0
	} else if count < end {
		end = count
	}
	return
}

func Context(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}
