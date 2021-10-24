package main

import (
	"log"
	"os"

	"github.com/bootjp/simple_dns/core"
	"go.uber.org/zap"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {

	c, err := core.LoadConfig()
	if err != nil {
		log.Println("failed load config", zap.Error(err))
		return 1
	}

	dns, err := core.NewSimpleDNSServer(c)
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
