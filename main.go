package main

import (
	"log"
	"net"
	"os"

	"github.com/bootjp/simple_dns/core"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	logger := log.New(os.Stdout, "[simpleDNS] ", log.LstdFlags|log.Lmicroseconds|log.Lshortfile)
	var ips [2]*core.NameServer

	ips[0] = core.NewNameServer(net.ParseIP("8.8.8.8"), 53)
	ips[1] = core.NewNameServer(net.ParseIP("1.1.1.1"), 53)

	dns, err := core.NewSimpleDNSServer(ips, logger)
	if err != nil {
		log.Println(err)
		return 1
	}
	if err := dns.Run(); err != nil {
		log.Println(err)
		return 1
	}

	return 0
}
