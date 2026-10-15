package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/ZahakJ/machine-atlas/internal/server"
	"github.com/ZahakJ/machine-atlas/internal/snapshot"
	"github.com/ZahakJ/machine-atlas/internal/version"
)

// fallbackFixture is shown where this machine cannot be collected (a
// non-Linux build) and no snapshot was named.
const fallbackFixture = "desktop-x86_64"

func cmdServe(args []string) int {
	fs := newFlags("serve", "atlas serve [--addr 127.0.0.1:7373] [--snapshot FILE | --fixture NAME] [--no-probe] [--open] [--allow-remote]")
	addr := fs.String("addr", "127.0.0.1:7373", "listen on `host:port`; the host must be a loopback address unless --allow-remote is given")
	file := fs.String("snapshot", "", "show the snapshot `FILE` instead of collecting this machine")
	fixture := fs.String("fixture", "", "show the embedded demonstration snapshot `NAME` (see atlas fixtures list)")
	noProbe := fs.Bool("no-probe", false, "do not start the probe child process when collecting")
	open := fs.Bool("open", false, "open the interface in the default browser (xdg-open)")
	allowRemote := fs.Bool("allow-remote", false, "allow a non-loopback --addr: anyone who can reach it can read this machine's snapshot")
	pos, code := parseArgs(fs, args)
	if code >= 0 {
		return code
	}
	if len(pos) > 0 {
		return usageError(fs, "unexpected argument %q", pos[0])
	}
	if *file != "" && *fixture != "" {
		return usageError(fs, "--snapshot and --fixture are alternatives")
	}
	host, _, err := net.SplitHostPort(*addr)
	if err != nil {
		return usageError(fs, "--addr: %v", err)
	}
	if !loopbackHost(host) {
		if !*allowRemote {
			return usageError(fs, "refusing to listen on %q: it is not a loopback address, so other computers could read this machine's snapshot (add --allow-remote if that is really intended)", *addr)
		}
		fmt.Fprintf(os.Stderr, "warning: listening on %s, which is reachable from other computers; anyone who can connect can read this machine's snapshot\n", *addr)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opt := server.Options{AllowRemote: *allowRemote}
	// Collection, the probe and experiments are Linux-only (ARCHITECTURE.md §2);
	// elsewhere the server shows snapshots and fixtures.
	if runtime.GOOS == "linux" {
		opt.Collect = func(ctx context.Context) (*snapshot.Snapshot, error) {
			return collectSnapshot(ctx, "", time.Time{}, *noProbe)
		}
	}
	switch {
	case *file != "":
		opt.Mode, opt.Origin = server.ModeSnapshot, "file"
		opt.Snapshot, err = loadFile(*file)
	case *fixture != "" || opt.Collect == nil:
		name := *fixture
		if name == "" {
			name = fallbackFixture
			fmt.Fprintf(os.Stderr, "this build cannot collect from a %s machine; showing the fixture %s\n", runtime.GOOS, name)
		}
		opt.Mode, opt.Origin, opt.Fixture = server.ModeFixture, "fixture", name
		opt.Snapshot, err = loadFixture(name)
	default:
		fmt.Fprintln(os.Stderr, "collecting this machine...")
		opt.Mode, opt.Origin = server.ModeLive, "collection"
		opt.Snapshot, err = opt.Collect(ctx)
	}
	if err != nil {
		return fail("serve", err)
	}

	ln, err := net.Listen("tcp", *addr)
	if errors.Is(err, syscall.EADDRINUSE) {
		return fail("serve", fmt.Errorf("%s is already in use (is another atlas serve running?); choose another port with --addr", *addr))
	}
	if err != nil {
		return fail("serve", err)
	}
	opt.Port = ln.Addr().(*net.TCPAddr).Port
	srv, err := server.New(opt)
	if err != nil {
		ln.Close()
		return fail("serve", err)
	}
	hs := srv.HTTPServer()

	url := "http://" + net.JoinHostPort(urlHost(host), strconv.Itoa(opt.Port)) + "/"
	fmt.Printf("Machine Atlas %s\nshowing %s\nserving %s (Ctrl+C to stop)\n", version.Version, describeServeMode(opt), url)
	if dir := os.Getenv("ATLAS_WEB_DIR"); dir != "" {
		fmt.Printf("web files are read from %s (ATLAS_WEB_DIR)\n", dir)
	}
	if *open {
		openBrowser(url)
	}

	served := make(chan error, 1)
	go func() { served <- hs.Serve(ln) }()
	select {
	case err := <-served:
		return fail("serve", err)
	case <-ctx.Done():
	}
	stop() // a second Ctrl+C now kills the process outright
	fmt.Println("\nstopping")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := hs.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fail("serve", err)
	}
	return 0
}

// loopbackHost reports whether a listen host keeps the server on this
// computer. An empty host or 0.0.0.0 means every interface.
func loopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}

// urlHost is the host to print in the URL: a wildcard listen address is
// reachable as 127.0.0.1 from this computer.
func urlHost(host string) string {
	ip, err := netip.ParseAddr(host)
	if host == "" || (err == nil && ip.IsUnspecified()) {
		return "127.0.0.1"
	}
	return host
}

func describeServeMode(opt server.Options) string {
	switch opt.Mode {
	case server.ModeLive:
		return "this machine, collected at " + opt.Snapshot.CreatedAt
	case server.ModeFixture:
		return "the demonstration fixture " + strconv.Quote(opt.Fixture) + " (not your machine)"
	}
	return "a snapshot file captured at " + opt.Snapshot.CreatedAt + " (not this machine's current state)"
}

// openBrowser asks the desktop to open url. Failing to is not an error: the
// URL has been printed.
func openBrowser(url string) {
	cmd := exec.Command("xdg-open", url)
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "could not open a browser (%v); open the URL above yourself\n", err)
		return
	}
	go cmd.Wait() // reap it; xdg-open exits once the browser has the URL
}
