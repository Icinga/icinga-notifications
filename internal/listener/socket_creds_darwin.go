//go:build darwin

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

	var xucreds *unix.Xucred
	var xucredsErr error
	err = rawConn.Control(func(fd uintptr) {
		xucreds, xucredsErr = unix.GetsockoptXucred(int(fd), unix.SOL_SOCKET, unix.LOCAL_PEERCRED)
	})
	if err != nil {
		return nil, err
	}
	if xucredsErr != nil {
		return nil, xucredsErr
	}

	return &unix.Ucred{
		Uid: xucreds.Uid,
	}, nil
}
