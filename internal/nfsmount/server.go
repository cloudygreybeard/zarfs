// Package nfsmount implements an NFS-based mount transport for zarfs.
// It embeds an NFSv3 server and mounts it via the operating system's
// built-in NFS client, requiring no FUSE installation.
package nfsmount

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"runtime"

	nfs "github.com/willscott/go-nfs"
	nfshelper "github.com/willscott/go-nfs/helpers"

	"github.com/cloudygreybeard/zarfs/internal/arcfs"
)

// Server manages the embedded NFSv3 server and mount lifecycle.
type Server struct {
	afs        *arcfs.FS
	listener   net.Listener
	mountpoint string
	logger     *log.Logger
	cancel     context.CancelFunc
}

// Mount starts an NFSv3 server on localhost and mounts it at the given
// mount point using the operating system's built-in NFS client.
func Mount(ctx context.Context, afs *arcfs.FS, mountpoint, addr string, logger *log.Logger) (*Server, error) {
	if addr == "" {
		addr = "127.0.0.1:0"
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("starting NFS listener: %w", err)
	}

	tcpAddr := listener.Addr().(*net.TCPAddr)
	if !tcpAddr.IP.IsLoopback() {
		_ = listener.Close()
		return nil, fmt.Errorf("NFS server address %s is not loopback; refusing to bind (security risk)", tcpAddr.IP)
	}

	port := tcpAddr.Port
	logger.Printf("NFS server listening on %s:%d", tcpAddr.IP, port)

	ctx, cancel := context.WithCancel(ctx)

	billyFS := NewBillyFS(ctx, afs)
	handler := nfshelper.NewNullAuthHandler(billyFS)
	cacheHelper := nfshelper.NewCachingHandler(handler, 1024)

	go func() {
		if err := nfs.Serve(listener, cacheHelper); err != nil {
			logger.Printf("NFS server error: %v", err)
		}
	}()

	if err := mountNFS(mountpoint, port); err != nil {
		cancel()
		_ = listener.Close()
		return nil, fmt.Errorf("mounting NFS: %w", err)
	}

	return &Server{
		afs:        afs,
		listener:   listener,
		mountpoint: mountpoint,
		logger:     logger,
		cancel:     cancel,
	}, nil
}

// Unmount unmounts the filesystem and stops the NFS server.
func (s *Server) Unmount() error {
	mountErr := exec.Command("umount", s.mountpoint).Run()
	s.cancel()
	_ = s.listener.Close()
	return mountErr
}

// Wait blocks until the context is cancelled.
func (s *Server) Wait(ctx context.Context) {
	<-ctx.Done()
}

func mountNFS(mountpoint string, port int) error {
	if err := os.MkdirAll(mountpoint, 0o755); err != nil {
		return fmt.Errorf("creating mount point: %w", err)
	}

	opts := fmt.Sprintf("port=%d,mountport=%d", port, port)

	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("mount", "-o", opts, "-t", "nfs", "localhost:/", mountpoint)
	} else {
		opts += ",nfsvers=3,noacl,tcp"
		cmd = exec.Command("mount", "-o", opts, "-t", "nfs", "localhost:/", mountpoint)
	}

	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mount command failed: %w", err)
	}
	return nil
}
