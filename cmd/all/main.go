// cmd/all/main.go — single-container launcher for the API and the job worker.
//
// The reference topology runs the API and the worker as separate services
// (Cloud Run services, or `docker compose` services locally). Some free-tier
// container hosts only offer one always-on process and no free background
// worker, and this image's runtime stage is distroless, so there is no shell to
// chain the two binaries with `sh -c "… & …"`. This launcher fills that gap:
//
//	/fcrt-all
//
// It starts the worker and then the API as child processes, hands them the
// container's stdout/stderr so their structured JSON logs reach the platform
// log stream interleaved, forwards SIGTERM/SIGINT to both for a graceful stop,
// and exits non-zero if either child dies unexpectedly so the platform can
// restart the container.
//
// This is a deployment adapter, not a replacement for the two-service topology:
// use `docker run … /fcrt-api` and `/fcrt-worker` (or two platform services)
// wherever a free background worker is available.
package main

import (
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

// shutdownGrace bounds how long we wait for children to drain after a stop
// signal before force-killing them. The API's own graceful shutdown allows 15s.
const shutdownGrace = 20 * time.Second

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel(),
	})))

	// Worker first: it should already be polling by the time the API can accept
	// a request that enqueues work.
	roles := []struct {
		label  string
		envKey string
		defval string
	}{
		{"worker", "WORKER_BINARY", "/fcrt-worker"},
		{"api", "API_BINARY", "/fcrt-api"},
	}

	children := make([]*exec.Cmd, 0, len(roles))
	for _, r := range roles {
		path := envOrDefault(r.envKey, r.defval)
		cmd := exec.Command(path)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			// Nothing worth keeping alive if one half cannot start.
			slog.Error("failed to start child process", "role", r.label, "path", path, "err", err)
			terminate(children, syscall.SIGTERM)
			os.Exit(1)
		}
		slog.Info("child process started", "role", r.label, "path", path, "pid", cmd.Process.Pid)
		children = append(children, cmd)
	}

	slog.Info("launcher ready", "children", len(children))

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigs)

	if supervisedBadly := supervise(children, sigs, shutdownGrace); supervisedBadly {
		slog.Error("container exiting because a child process failed")
		os.Exit(1)
	}
	slog.Info("all child processes stopped")
}

// supervise waits for a stop signal or for any child to exit, then stops the
// remaining children. It waits up to grace for them to drain before killing
// them, and reports whether a child exited on its own (i.e. the container
// should be restarted).
//
// It returns only once every child has been reaped or killed, so callers can
// treat the return as "the container is quiesced". The signal channel is a
// parameter so the shutdown paths can be exercised without real signals.
func supervise(children []*exec.Cmd, sigs <-chan os.Signal, grace time.Duration) (exitedBadly bool) {
	if len(children) == 0 {
		return false
	}

	// Buffered for every child so reaping a child never blocks, even when we are
	// no longer reading the channel.
	exited := make(chan *exec.Cmd, len(children))
	for _, cmd := range children {
		go func(c *exec.Cmd) {
			_ = c.Wait()
			exited <- c
		}(cmd)
	}

	reaped := 0
	select {
	case sig := <-sigs:
		slog.Info("stop signal received, terminating children", "signal", sig.String())
	case cmd := <-exited:
		reaped++
		exitedBadly = true
		slog.Error("child process exited, shutting down the container", "path", cmd.Path)
	}

	// Ask every child to stop; the ones that already exited ignore this.
	terminate(children, syscall.SIGTERM)

	// Wait for a clean exit, then force-kill whatever is left.
	deadline := time.After(grace)
	for reaped < len(children) {
		select {
		case <-exited:
			reaped++
		case <-deadline:
			slog.Warn("graceful shutdown timed out, killing remaining children")
			terminate(children, syscall.SIGKILL)
			// Reap the stragglers so no zombies outlive the container.
			for reaped < len(children) {
				<-exited
				reaped++
			}
			return true
		}
	}
	return exitedBadly
}

// terminate signals every child that is still running. A child that has already
// exited returns an error here, which is expected and ignored.
func terminate(children []*exec.Cmd, sig syscall.Signal) {
	for _, cmd := range children {
		if cmd.Process == nil {
			continue
		}
		_ = cmd.Process.Signal(sig)
	}
}

func logLevel() slog.Level {
	switch os.Getenv("LOG_LEVEL") {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
