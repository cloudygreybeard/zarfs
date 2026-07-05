// Copyright 2026
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/cloudygreybeard/zarfs/internal/archive"
	"github.com/cloudygreybeard/zarfs/internal/arcfs"
	"github.com/cloudygreybeard/zarfs/internal/fusemount"
	"github.com/cloudygreybeard/zarfs/internal/nfsmount"
)

const (
	daemonEnv      = "_ZARFS_DAEMON"
	statusFDEnv    = "_ZARFS_STATUS_FD"
	passwordEnvVar = "ZARFS_PASSWORD"
	childPwEnvVar  = "_ZARFS_PASSWORD"
)

var (
	debug      bool
	foreground bool
	readOnly   bool
	transport  string
	nfsAddr    string
	password   string
	formatFlag string
)

var mountCmd = &cobra.Command{
	Use:   "mount ARCHIVE MOUNTPOINT",
	Short: "Mount a RISC OS archive as a filesystem",
	Long: `Mount a RISC OS archive file as a filesystem. ArcFS archives
(filetype 3FB) are mounted read-write by default; other formats are
always read-only. Use --read-only to force read-only mode.

On Linux, this uses kernel FUSE (/dev/fuse) by default.
On macOS, this uses an embedded NFS server if FUSE is unavailable.

Use --transport to override the auto-detected transport:

  zarfs mount archive.arc /mnt/archive                     # auto-detect
  zarfs mount archive.arc /mnt/archive --transport=fuse    # force FUSE
  zarfs mount archive.arc /mnt/archive --transport=nfs     # force NFS
  zarfs mount archive.arc /mnt/archive --read-only         # force read-only`,
	Args: cobra.ExactArgs(2),
	RunE: runMount,
}

func init() {
	mountCmd.Flags().BoolVar(&debug, "debug", false, "enable debug logging")
	mountCmd.Flags().BoolVarP(&foreground, "foreground", "f", false, "run in the foreground (default: daemonize)")
	mountCmd.Flags().BoolVar(&readOnly, "read-only", false, "mount the archive read-only (default: read-write for ArcFS)")
	mountCmd.Flags().StringVar(&transport, "transport", "auto", "mount transport: auto, fuse, or nfs")
	mountCmd.Flags().StringVar(&nfsAddr, "nfs-addr", "127.0.0.1:0", "NFS server listen address (transport=nfs only)")
	mountCmd.Flags().StringVarP(&password, "password", "p", "", "archive password for garbled Spark/ArcFS archives")
	mountCmd.Flags().StringVar(&formatFlag, "format", "", "archive format (tar, targz, arcfs, spark, packdir, squash, cfs); auto-detected if omitted")

	rootCmd.AddCommand(mountCmd)
}

func expandTilde(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}

func resolveTransport() string {
	if transport != "auto" {
		return transport
	}
	if runtime.GOOS == "darwin" {
		if _, err := os.Stat("/Library/Filesystems/macfuse.fs"); err == nil {
			return "fuse"
		}
		return "nfs"
	}
	if _, err := os.Stat("/dev/fuse"); err == nil {
		return "fuse"
	}
	return "nfs"
}

func resolvePassword(cmd *cobra.Command) ([]byte, error) {
	if v := os.Getenv(childPwEnvVar); v != "" {
		_ = os.Unsetenv(childPwEnvVar)
		return []byte(v), nil
	}
	if v := os.Getenv(passwordEnvVar); v != "" {
		return []byte(v), nil
	}
	if !cmd.Flags().Changed("password") {
		return nil, nil
	}
	if password == "" || password == "-" {
		fmt.Fprintf(os.Stderr, "Password: ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			return []byte(scanner.Text()), nil
		}
		return nil, fmt.Errorf("reading password from stdin: %w", scanner.Err())
	}
	fmt.Fprintf(os.Stderr, "zarfs: warning: command-line passwords are visible in process listings; prefer ZARFS_PASSWORD env var\n")
	return []byte(password), nil
}

func runMount(cmd *cobra.Command, args []string) error {
	archivePath := expandTilde(args[0])
	mountpoint := expandTilde(args[1])

	absArchive, err := filepath.Abs(archivePath)
	if err != nil {
		return fmt.Errorf("resolving archive path: %w", err)
	}

	logger := log.New(os.Stderr, "zarfs: ", log.LstdFlags)

	if !foreground && os.Getenv(daemonEnv) == "" {
		return daemonize(logger)
	}

	passwd, err := resolvePassword(cmd)
	if err != nil {
		return err
	}

	var format archive.Format
	if formatFlag != "" {
		format, err = archive.ParseFormat(formatFlag)
		if err != nil {
			reportMountStatus(err)
			return err
		}
	}

	afs, err := arcfs.OpenFormat(absArchive, passwd, readOnly, format)
	if err != nil {
		reportMountStatus(err)
		return err
	}
	defer func() { _ = afs.Close() }()

	mode := "read-write"
	if afs.ReadOnly() {
		mode = "read-only"
	}
	logger.Printf("opened %s archive: %s (%s)", afs.Format(), absArchive, mode)

	resolved := resolveTransport()
	logger.Printf("using %s transport", resolved)

	switch resolved {
	case "nfs":
		return serveNFS(afs, mountpoint, logger)
	case "fuse":
		return serveFUSE(afs, mountpoint, logger)
	default:
		return fmt.Errorf("unknown transport %q (valid: auto, fuse, nfs)", resolved)
	}
}

