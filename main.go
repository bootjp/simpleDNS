package main

import (
	"log"
	"os"

	"github.com/bootjp/simple_dns/core"
)

var logger *log.Logger

func main() {
	os.Exit(_main())
}

func _main() int {
	dns := core.SimpleDNS{
		Log: log.New(os.Stdout, "[simpleDNS] ", log.LstdFlags|log.Lmicroseconds|log.Lshortfile),
	}
	return dns.Run()
}
