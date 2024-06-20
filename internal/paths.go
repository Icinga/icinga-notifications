package internal

// These variables exist to allow overwriting the paths using `go build -ldflags "-X ...", see Makefile.
var (
	LibExecDir = "/usr/libexec"
	SysConfDir = "/etc"
)
