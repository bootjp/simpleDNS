package core

import (
	"os"
	"testing"
)

func TestLoglevel(t *testing.T) {
	f, err := os.Open("../config.yaml")
	if err != nil {
		t.Fatal(err)
	}

	conf, err := ParseConfigByFile(f)

	if conf.LogLevel != LogLevelInfo {
		t.Fatal("miss match log level")
	}

}
