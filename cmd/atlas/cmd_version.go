package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/ZahakJ/machine-atlas/internal/snapshot"
	"github.com/ZahakJ/machine-atlas/internal/version"
)

func cmdVersion(args []string) int {
	if len(args) > 0 {
		fmt.Fprintln(os.Stderr, "Usage: atlas version")
		return 2
	}
	fmt.Printf("atlas %s\nsnapshot schema %d\n%s %s/%s\n",
		version.Version, snapshot.SchemaVersion, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return 0
}
