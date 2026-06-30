//go:build linux

package listener

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

func socketPeerCreds(c net.Conn) (*unix.Ucred, error) {
	unixConn, ok := c.(*net.UnixConn)
	if !ok {
		return nil, fmt.Errorf("expected *net.UnixConn, got %T", c)
	}

	rawConn, err := unixConn.SyscallConn()
	if err != nil {
		return nil, err
	}

	var creds *unix.Ucred
	var credsErr error
	err = rawConn.Control(func(fd uintptr) {
		creds, credsErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	})
	if err != nil {
		return nil, err
	}
	if credsErr != nil {
		return nil, credsErr
	}

	return creds, nil
}
