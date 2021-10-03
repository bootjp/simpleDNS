package main

import (
	"log"
	"os"

	"github.com/bootjp/simple_dns/core"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	dns := core.SimpleDNS{
		Log: log.New(os.Stdout, "[simpleDNS] ", log.LstdFlags|log.Lmicroseconds|log.Lshortfile),
	}
	return dns.Run()
}
