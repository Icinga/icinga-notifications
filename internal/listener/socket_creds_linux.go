//go:build linux

package listener

import (
	"net"

	"golang.org/x/sys/unix"
)

func socketPeerCreds(c net.Conn) (*unix.Ucred, error) {
	rawConn, err := c.(*net.UnixConn).SyscallConn()
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
