package derive

import (
	"fmt"
	"math/bits"
	"slices"
	"strconv"
	"strings"
)

// entryBytes is the size of one page-table entry in every 64-bit format
// modelled here (x86-64, arm64 VMSAv8-64, RISC-V Sv39/48/57).
const entryBytes = 8

// paging is the inferred page-table format: how wide virtual addresses are,
// how many levels translate them, and which simulator profile matches.
type paging struct {
	c        *calc
	levels   int
	vaBits   int
	userBits int    // log2 of the user address range
	profile  string // "" when the simulator has no matching profile
	rule     string
}

// simulatorProfiles are the paging profiles web/js/sim/translation-walker
// implements.
var simulatorProfiles = []string{
	"x86_64-4level", "x86_64-5level", "arm64-4k-48", "arm64-4k-39", "arm64-16k-47",
	"arm64-16k-48", "arm64-64k-48", "riscv-sv39", "riscv-sv48",
}

func pagingFacts(in inputs) []Fact {
	p := inferPaging(in)
	profile := p.profile
	pc := p.c.clone()
	if !pc.failed() && profile == "" {
		pc.fail("the translation simulator has no profile for %d-bit addresses with %d levels on this architecture", p.vaBits, p.levels)
	}
	var user any
	if p.userBits > 0 && p.userBits < 63 {
		user = int64(1) << p.userBits
	}
	// An undetermined width is not a width of 0: the rule then states the
	// formula without numbers.
	userRule := "2^(user address bits) bytes: the user part of the virtual address space (" + p.rule + ")"
	if !p.c.failed() {
		userRule = fmt.Sprintf("2^%d bytes: the user part of a %d-bit address space (%s)", p.userBits, p.vaBits, p.rule)
	}
	return append(pageSizeFacts(in),
		p.c.fact("derived.paging.levels", p.rule, int64(p.levels)),
		p.c.fact("derived.paging.va_bits", p.rule, int64(p.vaBits)),
		p.c.fact("derived.paging.user_space_bytes", userRule, user),
		pc.fact("derived.paging.profile", p.rule, profile),
	)
}

// pageSizeFacts are the facts that follow from the base page size alone.
func pageSizeFacts(in inputs) []Fact {
	c := in.calc()
	ps := c.int("kernel.page_size_bytes")
	// Base pages on the architectures Linux runs are 4 to 64 KiB; outside
	// that range the spans below would be meaningless or overflow.
	if !c.failed() && (ps < 4096 || ps > 65536 || ps&(ps-1) != 0) {
		c.fail("kernel.page_size_bytes %d is not a power of two from 4 KiB to 64 KiB", ps)
	}
	var offset, index, leafSpan, upperSpan any
	if !c.failed() {
		o := bits.TrailingZeros64(uint64(ps))
		i := o - 3 // log2(page size / 8-byte entries)
		offset, index = int64(o), int64(i)
		leafSpan = int64(1) << (o + i)
		upperSpan = int64(1) << (o + 2*i)
	}
	return []Fact{
		c.fact("derived.paging.offset_bits", "log2(kernel.page_size_bytes)", offset),
		c.fact("derived.paging.index_bits", "log2(kernel.page_size_bytes / 8): a table fills one page with 8-byte entries", index),
		c.fact("derived.paging.leaf_table_span_bytes", "(page size / 8) entries × page size", leafSpan),
		c.fact("derived.paging.upper_entry_span_bytes", "(page size / 8)² entries × page size", upperSpan),
	}
}

func inferPaging(in inputs) paging {
	c := in.calc()
	p := paging{c: c}
	arch := c.str("kernel.arch")
	if c.failed() {
		p.rule = "from kernel.arch and the architecture's rules"
		return p
	}
	switch familyOf(arch) {
	case "x86":
		x86Paging(&p, arch)
	case "arm":
		probedPaging(&p, arch)
	case "riscv":
		probedPaging(&p, arch)
	default:
		c.fail("the paging format of %q is not modelled", arch)
		p.rule = "x86-64, arm64 and riscv64 are modelled"
	}
	return p
}

