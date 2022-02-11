package core

import (
	"fmt"
	"net"
	"testing"
)

func TestIPVersion(t *testing.T) {

	test := []struct {
		ipPlain string
		wont    int
	}{
		{
			"127.0.0.1", 4,
		},
		{
			"192.168.0.1", 4,
		},
		{
			"::1", 6,
		},
		{
			"2001:DB8:0:0:8:800:200C:417A", 6,
		},
	}

	for _, ip := range test {
		res := checkIPVersion(ip.ipPlain)
		if res != ip.wont {
			t.Fatalf("miss match ip version test wont %d got %d", ip.wont, res)
		}

		fmt.Println(net.ParseIP(ip.ipPlain).To16())
	}
}
