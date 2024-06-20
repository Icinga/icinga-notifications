package internal

// This variable exists to allow overwriting the path using `go build -ldflags "-X ...", see Makefile.
var (
	LibExecDir = "/usr/libexec"
)
