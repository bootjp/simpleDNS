package main

import (
	"io/ioutil"
	"log"
	"os"

	"gopkg.in/yaml.v3"

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

	b, err := ioutil.ReadFile("config.yaml")
	if err != nil {
		logger.Error("failed to load config", zap.Error(err))
		os.Exit(1)
	}

	var c core.Config
	err = yaml.Unmarshal(b, &c)
	if err != nil {
		logger.Error("failed parse yaml", zap.Error(err))
		os.Exit(1)
	}

	defer func(logger *zap.Logger) {
		_ = logger.Sync()
	}(logger)

	dns, err := core.NewSimpleDNSServer(&c, logger)
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
