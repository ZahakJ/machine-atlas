package obs

import (
	"errors"
	"io/fs"
	"os/exec"
	"syscall"
)

// FromError maps a read or command error to the status it implies:
//
//   - nil → ok
//   - ENOENT, ENOTDIR, a missing executable → absent: the interface answered
//     that the thing is not there
//   - EACCES, EPERM → permission_denied
//   - errors.ErrUnsupported (which ENOSYS and EOPNOTSUPP also match) →
//     unsupported: the platform or kernel lacks the interface by rule
//   - anything else, including timeouts → failed
//
// ENOENT cannot always be told apart from "the kernel was built without this
// feature"; collectors that know which applies should say so in the note.
func FromError(err error) Status {
	switch {
	case err == nil:
		return StatusOK
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOTDIR), errors.Is(err, exec.ErrNotFound):
		return StatusAbsent
	case errors.Is(err, fs.ErrPermission):
		return StatusPermissionDenied
	case errors.Is(err, errors.ErrUnsupported):
		return StatusUnsupported
	default:
		return StatusFailed
	}
}
