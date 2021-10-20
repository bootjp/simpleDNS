package core

import (
	"net"
	"strconv"
)

type Host string
type LogLevel int8

const (
	LoglevelDebug LogLevel = iota - 1
	LogLevelInfo  LogLevel = 0
	LogLevelError LogLevel = 5
)

type Config struct {
	ListenAddr   *net.UDPAddr
	MaxCacheSize int
	NameServer   [2]*NameServer
	Hosts        []*Host
	LogLevel     LogLevel
	UseHosts     bool
}

func NewDefaultConfig(s [2]*NameServer) *Config {
	return &Config{
		NameServer:   s,
		MaxCacheSize: 1000,
		ListenAddr: &net.UDPAddr{
			IP:   net.ParseIP("0.0.0.0"),
			Port: 53,
		},
		LogLevel: LogLevelInfo,
		UseHosts: true,
	}
}

func (c *Config) IsValid() bool {
	// allow listen port is 0
	if c.ListenAddr.Port < 0 {
		return false
	}

	for _, s := range c.NameServer {
		if s.Port <= 0 {
			return false
		}
	}

	return true
}

func NewNameServer(ip net.IP, port int) *NameServer {
	return &NameServer{
		IP:   ip,
		Port: port,
	}
}

type NameServer struct {
	IP   net.IP
	Port int
}

func (n *NameServer) String() string {
	return n.IP.String() + ":" + strconv.Itoa(n.Port)
}
