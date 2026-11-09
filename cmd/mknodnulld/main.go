package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func allowed(path string) bool {
	path = filepath.Clean(path)
	base := filepath.Base(path)
	for _, r := range []string{"/srv/hustoj", "/home/judge"} {
		if strings.HasPrefix(path, r+string(filepath.Separator)) {
			if base == "null" && filepath.Base(filepath.Dir(path)) == "dev" {
				return true
			}
			if base == "proc" {
				return true
			}
		}
	}
	return false
}

func main() {
	sock := os.Args[1]
	if sock == "" {
		sock = "/run/hustoj-null.sock"
	}
	_ = os.Remove(sock)
	l, err := net.Listen("unix", sock)
	if err != nil {
		panic(err)
	}
	defer l.Close()
	os.Chmod(sock, 0o660)
	for {
		c, err := l.Accept()
		if err != nil {
			continue
		}
		go handle(c)
	}
}

func handle(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "bindproc ") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			fmt.Fprintf(c, "NO args\n")
			return
		}
		pid, err := strconv.Atoi(fields[1])
		if err != nil || pid <= 0 {
			fmt.Fprintf(c, "NO pid\n")
			return
		}
		target := strings.TrimSpace(fields[2])
		if !allowed(target) {
			fmt.Fprintf(c, "NO ok\n")
			return
		}
		if err := os.MkdirAll(target, 0755); err != nil {
			fmt.Fprintf(c, "NO mkdir %s\n", err)
			return
		}
		ns, err := os.Open(fmt.Sprintf("/proc/%d/ns/mnt", pid))
		if err != nil {
			fmt.Fprintf(c, "NO ns %s\n", err)
			return
		}
		if err := unix.Setns(int(ns.Fd()), unix.CLONE_NEWNS); err != nil {
			ns.Close()
			fmt.Fprintf(c, "NO setns %s\n", err)
			return
		}
		ns.Close()
		if err := unix.Mount("proc", target, "proc", unix.MS_NOSUID|unix.MS_NOEXEC|unix.MS_NODEV, ""); err != nil {
			fmt.Fprintf(c, "NO mount %s\n", err)
			return
		}
		fmt.Fprintf(c, "OK\n")
		return
	}
	if strings.HasPrefix(line, "bindnull ") {
		target := strings.TrimSpace(strings.TrimPrefix(line, "bindnull"))
		if !allowed(target) {
			fmt.Fprintf(c, "NO ok\n")
			return
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			fmt.Fprintf(c, "NO mkdir %s\n", err)
			return
		}
		if _, err := os.Stat(target); os.IsNotExist(err) {
			f, _ := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, 0o666)
			if f != nil {
				f.Close()
			}
		}
		if err := unix.Mount("/dev/null", target, "", unix.MS_BIND, ""); err != nil {
			fmt.Fprintf(c, "NO mount %s\n", err)
			return
		}
		fmt.Fprintf(c, "OK\n")
		return
	}
	if strings.HasPrefix(line, "bind ") {
		target := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "bind"), "bindnull"))
		target = strings.TrimSpace(target)
		if !allowed(target) || filepath.Base(target) != "null" {
			fmt.Fprintf(c, "NO ok\n")
			return
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			fmt.Fprintf(c, "NO mkdir %s\n", err)
			return
		}
		if _, err := os.Stat(target); os.IsNotExist(err) {
			f, _ := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, 0o666)
			if f != nil {
				f.Close()
			}
		}
		if err := unix.Mount("/dev/null", target, "", unix.MS_BIND, ""); err != nil {
			fmt.Fprintf(c, "NO mount %s\n", err)
			return
		}
		fmt.Fprintf(c, "OK\n")
		return
	}
	if strings.HasPrefix(line, "unbind") {
		target := strings.TrimSpace(strings.TrimPrefix(line, "unbind"))
		if !allowed(target) {
			fmt.Fprintf(c, "NO ok\n")
			return
		}
		_ = unix.Unmount(target, unix.MNT_DETACH)
		fmt.Fprintf(c, "OK\n")
		return
	}
	if strings.HasPrefix(line, "unbindproc") {
		target := strings.TrimSpace(strings.TrimPrefix(line, "unbindproc"))
		fields := strings.Fields(target)
		if len(fields) != 2 {
			fmt.Fprintf(c, "NO args\n")
			return
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			fmt.Fprintf(c, "NO pid\n")
			return
		}
		target = strings.TrimSpace(fields[1])
		if !allowed(target) {
			fmt.Fprintf(c, "NO ok\n")
			return
		}
		ns, err := os.Open(fmt.Sprintf("/proc/%d/ns/mnt", pid))
		if err != nil {
			fmt.Fprintf(c, "NO ns %s\n", err)
			return
		}
		if err := unix.Setns(int(ns.Fd()), unix.CLONE_NEWNS); err != nil {
			ns.Close()
			fmt.Fprintf(c, "NO setns %s\n", err)
			return
		}
		ns.Close()
		_ = unix.Unmount(target, unix.MNT_DETACH)
		fmt.Fprintf(c, "OK\n")
		return
	}
	fmt.Fprintf(c, "NO unknown\n")
}