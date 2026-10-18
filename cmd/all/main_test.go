// Tests for the single-container launcher's supervision logic.
//
// These exercise the two ways the container stops: a child dying on its own
// (restart-worthy) and a stop signal (clean exit). Real grandchildren are used
// because the whole point of the code is process lifecycle handling.
package main

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// startChild launches a command and returns it once it is running.
func startChild(t *testing.T, name string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", name, err)
	}
	return cmd
}

// stopped reports whether a child has been reaped, i.e. is no longer running.
// It deliberately does not use ProcessState.Exited(), which is false for a
// process that was terminated by a signal — exactly how these children die.
func stopped(cmd *exec.Cmd) bool {
	return cmd.ProcessState != nil
}

func TestSuperviseRestartsWhenAChildDies(t *testing.T) {
	survivor := startChild(t, "sleep", "60")
	shortLived := startChild(t, "true")

	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	go func() { done <- supervise([]*exec.Cmd{survivor, shortLived}, sigs, 5*time.Second) }()

	select {
	case exitedBadly := <-done:
		if !exitedBadly {
			t.Error("supervise() = false, want true when a child exits on its own")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("supervise did not return after a child died")
	}

	// The survivor must have been stopped too — otherwise the container would
	// leak a running API with no worker behind it.
	if !stopped(survivor) {
		t.Error("survivor was not terminated after its sibling exited")
	}
	if !stopped(shortLived) {
		t.Error("exited child was not reaped")
	}
}

func TestSuperviseStopsCleanlyOnSignal(t *testing.T) {
	first := startChild(t, "sleep", "60")
	second := startChild(t, "sleep", "60")

	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	go func() { done <- supervise([]*exec.Cmd{first, second}, sigs, 5*time.Second) }()

	// The buffer holds this until supervise is ready to read it.
	sigs <- syscall.SIGTERM

	select {
	case exitedBadly := <-done:
		if exitedBadly {
			t.Error("supervise() = true, want false for a signal-initiated shutdown")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("supervise did not return after a stop signal")
	}

	for i, cmd := range []*exec.Cmd{first, second} {
		if !stopped(cmd) {
			t.Errorf("child %d was still running after a stop signal", i)
		}
	}
}

func TestSuperviseKillsChildrenThatIgnoreTheStopSignal(t *testing.T) {
	// A child that traps SIGTERM never exits on its own, so the launcher has to
	// escalate to SIGKILL once the grace period lapses.
	script := "trap \"\" TERM; exec sleep 60"
	child := startChild(t, "sh", "-c", script)

	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	grace := 300 * time.Millisecond
	go func() { done <- supervise([]*exec.Cmd{child}, sigs, grace) }()

	sigs <- syscall.SIGTERM

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("supervise did not escalate to SIGKILL within the grace period")
	}

	if !stopped(child) {
		t.Error("child that ignored SIGTERM was not killed")
	}
}