// x86Paging: 5 levels and 57-bit addresses if /proc/cpuinfo lists la57,
// otherwise 4 levels and 48 bits. The flag describes the running kernel,
// not only the CPU: since Linux 4.17 early_identify_cpu() in
// arch/x86/kernel/cpu/common.c clears X86_FEATURE_LA57 whenever 5-level
// paging is not enabled (a kernel built without it, booted with no5lvl, or
// a CPU without LA57). The CPU's reported virtual address bits come from
// CPUID and say nothing about the kernel, so they are not used.
func x86Paging(p *paging, arch string) {
	c := p.c
	if arch != "x86_64" && arch != "amd64" {
		c.fail("32-bit x86 paging (%s) is not modelled", arch)
		p.rule = "only x86_64 is modelled"
		return
	}
	p.levels, p.vaBits = 4, 48
	la57 := c.bool("cpu.flags.la57")
	const why = "since Linux 4.17 the kernel clears la57 from the CPU flags at boot unless it enabled 5-level paging (arch/x86/kernel/cpu/common.c, early_identify_cpu), so the flag's presence is the kernel's choice, not only the CPU's ability"
	switch {
	case c.failed():
		p.rule = "x86_64: 5 levels if /proc/cpuinfo lists la57, otherwise 4; " + why
	case la57:
		p.levels, p.vaBits = 5, 57
		p.rule = "x86_64 with la57 listed in /proc/cpuinfo: 5-level paging with 57-bit virtual addresses (9+9+9+9+9 index bits above a 12-bit offset); " + why
	default:
		p.rule = "x86_64 without la57 in /proc/cpuinfo: 4-level paging with 48-bit virtual addresses (9+9+9+9 index bits above a 12-bit offset), whatever the CPU itself supports; " + why
	}
	p.userBits = p.vaBits - 1
	if p.vaBits == 57 {
		p.profile = "x86_64-5level"
	} else {
		p.profile = "x86_64-4level"
	}
}

// vaWidths lists, per architecture and page size, the virtual address
// widths Linux can be configured with, narrowest first. 52-bit
// configurations are left out: such kernels keep user mappings below 2^48
// unless a program asks for more, so the probe cannot tell them apart.
var vaWidths = map[string]map[int64][]int{
	"arm": {4096: {39, 48}, 16384: {36, 47, 48}, 65536: {42, 48}},
	// RISC-V Sv57 kernels likewise keep default mappings within Sv48.
	"riscv": {4096: {39, 48}},
}

// probedPaging handles arm64 and RISC-V, which publish no virtual address
// width to unprivileged programs. The kernel places the stack just below the
// top of user space, so the highest address the probe saw fits under the
// configured width and not under the next narrower one.
func probedPaging(p *paging, arch string) {
	c := p.c
	family := familyOf(arch)
	if arch != "aarch64" && arch != "arm64" && arch != "riscv64" {
		c.fail("32-bit paging (%s) is not modelled", arch)
		p.rule = "only aarch64 and riscv64 are modelled"
		return
	}
	ps := c.int("kernel.page_size_bytes")
	hiText := c.str("probe.maps.highest_end")
	p.rule = arch + ": the configured virtual address width is not reported, so it is inferred from the page size and the highest user address the probe mapped"
	if c.failed() {
		return
	}
	widths, ok := vaWidths[family][ps]
	if !ok {
		c.fail("%s with %d-byte pages is not modelled", arch, ps)
		return
	}
	hi, err := strconv.ParseUint(strings.TrimPrefix(hiText, "0x"), 16, 64)
	if err != nil {
		c.fail("probe.maps.highest_end %q is not a hex address", hiText)
		return
	}
	// arm64 user space is the whole TTBR0 range, 2^va; RISC-V splits one
	// sign-extended range, so user space is its lower half, 2^(va-1).
	half := 0
	if family == "riscv" {
		half = 1
	}
	offset := bits.TrailingZeros64(uint64(ps))
	index := offset - 3
	for _, w := range widths {
		if hi > uint64(1)<<(w-half) {
			continue
		}
		p.vaBits, p.userBits = w, w-half
		p.levels = (w - offset + index - 1) / index // ceil((va − offset) / index bits)
		p.rule = fmt.Sprintf("%s with %d KiB pages: virtual address bits are not reported unprivileged, so they are inferred from the highest user address the probe mapped (%s), which lies within 2^%d; of the widths Linux offers here (%s) the narrowest that fits is %d bits, and (%d − %d offset bits) / %d index bits per level gives %d levels. A kernel configured wider (52-bit arm64, Sv57) keeps user mappings within this range by default and reads the same",
			arch, ps/1024, hiText, p.userBits, joinInts(widths), w, w, offset, index, p.levels)
		p.profile = profileFor(arch, ps, w)
		return
	}
	c.fail("probe.maps.highest_end %s is above every user address width modelled for %s", hiText, arch)
}

func profileFor(arch string, pageSize int64, va int) string {
	var id string
	if arch == "riscv64" {
		id = fmt.Sprintf("riscv-sv%d", va)
	} else {
		id = fmt.Sprintf("arm64-%dk-%d", pageSize/1024, va)
	}
	if slices.Contains(simulatorProfiles, id) {
		return id
	}
	return ""
}

func joinInts(xs []int) string {
	s := make([]string, len(xs))
	for i, x := range xs {
		s[i] = strconv.Itoa(x)
	}
	return strings.Join(s, ", ")
}
