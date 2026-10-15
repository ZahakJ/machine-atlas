//go:build linux

package experiment

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	fileBytes  = 16 * mib
	minFree    = 256 * mib
	chunkBytes = 1 * mib
	// filePrefix and fileSuffix name the temporary file, with the child's
	// PID between them, so a later run can tell a crashed run's file from a
	// running one's.
	filePrefix = "page-cache-residency-"
	fileSuffix = ".tmp"
)

var residencyNotes = []string{
	"mincore reports whether each page of the file is in the page cache; it does not say which storage device holds the data or whether the data has reached it.",
	"The timings are one 16 MiB read on this device at this moment: other I/O, CPU frequency, readahead and the drive's own cache all change them. They are not a benchmark.",
	"POSIX_FADV_DONTNEED is advice: the kernel may keep pages that are dirty, under writeback or in use.",
	"The file holds non-zero pseudo-random bytes so that no filesystem can store it as a hole or compress it away.",
}

// pageCacheResidency runs the page-cache-residency experiment.
func pageCacheResidency(rec *recorder) (notes []string, err error) {
	dir, err := cacheDir()
	if err != nil {
		return nil, err
	}
	removeStale(dir)

	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return nil, fmt.Errorf("statfs on the cache directory: %w", err)
	}
	fsType := fsName(uint32(st.Type))
	free := int64(st.Bavail) * int64(st.Bsize)
	rec.add("fs.type", "setup", "Filesystem type of the cache directory (statfs)", fsType, "")
	rec.add("fs.free_bytes", "setup", "Free space available on that filesystem", free, "bytes")
	if free < minFree {
		return nil, fmt.Errorf("refusing to run: the cache directory's filesystem has %d MiB free, less than %d MiB", free/mib, minFree/mib)
	}
	ps := os.Getpagesize()
	pages := fileBytes / ps
	rec.add("file_bytes", "setup", "Size of the temporary file", int64(fileBytes), "bytes")
	rec.add("file_pages", "setup", "Pages in the temporary file", int64(pages), "count")

	path := filepath.Join(dir, filePrefix+strconv.Itoa(os.Getpid())+fileSuffix)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create the temporary file: %w", noPath(err))
	}
	pendingRemove.Store(&path)
	removed := false
	defer func() {
		f.Close()
		if !removed {
			os.Remove(path)
		}
	}()
	fd := int(f.Fd())
	buf := make([]byte, chunkBytes)
	vec := make([]byte, pages)

	if err := writeFile(f, buf); err != nil {
		return nil, err
	}
	residency := func(step, id, label string) error {
		resident, err := residentPages(fd, vec)
		if err != nil {
			return err
		}
		rec.add(id, step, label, 100*float64(resident)/float64(pages), "percent")
		return nil
	}
	if err := residency("after-write", "residency.after_write", "File pages in the page cache after write and fsync"); err != nil {
		return nil, err
	}
	if err := unix.Fadvise(fd, 0, 0, unix.FADV_DONTNEED); err != nil {
		return nil, fmt.Errorf("posix_fadvise: %w", err)
	}
	if err := residency("after-advice", "residency.after_advice", "File pages in the page cache after POSIX_FADV_DONTNEED"); err != nil {
		return nil, err
	}
	cold, err := timedRead(fd, buf)
	if err != nil {
		return nil, err
	}
	rec.add("read.cold_seconds", "cold-read", "Time to read the whole file with read(2), first time", cold.Seconds(), "seconds")
	if err := residency("cold-read", "residency.after_cold_read", "File pages in the page cache after the cold read"); err != nil {
		return nil, err
	}
	warm, err := timedRead(fd, buf)
	if err != nil {
		return nil, err
	}
	rec.add("read.warm_seconds", "warm-read", "Time to read the whole file with read(2), second time", warm.Seconds(), "seconds")

	f.Close()
	if err := os.Remove(path); err != nil {
		return nil, fmt.Errorf("delete the temporary file: %w", noPath(err))
	}
	removed = true
	pendingRemove.Store(nil)
	rec.add("file.removed", "cleanup", "Temporary file deleted", true, "")
	return residencyNotes, nil
}

// cacheDir returns $XDG_CACHE_HOME/machine-atlas (or ~/.cache/machine-atlas),
// creating it with mode 0700 when it does not exist.
func cacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("no cache directory: %w", err)
	}
	dir := filepath.Join(base, "machine-atlas")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create the cache directory: %w", noPath(err))
	}
	return dir, nil
}

