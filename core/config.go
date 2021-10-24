package core

import (
	"io/ioutil"
	"net"
	"os"
	"strconv"

	"gopkg.in/yaml.v2"
)

type Host string
type LogLevel int8

const (
	LoglevelDebug LogLevel = iota - 1
	LogLevelInfo  LogLevel = 0
	LogLevelError LogLevel = 5
)

type Config struct {
	ListenAddr   *net.UDPAddr   `yaml:"listen_addr"`
	MaxCacheSize int            `yaml:"max_cache_size"`
	NameServer   [2]*NameServer `yaml:"name_server"`
	Hosts        []*Host        `yaml:"hosts"`
	LogLevel     LogLevel       `yaml:"log_level"`
	UseHosts     bool           `yaml:"use_hosts"`
}

func LoadConfig(file ...*os.File) (*Config, error) {
	var openFile *os.File
	if file == nil {
		f, err := os.Open("config.yaml")
		if err != nil {
			return nil, err
		}
		openFile = f
	}

	return ParseConfigByFile(openFile)
}

func ParseConfigByFile(file *os.File) (*Config, error) {

	b, err := ioutil.ReadAll(file)
	if err != nil {
		return nil, err
	}

	c := Config{}
	err = yaml.Unmarshal(b, &c)
	if err != nil {
		return nil, err
	}

	return &c, err
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
