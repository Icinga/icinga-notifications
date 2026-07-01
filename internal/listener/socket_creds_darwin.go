//go:build darwin

package listener

import (
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

func socketPeerCreds(rawConn syscall.RawConn) (string, error) {

	var xucreds *unix.Xucred
	var xucredsErr error
	err := rawConn.Control(func(fd uintptr) {
		xucreds, xucredsErr = unix.GetsockoptXucred(int(fd), unix.SOL_SOCKET, unix.LOCAL_PEERCRED)
	})
	if err != nil {
		return "", err
	}
	if xucredsErr != nil {
		return "", xucredsErr
	}

	uid := strconv.FormatUint(uint64(xucreds.Uid), 10)

	return uid, nil
}
