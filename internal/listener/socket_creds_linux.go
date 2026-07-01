//go:build linux

package listener

import (
	"syscall"

	"golang.org/x/sys/unix"
)

func socketPeerCreds(rawConn syscall.RawConn) (string, error) {
	var creds *unix.Ucred
	var credsErr error
	err = rawConn.Control(func(fd uintptr) {
		creds, credsErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	})
	if err != nil {
		return "", err
	}
	if credsErr != nil {
		return "", credsErr
	}

	uid := strconv.FormatUint(uint64(creds.Uid), 10)

	return uid, nil
}
