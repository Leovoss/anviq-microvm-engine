// Command anviq-guest runs INSIDE each microVM as an init-time service. It listens
// on AF_VSOCK and serves the host control plane's exec and file operations.
//
// Build static so it runs on a minimal rootfs regardless of libc:
//
//	CGO_ENABLED=0 GOOS=linux go build -o anviq-guest ./cmd/anviq-guest
//
// Then install it into the base rootfs at /usr/local/bin/anviq-guest and start it
// at boot (see internal/guest/README.md). It is Linux-only by construction.
package main

import (
	"bufio"
	"encoding/json"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/Leovoss/anviq-microvm-engine/internal/proto"
	"golang.org/x/sys/unix"
)

func main() {
	ln, err := listenVsock(proto.GuestPort)
	if err != nil {
		log.Fatalf("anviq-guest: listen vsock: %v", err)
	}
	log.Printf("anviq-guest: listening on vsock port %d", proto.GuestPort)
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("anviq-guest: accept: %v", err)
			continue
		}
		go serve(conn)
	}
}

// listenVsock binds an AF_VSOCK stream listener on the given port for any CID.
// Uses golang.org/x/sys/unix directly so the guest needs no third-party vsock lib.
func listenVsock(port uint32) (net.Listener, error) {
	fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	if err := unix.Bind(fd, &unix.SockaddrVM{CID: unix.VMADDR_CID_ANY, Port: port}); err != nil {
		unix.Close(fd)
		return nil, err
	}
	if err := unix.Listen(fd, 16); err != nil {
		unix.Close(fd)
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "vsock")
	defer f.Close()
	return net.FileListener(f)
}

func serve(conn net.Conn) {
	defer conn.Close()
	req, err := proto.ReadRequest(bufio.NewReader(conn))
	if err != nil {
		return
	}
	switch req.Op {
	case proto.OpExec:
		runExec(conn, req)
	case proto.OpList:
		runList(conn, req)
	case proto.OpRead:
		runRead(conn, req)
	case proto.OpWrite:
		runWrite(conn, req)
	default:
		out := proto.NewEventStream(conn)
		_ = out.Emit(proto.StderrEvent("unknown op: " + req.Op + "\n"))
		_ = out.Emit(proto.ExitEvent(127))
	}
}

// runExec runs the command and streams stdout/stderr, then a terminal exit event.
func runExec(conn net.Conn, req proto.Request) {
	out := proto.NewEventStream(conn)
	var mu sync.Mutex // serialize concurrent stdout/stderr emits onto one stream
	emit := func(ev proto.ProcessEvent) {
		mu.Lock()
		_ = out.Emit(ev)
		mu.Unlock()
	}

	cmd := exec.Command(req.Argv[0], req.Argv[1:]...)
	if req.Cwd != "" {
		cmd.Dir = req.Cwd
	}
	cmd.Env = os.Environ()
	for k, v := range req.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		emit(proto.StderrEvent(err.Error() + "\n"))
		emit(proto.ExitEvent(126))
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		emit(proto.StderrEvent(err.Error() + "\n"))
		emit(proto.ExitEvent(126))
		return
	}
	if err := cmd.Start(); err != nil {
		emit(proto.StderrEvent(err.Error() + "\n"))
		emit(proto.ExitEvent(127))
		return
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go pump(stdout, func(s string) { emit(proto.StdoutEvent(s)) }, &wg)
	go pump(stderr, func(s string) { emit(proto.StderrEvent(s)) }, &wg)
	wg.Wait()

	code := 0
	if err := cmd.Wait(); err != nil {
		code = 1
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
	}
	emit(proto.ExitEvent(code))
}

// pump forwards a pipe to sink in chunks as they arrive.
func pump(r interface{ Read([]byte) (int, error) }, sink func(string), wg *sync.WaitGroup) {
	defer wg.Done()
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			sink(string(buf[:n]))
		}
		if err != nil {
			return
		}
	}
}

func runList(conn net.Conn, req proto.Request) {
	entries := []proto.FileEntry{}
	des, err := os.ReadDir(req.Path)
	if err == nil {
		for _, de := range des {
			info, _ := de.Info()
			kind := "file"
			if de.IsDir() {
				kind = "dir"
			}
			var size int64
			if info != nil {
				size = info.Size()
			}
			entries = append(entries, proto.FileEntry{
				Path: filepath.Join(req.Path, de.Name()),
				Kind: kind,
				Size: size,
			})
		}
	}
	_ = json.NewEncoder(conn).Encode(entries)
}

func runRead(conn net.Conn, req proto.Request) {
	f, err := os.Open(req.Path)
	if err != nil {
		return // empty body signals failure; host treats a read error as empty
	}
	defer f.Close()
	buf := make([]byte, 32*1024)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			_, _ = conn.Write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func runWrite(conn net.Conn, req proto.Request) {
	ack := func(ok bool, msg string) {
		_ = json.NewEncoder(conn).Encode(map[string]any{"ok": ok, "error": msg})
	}
	if err := os.MkdirAll(filepath.Dir(req.Path), 0o755); err != nil {
		ack(false, err.Error())
		return
	}
	if err := os.WriteFile(req.Path, req.Content, 0o644); err != nil {
		ack(false, err.Error())
		return
	}
	ack(true, "")
}