func serveFUSE(afs *arcfs.FS, mountpoint string, logger *log.Logger) error {
	server, err := fusemount.Mount(afs, mountpoint, afs.ReadOnly(), debug, logger)

	reportMountStatus(err)

	if err != nil {
		return fmt.Errorf("mounting on %s: %w", mountpoint, err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Printf("received %v, unmounting...", sig)
		_ = afs.Sync()
		if err := server.Unmount(); err != nil {
			logger.Printf("unmount failed: %v; forcing exit", err)
			os.Exit(1)
		}
		sig = <-sigCh
		logger.Printf("received %v again, forcing exit", sig)
		_ = afs.Sync()
		os.Exit(1)
	}()

	server.Wait()
	logger.Println("unmounted")
	return nil
}

func serveNFS(afs *arcfs.FS, mountpoint string, logger *log.Logger) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv, err := nfsmount.Mount(ctx, afs, mountpoint, nfsAddr, logger)

	reportMountStatus(err)

	if err != nil {
		return fmt.Errorf("mounting on %s: %w", mountpoint, err)
	}

	logger.Printf("mounted %s on %s (pid %d, nfs)", afs.Format(), mountpoint, os.Getpid())

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Printf("received %v, unmounting...", sig)
		_ = afs.Sync()
		if err := srv.Unmount(); err != nil {
			logger.Printf("unmount failed: %v; forcing exit", err)
			_ = afs.Sync()
			os.Exit(1)
		}
		cancel()
	}()

	srv.Wait(ctx)
	logger.Println("unmounted")
	return nil
}

func stripPasswordArgs(args []string) []string {
	var out []string
	skip := false
	for i, a := range args {
		if skip {
			skip = false
			continue
		}
		if a == "-p" || a == "--password" {
			skip = true
			continue
		}
		if strings.HasPrefix(a, "-p") && len(a) > 2 && a[2] != '-' {
			continue
		}
		if strings.HasPrefix(a, "--password=") {
			continue
		}
		_ = i
		out = append(out, a)
	}
	return out
}

func daemonize(logger *log.Logger) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding executable path: %w", err)
	}

	statusR, statusW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("creating status pipe: %w", err)
	}

	childArgs := stripPasswordArgs(os.Args[1:])
	child := exec.Command(exe, childArgs...)
	child.Env = append(os.Environ(), daemonEnv+"=1")
	if password != "" {
		child.Env = append(child.Env, childPwEnvVar+"="+password)
	}
	child.ExtraFiles = []*os.File{statusW}
	child.Env = append(child.Env, fmt.Sprintf("%s=%d", statusFDEnv, 3))
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	logFile, err := os.CreateTemp("", "zarfs-*.log")
	if err != nil {
		_ = statusR.Close()
		_ = statusW.Close()
		return fmt.Errorf("creating log file: %w", err)
	}
	child.Stderr = logFile
	child.Stdout = logFile

	if err := child.Start(); err != nil {
		_ = logFile.Close()
		_ = statusR.Close()
		_ = statusW.Close()
		return fmt.Errorf("starting background process: %w", err)
	}

	_ = statusW.Close()

	buf := make([]byte, 1024)
	n, _ := statusR.Read(buf)
	_ = statusR.Close()

	msg := string(buf[:n])
	if msg != "ok" {
		_ = child.Wait()
		_ = logFile.Close()
		if msg == "" {
			return fmt.Errorf("background process exited before mounting (log: %s)", logFile.Name())
		}
		return fmt.Errorf("%s", msg)
	}

	logger.Printf("mounted (pid %d, log: %s)", child.Process.Pid, logFile.Name())

	_ = child.Process.Release()
	_ = logFile.Close()
	return nil
}

func reportMountStatus(mountErr error) {
	fdStr := os.Getenv(statusFDEnv)
	if fdStr == "" {
		return
	}
	fd := 3
	_, _ = fmt.Sscanf(fdStr, "%d", &fd)
	w := os.NewFile(uintptr(fd), "status-pipe")
	if w == nil {
		return
	}
	defer func() { _ = w.Close() }()
	if mountErr != nil {
		_, _ = fmt.Fprintf(w, "%v", mountErr)
	} else {
		_, _ = fmt.Fprint(w, "ok")
	}
}
