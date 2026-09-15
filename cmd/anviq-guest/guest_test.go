package main

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/Leovoss/anviq-microvm-engine/internal/proto"
)

// Exercise the guest's real exec-and-stream path over an in-memory pipe, with no
// vsock and no KVM. This is the guest half of the smoke test, runnable in CI.
func TestGuestExecStreamsOutputAndExit(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	go serve(server) // serve closes its side when done

	if err := proto.WriteRequest(client, proto.Request{
		Op:   proto.OpExec,
		Argv: []string{"printf", "hello-anviq"},
	}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	var stdout strings.Builder
	var exit *int
	err := proto.DecodeEvents(client, func(ev proto.ProcessEvent) error {
		switch ev.Type {
		case "stdout":
			stdout.WriteString(ev.Data)
		case "exit":
			exit = ev.Code
		}
		return nil
	})
	if err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if got := stdout.String(); got != "hello-anviq" {
		t.Fatalf("stdout = %q, want %q", got, "hello-anviq")
	}
	if exit == nil || *exit != 0 {
		t.Fatalf("exit = %v, want 0", exit)
	}
}

// A failing command must surface a non-zero exit code, not hang or drop it.
func TestGuestExecPropagatesNonZeroExit(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	go serve(server)

	if err := proto.WriteRequest(client, proto.Request{Op: proto.OpExec, Argv: []string{"false"}}); err != nil {
		t.Fatalf("write request: %v", err)
	}
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))

	var exit *int
	if err := proto.DecodeEvents(client, func(ev proto.ProcessEvent) error {
		if ev.Type == "exit" {
			exit = ev.Code
		}
		return nil
	}); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if exit == nil || *exit == 0 {
		t.Fatalf("expected non-zero exit, got %v", exit)
	}
}