// removeStale deletes temporary files whose run is no longer alive: a
// child killed at its deadline cannot run its deferred cleanup.
func removeStale(dir string) {
	matches, _ := filepath.Glob(filepath.Join(dir, filePrefix+"*"+fileSuffix))
	for _, m := range matches {
		pid, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(m), filePrefix), fileSuffix))
		if err != nil || pid <= 0 {
			continue
		}
		if errors.Is(unix.Kill(pid, 0), unix.ESRCH) {
			os.Remove(m)
		}
	}
}

// writeFile fills f with fileBytes of non-zero pseudo-random bytes from a
// fixed seed and fsyncs it, so the cached pages are clean and the advice
// can drop them.
func writeFile(f *os.File, buf []byte) error {
	rng := rand.New(rand.NewPCG(0x6d616368696e65, 0x61746c6173))
	for written := 0; written < fileBytes; written += len(buf) {
		for i := range buf {
			buf[i] = byte(rng.Uint32()) | 1
		}
		if _, err := f.Write(buf); err != nil {
			return fmt.Errorf("write the temporary file: %w", noPath(err))
		}
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("fsync: %w", noPath(err))
	}
	return nil
}

// residentPages maps the file read-only to ask mincore which of its pages
// are in the page cache. The mapping is never read and is removed at once,
// so it neither faults pages in nor pins them against the advice.
func residentPages(fd int, vec []byte) (int, error) {
	m, err := unix.Mmap(fd, 0, fileBytes, unix.PROT_READ, unix.MAP_SHARED)
	if err != nil {
		return 0, fmt.Errorf("mmap: %w", err)
	}
	defer unix.Munmap(m)
	// x/sys/unix has no mincore wrapper.
	_, _, errno := unix.Syscall(unix.SYS_MINCORE, uintptr(unsafe.Pointer(&m[0])), uintptr(len(m)), uintptr(unsafe.Pointer(&vec[0])))
	if errno != 0 {
		return 0, fmt.Errorf("mincore: %w", errno)
	}
	n := 0
	for _, v := range vec {
		n += int(v & 1)
	}
	return n, nil
}

// timedRead reads the whole file from the start with read(2) and returns
// how long it took.
func timedRead(fd int, buf []byte) (time.Duration, error) {
	if _, err := unix.Seek(fd, 0, 0); err != nil {
		return 0, fmt.Errorf("lseek: %w", err)
	}
	start := time.Now()
	total := 0
	for {
		n, err := unix.Read(fd, buf)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("read: %w", err)
		}
		if n == 0 {
			break
		}
		total += n
	}
	elapsed := time.Since(start)
	if total != fileBytes {
		return 0, fmt.Errorf("read %d bytes, want %d", total, fileBytes)
	}
	return elapsed, nil
}

// fsName names the common filesystems by their statfs magic number.
func fsName(magic uint32) string {
	switch magic {
	case unix.EXT4_SUPER_MAGIC: // shared by ext2, ext3 and ext4
		return "ext2/ext3/ext4"
	case unix.BTRFS_SUPER_MAGIC:
		return "btrfs"
	case unix.XFS_SUPER_MAGIC:
		return "xfs"
	case unix.F2FS_SUPER_MAGIC:
		return "f2fs"
	case unix.BCACHEFS_SUPER_MAGIC:
		return "bcachefs"
	case 0x2fc12fc1:
		return "zfs"
	case unix.TMPFS_MAGIC:
		return "tmpfs"
	case unix.RAMFS_MAGIC:
		return "ramfs"
	case unix.OVERLAYFS_SUPER_MAGIC:
		return "overlayfs"
	case 0xf15f:
		return "ecryptfs"
	case unix.FUSE_SUPER_MAGIC:
		return "fuse"
	case unix.NFS_SUPER_MAGIC:
		return "nfs"
	case unix.CIFS_SUPER_MAGIC, unix.SMB2_SUPER_MAGIC:
		return "cifs/smb"
	case unix.MSDOS_SUPER_MAGIC:
		return "vfat"
	case unix.EXFAT_SUPER_MAGIC:
		return "exfat"
	}
	return fmt.Sprintf("unknown (magic %#x)", magic)
}
