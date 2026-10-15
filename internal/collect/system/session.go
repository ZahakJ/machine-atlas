package system

import (
	"context"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// sessionVars maps session ids to the allow-listed variables they read.
// For the display variables only presence is recorded: their values are
// socket names that can embed a user id.
var sessionVars = []struct {
	id, name string
	presence bool
}{
	{"session.type", "XDG_SESSION_TYPE", false},
	{"session.desktop", "XDG_CURRENT_DESKTOP", false},
	{"session.wayland_display_set", "WAYLAND_DISPLAY", true},
	{"session.x11_display_set", "DISPLAY", true},
}

var sessionCollector = collector{
	id:   "system.session",
	owns: []string{"session.type", "session.desktop", "session.wayland_display_set", "session.x11_display_set"},
	run: func(_ context.Context, env collect.Env) []obs.Observation {
		var out []obs.Observation
		for _, v := range sessionVars {
			src := obs.File("/proc/self/environ", v.name)
			src.Detail = "atlas's own environment, read with getenv; only allow-listed names"
			val, ok := env.Getenv(v.name)
			switch {
			case v.presence:
				src.Detail += "; only whether it is set is kept"
				out = append(out, obs.OK(v.id, ok && val != "", src))
			case !ok || val == "":
				out = append(out, obs.Absent(v.id, src, v.name+" is not set in atlas's environment (typical over ssh, in a service or in a container)"))
			default:
				out = append(out, obs.OK(v.id, val, src))
			}
		}
		return out
	},
}
