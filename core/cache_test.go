package core

import (
	"log"
	"os"
	"testing"
)

func TestLoglevel(t *testing.T) {
	f, err := os.Open("../config.yaml")
	if err != nil {
		log.Fatalln(err)
	}

	conf, err := ParseConfigByFile(f)

	if conf.LogLevel != LogLevelInfo {
		log.Fatalln("miss match log level")
	}

}
