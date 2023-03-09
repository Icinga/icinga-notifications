package main

import (
	"flag"
	"github.com/icinga/noma/internal/listener"
)

var (
	flagListen = flag.String("listen", "localhost:5680", "host:port to listen on")
)

func main() {
	flag.Parse()

	if err := listener.NewListener(*flagListen).Run(); err != nil {
		panic(err)
	}
}
