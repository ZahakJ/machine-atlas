// Package system holds the collectors for everything around memory that the
// lessons and the overview need: OS release, hostname, kernel, CPU topology
// and flags, init, virtualisation evidence, block devices, mounts and the
// session type (ARCHITECTURE.md §4, "System"). Network interfaces are in
// internal/collect/network.
//
// Each collector reads through collect.Env, so the same code runs against
// the live machine and against fixture roots. Parsers are pure functions of
// the bytes read; they reject what they do not understand so a format
// change surfaces as a failed observation, not a wrong value.
//
// Privacy is enforced here, at collection, not only at export: DMI serials
// and UUIDs and process environments are never opened, mount points
// outside a standard list are replaced, and UUIDs embedded in device names
// are removed.
package system
