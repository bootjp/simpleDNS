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
	logger := log.New(os.Stdout, "[simpleDNS] ", log.LstdFlags|log.Lmicroseconds|log.Lshortfile)
	dns, err := core.NewSimpleDNSServer(logger)
	if err != nil {
		log.Println(err)
		return 1
	}
	return dns.Run()
}
