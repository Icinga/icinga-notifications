//go:build linux

package listener

import (
	"syscall"

	"golang.org/x/sys/unix"
)

func socketPeerUid(rawConn syscall.RawConn) (uint32, error) {
	var creds *unix.Ucred
	var credsErr error
	err := rawConn.Control(func(fd uintptr) {
		creds, credsErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	})
	if err != nil {
		return 0, err
	}
	if credsErr != nil {
		return 0, credsErr
	}

	return creds.Uid, nil
}
