---
{
  "id": "filesystems-and-the-vfs",
  "title": "How a path becomes blocks on a disk",
  "subsystem": "storage",
  "kind": "core",
  "order": 4,
  "summary": "The virtual filesystem's superblocks, inodes, dentries and open files; filesystems that live on disks, in memory, over the network and in layers; how ext4, xfs and btrfs map file offsets to blocks; readahead's growing window; access-time updates and the mount options that change them.",
  "prerequisites": ["io-statistics"],
  "uses": [
    "derived.storage.root_atime",
    "derived.storage.root_disk",
    "derived.storage.root_fstype",
    "derived.storage.root_options",
    "derived.storage.root_read_ahead_bytes",
    "fs.types",
    "mounts.filesystems"
  ],
  "relevance": [
    {"when": "derived.storage.root_fstype", "is": "overlay", "note": "The root here is an overlay filesystem, the layered tree container runtimes build."},
    {"when": "derived.storage.root_atime", "is": "strictatime", "note": "The root is mounted without relatime or noatime, so every read updates an access time."}
  ],
  "next": ["writeback-and-durability"],
  "deeper": ["planned:mount-namespaces", "planned:slab-allocator"],
  "version_notes": [
    {"applies": "Linux ≥ 2.6.30", "text": "relatime became the default for mounts that do not choose an access-time policy; strictatime was added to ask for the old behaviour."},
    {"applies": "Linux ≥ 3.18", "text": "overlayfs was merged, the union filesystem container runtimes use for image layers."},
    {"applies": "Linux ≥ 4.0", "text": "lazytime keeps access, change and modification times in memory and writes them only with other inode changes, on fsync, or after a day."},
    {"applies": "Linux ≥ 5.18", "text": "Readahead was rewritten around folios (page_cache_ra_order): a sequential read can allocate large folios for the readahead window on filesystems that support them."}
  ],
  "references": [
    {"title": "Linux kernel documentation: Overview of the Linux Virtual File System", "url": "https://docs.kernel.org/filesystems/vfs.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Pathname lookup", "url": "https://docs.kernel.org/filesystems/path-lookup.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: ext4 data structures and algorithms (extents, delayed allocation)", "url": "https://docs.kernel.org/filesystems/ext4/index.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Overlay Filesystem", "url": "https://docs.kernel.org/filesystems/overlayfs.html", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: Tmpfs", "url": "https://docs.kernel.org/filesystems/tmpfs.html", "kind": "kernel-doc"},
    {"title": "Kernel source: mm/readahead.c (the readahead window)", "url": "https://github.com/torvalds/linux/blob/master/mm/readahead.c", "kind": "source"},
    {"title": "mount(8): filesystem-independent mount options", "url": "https://man7.org/linux/man-pages/man8/mount.8.html", "kind": "man-page"},
    {"title": "readahead(2)", "url": "https://man7.org/linux/man-pages/man2/readahead.2.html", "kind": "man-page"},
    {"title": "posix_fadvise(2)", "url": "https://man7.org/linux/man-pages/man2/posix_fadvise.2.html", "kind": "man-page"},
    {"title": "LWN: Improving readahead (2005, on-demand readahead's design)", "url": "https://lwn.net/Articles/155510/", "kind": "lwn"},
    {"title": "LWN: Introducing lazytime (2014)", "url": "https://lwn.net/Articles/621046/", "kind": "lwn"}
  ]
}
---
## Question

:::when derived.storage.root_fstype
On this machine / is a filesystem of type {{derived:derived.storage.root_fstype}}. A program that opens `/etc/hostname` and reads it never mentions ext4 or xfs or a disk; it names a path. Something turns that path into an object with an inode number, a size and permissions, maps an offset in it to a block on some device, and does all of this the same way whether the file is on a disk, in memory or on a server. What is that something, and what does each filesystem add underneath it?
:::unknown
A program that opens `/etc/hostname` and reads it never mentions ext4 or xfs or a disk; it names a path. Something turns that path into an object with an inode number, a size and permissions, maps an offset in it to a block on some device, and does all of this the same way whether the file is on a disk, in memory or on a server. What is that something, and what does each filesystem add underneath it? The type of / is {{derived:derived.storage.root_fstype}} here.
:::end

## Why it exists

Files have to live somewhere: on a disk with a journal, on flash with a log, in RAM, on a server, in the kernel's own state (`/proc`). Each place needs a different way of finding and storing data, and new ones keep appearing. Programs must not have to know which they are using, and the page cache, permissions, locking and `mmap` should not be written again for each one.

So the kernel splits the work. The *virtual filesystem* (VFS) owns everything common: path lookup, open files, permissions, the page cache, the system calls. A filesystem supplies a set of operations the VFS calls when it needs something only that filesystem knows, such as where block 7 of this file lives or how to create a directory entry. The mount table stitches every filesystem into one tree.

## How it works

### The VFS objects

- A **superblock** is one mounted filesystem instance: its type, its device (if any), its options, its root.
- An **inode** is one file: type, size, owner, permissions, timestamps, and an *address space*, the page cache index from file offset to folio (the [page cache lesson](lesson:page-cache)).
- A **dentry** links a name in a directory to an inode. Dentries are cached in a hash table, the *dcache*, which is why the second lookup of a path costs a few hash lookups and no disk access; a *negative* dentry remembers that a name does not exist.
- A **file** is one open instance: an inode, a position, the flags `open()` was given.

Path lookup walks the path one component at a time through the dcache, checking permission to search each directory, crossing into another filesystem where something is mounted, following symbolic links. Only a component missing from the dcache makes the filesystem read a directory from storage.

### Kinds of filesystem

`/proc/filesystems` lists the types this kernel can mount now; those marked `nodev` need no block device:

- **On a block device:** ext4, xfs, btrfs, f2fs, vfat. They read and write the device through the page cache and the block layer of the previous lessons.
- **In memory:** tmpfs keeps files only in the page cache (and swap). There is no device and nothing to write back; a tmpfs file's pages are shmem.
- **Layered:** overlayfs presents a writable *upper* directory over read-only *lower* ones; the first write to a lower file copies it up whole. Container runtimes build each container's root this way from image layers.
- **Remote and user space:** NFS, SMB and FUSE send operations over a network or to a process.
- **Pseudo:** proc, sysfs, cgroup2 generate their contents on each read.

A filesystem built as a module appears in `/proc/filesystems` only once loaded, which usually happens at the first mount.

### From file offset to disk block

ext4 and xfs describe a file's data as *extents*: runs of contiguous blocks, "file blocks 0 to 2047 are disk blocks 81,920 to 83,967". A large file written in one go needs a handful. Both use *delayed allocation*: `write()` only reserves space, and blocks are chosen at writeback, when the whole run of dirty data is known, so it can be laid out contiguously. btrfs never overwrites live data in place (*copy-on-write*): a changed block is written elsewhere and the tree pointing to it updated, which gives snapshots and checksums at the cost of fragmenting files that are rewritten in place. f2fs writes in a log, as flash prefers.

### Reading ahead

A cache miss costs a trip to the device, so the kernel reads more than it was asked for when access looks sequential. The first read of a file starts a window from the request size: for a small read, about four times the request; for a larger one, twice. Within that window one page is marked; when a later read reaches it, the next window is read asynchronously, before it is needed, and each new window is twice the last (four times while small) up to the device's maximum, `read_ahead_kb`. A program streaming a file therefore finds its data already arriving. A read that jumps elsewhere starts again with a small window, and random reads get almost none. Pages of an `mmap`ed file are read around the faulting page instead.

Programs can steer it: `posix_fadvise(POSIX_FADV_SEQUENTIAL)` doubles the window, `POSIX_FADV_RANDOM` turns readahead off for that file, and `readahead(2)` or `POSIX_FADV_WILLNEED` asks for a range to be read now.

### Access times and mount options

POSIX says reading a file updates its access time. Taken literally, every read becomes an inode write. `strictatime` does exactly that; `noatime` never updates it; `relatime`, the default since 2.6.30, updates it only when the access time is older than the last modification or change, or more than a day old, which is enough for tools that ask "was this read since it changed?". `lazytime` keeps all timestamp updates in memory until something else writes the inode.

Other options seen in the mount table: `ro` and `rw`; `sync` (every write waits for the device, very slow); `discard` (send TRIM as blocks are freed, rather than in periodic `fstrim` batches); ext4's `commit=N` (journal commit interval, 5 s by default) and `data=` (journaling mode, the subject of the [next lesson](lesson:writeback-and-durability)); btrfs's `compress=`. Options belong to the mount or to the superblock: a bind mount can be read-only while the same filesystem is writable elsewhere.

## On this machine

:::table mounts.filesystems columns=mount_point,fstype,source_kind,device,disk,options limit=40

:::evidence The root filesystem
- derived.storage.root_fstype
- derived.storage.root_options | Allow-listed options the kernel printed
- derived.storage.root_atime | When reads update access times
- derived.storage.root_disk
- derived.storage.root_read_ahead_bytes | Largest readahead window on that disk
:::end

Mount points outside the standard ones are shown as `<path>`, and only options from an allow-list are kept, so the table says how each filesystem behaves without saying whose machine this is.

:::when derived.storage.root_fstype is overlay
/ is an overlay: Atlas runs in a container whose root is the union of read-only image layers and one writable layer, all of them directories on a filesystem of the host. Reads of an unchanged file go to the lower layer's file and its page cache; the first write to it copies the whole file up into the writable layer. The bind mounts of `/etc/hosts`, `/etc/hostname` and `/etc/resolv.conf` come from the host's own filesystem, which is why they are the rows here with a block device.
:::else
:::when derived.storage.root_disk
/ is {{derived:derived.storage.root_fstype}} on {{derived:derived.storage.root_disk}}, so a cache miss under / goes through that filesystem's block mapping to that device (and from a device-mapper volume on to the disk beneath it), and sequential reads are prefetched in windows of up to {{derived:derived.storage.root_read_ahead_bytes}}.
:::unknown
/ is {{derived:derived.storage.root_fstype}}, but the disk beneath it is {{derived:derived.storage.root_disk}} here: the mount names a device, and `/sys` would say which disk it belongs to. Its readahead window is therefore not known either; 128 KiB is the common default.
:::end
:::unknown
The type of / is {{derived:derived.storage.root_fstype}} here: the mount table could not be read.
:::end

:::when derived.storage.root_atime is relatime
The root uses relatime: a read updates a file's access time only once after each change, or once a day, so reading files does not turn into a stream of inode writes.
:::else
:::when derived.storage.root_atime is noatime
The root uses noatime: reads never update access times. Nothing is written because a file was read; tools that check whether a file was read since it changed (some mail readers, `tmpwatch`) see no reads at all.
:::else
The root prints neither relatime nor noatime, which means strictatime: every read updates the file's access time in memory, and the inode has to be written back later. With `lazytime` that write is postponed for up to a day.
:::unknown
The access-time policy of / is {{derived:derived.storage.root_atime}} here.
:::end
:::unknown
The access-time policy of / is {{derived:derived.storage.root_atime}} here.
:::end

:::table fs.types columns=name,nodev limit=60

## Predict

:::predict
A monitoring script reads the same 1 KiB file every minute for an hour; nothing else touches it, and it was last modified last week. How many times is the file's access time written to disk?

- 60 times, once per read.
- Once at most: the first read after the change updates it, later ones do not.
- Never: a read cannot cause a write.
:::reveal
It depends on the access-time policy, and each answer is right for one of them.

- With `strictatime`, every read updates the access time in memory, and writeback writes the inode out every time it is dirty and old enough: up to 60 inode writes, fewer if several updates fall into one writeback interval, and at most one a day with `lazytime`.
- With `relatime`, the first read updates it, because the stored access time is older than a day; the next 59 do not, because it is now newer than the modification time and less than a day old. One inode write.
- With `noatime`, none.

:::when derived.storage.root_atime
For files under / on this machine the policy is {{derived:derived.storage.root_atime}}.
:::unknown
This machine's policy for / is {{derived:derived.storage.root_atime}} here.
:::end

The last answer was true of no Unix until noatime was invented: reads have always been able to cause writes, which is why relatime became the default.
:::end

## Try it

:::experiment page-cache-residency

The experiment writes a 16 MiB file in Atlas's cache directory, drops its pages from the page cache, and times a cold read and a warm one. It reports the filesystem type of that directory. On a disk filesystem the cold read is the whole path of this track: the VFS finds the inode, readahead issues windows growing towards `read_ahead_kb`, the filesystem maps them through its extents, and the block layer sends requests of up to `max_sectors_kb` to the disk's queue; the warm read is a copy from memory. On tmpfs the file was never on a device, the drop does nothing, and both reads are copies. Comparing the cold read's time with the disk's average read time in the [statistics lesson](lesson:io-statistics) shows how much readahead keeps the device busy with large, sequential requests.

Without the experiment, read-only commands show the same structures:

```sh
findmnt -o TARGET,FSTYPE,SOURCE,OPTIONS /     # how / is mounted
stat /etc/hostname                            # inode number, size, blocks, access/modify/change times
cat /proc/filesystems
filefrag -v /usr/bin/ls                       # the extents of one file (ext4, xfs, btrfs)
```

`filefrag` shows a small, unchanged file as one extent; a large file written over months on a full filesystem, or a database file on btrfs, as many.

## Limits

:::misconception tmpfs is a RAM disk
A RAM disk is a block device in memory with a filesystem on top, so data sits in RAM twice: the device's pages and the page cache. tmpfs has no device; its files are page cache pages that can never be written back, only swapped. It grows and shrinks with its contents, up to its size limit.
:::end

:::misconception noatime makes reads faster
A read that hits the cache is the same speed either way. What noatime saves is the inode writes that strictatime causes later, which matters for flash wear and for filesystems on slow devices; with relatime most of that saving is already there.
:::end

:::misconception Every filesystem on a disk is a block device mount
In the table, overlay, tmpfs, network and FUSE mounts have no device. A container's root and its `/dev` are such mounts, and their data lives in a directory of the host, in memory, or on a server: none of the block device counters of the earlier lessons describe it directly.
:::end

:::cannot
Paths, subvolume names and the sources of non-standard mounts: Machine Atlas replaces them by `<path>` or leaves them out, so it cannot say which filesystem holds a given directory outside the standard ones.

How full each filesystem is, its features (ext4 journal size, xfs log, btrfs profiles) and its fragmentation: those need `statfs`, `dumpe2fs` or filesystem-specific tools, which Machine Atlas does not run.

Other mount namespaces: another container, or a process that unshared its mounts, sees a different tree.
:::end

## Go deeper

Next, [when written data is safe](lesson:writeback-and-durability) follows writes from dirty pages through the filesystem's journal to the device's cache. In Explore, [filesystems](component:filesystems) lists every mount found here. Planned lessons cover mounts and mount namespaces, how a container gets a tree of its own, and the slab allocator, where the dentry and inode caches live.
