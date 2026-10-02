package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

var ErrSFTPNotConfigured = errors.New("sftp storage not configured")

type SFTPConfig struct {
	Host      string
	Port      int
	Username  string
	AuthType  string // "password" | "key"
	Secret    string // password or PEM private key
	RemoteDir string
}

type SFTP struct {
	cfg SFTPConfig
}

func NewSFTP(cfg SFTPConfig) (*SFTP, error) {
	if cfg.Host == "" || cfg.Username == "" || cfg.AuthType == "" || cfg.Secret == "" {
		return nil, ErrSFTPNotConfigured
	}
	if cfg.AuthType != "password" && cfg.AuthType != "key" {
		return nil, fmt.Errorf("%w: invalid authType %q", ErrSFTPNotConfigured, cfg.AuthType)
	}
	if cfg.Port == 0 {
		cfg.Port = 22
	}
	if cfg.RemoteDir == "" {
		cfg.RemoteDir = "."
	}
	return &SFTP{cfg: cfg}, nil
}

func (s *SFTP) Kind() string { return "sftp" }

// dial opens one SSH connection with an SFTP session over it. The returned
// func closes both and must be called exactly once.
func (s *SFTP) dial() (*sftp.Client, func(), error) {
	var auth ssh.AuthMethod
	switch s.cfg.AuthType {
	case "password":
		auth = ssh.Password(s.cfg.Secret)
	default: // "key", guarded by NewSFTP
		signer, err := ssh.ParsePrivateKey([]byte(s.cfg.Secret))
		if err != nil {
			return nil, nil, fmt.Errorf("parse private key: %w", err)
		}
		auth = ssh.PublicKeys(signer)
	}
	// ponytail: host key not pinned; add known_hosts verification before
	// pointing this at an untrusted network.
	conn, err := ssh.Dial("tcp",
		net.JoinHostPort(s.cfg.Host, fmt.Sprintf("%d", s.cfg.Port)),
		&ssh.ClientConfig{
			User:            s.cfg.Username,
			Auth:            []ssh.AuthMethod{auth},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			Timeout:         15 * time.Second,
		})
	if err != nil {
		return nil, nil, err
	}
	cl, err := sftp.NewClient(conn)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	return cl, func() { cl.Close(); conn.Close() }, nil
}

func (s *SFTP) remotePath(key string) (string, error) {
	if key == "" || strings.Contains(key, "..") {
		return "", errors.New("invalid storage key")
	}
	return path.Join(s.cfg.RemoteDir, key), nil
}

func (s *SFTP) Put(ctx context.Context, key, localPath string) error {
	p, err := s.remotePath(key)
	if err != nil {
		return err
	}
	cl, closeConn, err := s.dial()
	if err != nil {
		return err
	}
	defer closeConn()
	if dir := path.Dir(p); dir != "." && dir != "/" {
		if err := cl.MkdirAll(dir); err != nil {
			return err
		}
	}
	if _, err := cl.Stat(p); err == nil {
		return fmt.Errorf("object already exists: %s", p)
	}
	in, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := cl.Create(p)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return cl.Chmod(p, 0o644)
}

func (s *SFTP) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	p, err := s.remotePath(key)
	if err != nil {
		return nil, err
	}
	cl, closeConn, err := s.dial()
	if err != nil {
		return nil, err
	}
	f, err := cl.Open(p)
	if err != nil {
		closeConn()
		return nil, err
	}
	return sftpReadCloser{ReadCloser: f, closeConn: closeConn}, nil
}

type sftpReadCloser struct {
	io.ReadCloser
	closeConn func()
}

func (r sftpReadCloser) Close() error {
	err := r.ReadCloser.Close()
	r.closeConn()
	return err
}

func (s *SFTP) Delete(ctx context.Context, key string) error {
	p, err := s.remotePath(key)
	if err != nil {
		return err
	}
	cl, closeConn, err := s.dial()
	if err != nil {
		return err
	}
	defer closeConn()
	if err := cl.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *SFTP) Stat(ctx context.Context, key string) error {
	p, err := s.remotePath(key)
	if err != nil {
		return err
	}
	cl, closeConn, err := s.dial()
	if err != nil {
		return err
	}
	defer closeConn()
	_, err = cl.Stat(p)
	return err
}

func (s *SFTP) Test(ctx context.Context) error {
	cl, closeConn, err := s.dial()
	if err != nil {
		return err
	}
	defer closeConn()
	if s.cfg.RemoteDir != "." && s.cfg.RemoteDir != "/" {
		if err := cl.MkdirAll(s.cfg.RemoteDir); err != nil {
			return err
		}
	}
	probe := path.Join(s.cfg.RemoteDir, ".kudump-probe")
	w, err := cl.Create(probe)
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte("ok")); err != nil {
		w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	if _, err := cl.Stat(probe); err != nil {
		return err
	}
	return cl.Remove(probe)
}
