package main

import (
	"log"
	"net"
	"os"

	"github.com/bootjp/simple_dns/core"
	"go.uber.org/zap"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	lc := zap.NewProductionConfig()
	logger, _ := lc.Build()
	logger.Core()

	defer func(logger *zap.Logger) {
		_ = logger.Sync()
	}(logger)

	var ips [2]*core.NameServer

	ips[0] = core.NewNameServer(net.ParseIP("1.1.1.1"), 53)
	ips[1] = core.NewNameServer(net.ParseIP("8.8.8.8"), 53)

	c := core.NewDefaultConfig(ips)

	dns, err := core.NewSimpleDNSServer(c, logger)
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
