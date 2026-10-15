//go:build !linux

package collect

import "errors"

// Uname implements Env. Machine Atlas describes Linux kernels only; on other
// systems the live environment reports the interfaces as unsupported.
func (LiveEnv) Uname() (Uname, error) { return Uname{}, errors.ErrUnsupported }

// PageSize implements Env; see Uname.
func (LiveEnv) PageSize() (int, error) { return 0, errors.ErrUnsupported }
