//go:build darwin

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
