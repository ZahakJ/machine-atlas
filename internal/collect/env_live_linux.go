//go:build linux

package collect

import "golang.org/x/sys/unix"

// Uname implements Env.
func (LiveEnv) Uname() (Uname, error) {
	var u unix.Utsname
	if err := unix.Uname(&u); err != nil {
		return Uname{}, err
	}
	return Uname{Machine: unix.ByteSliceToString(u.Machine[:]), Release: unix.ByteSliceToString(u.Release[:])}, nil
}

// PageSize implements Env.
func (LiveEnv) PageSize() (int, error) { return unix.Getpagesize(), nil }
