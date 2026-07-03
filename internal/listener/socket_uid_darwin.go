//go:build darwin

package listener

import (
	"syscall"

	"golang.org/x/sys/unix"
)

func socketPeerUid(rawConn syscall.RawConn) (uint32, error) {
	var xucreds *unix.Xucred
	var xucredsErr error
	err := rawConn.Control(func(fd uintptr) {
		xucreds, xucredsErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	})
	if err != nil {
		return 0, err
	}
	if xucredsErr != nil {
		return 0, xucredsErr
	}

	return xucreds.Uid, nil
}
