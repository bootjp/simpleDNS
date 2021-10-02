package main

import (
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/pkg/errors"

	"github.com/google/gopacket/layers"

	"github.com/google/gopacket"
)

var logger *log.Logger
var cache *CacheRepository

type AnswerCache struct {
	Response  *layers.DNS
	TimeToDie int64
}

type DNSCache map[layers.DNSType]AnswerCache

func NewCacheRepository() *CacheRepository {
	return &CacheRepository{
		items: map[string]DNSCache{},
		mu:    &sync.RWMutex{},
	}
}

type CacheRepository struct {
	items map[string]DNSCache
	mu    *sync.RWMutex
}

var CacheNotFound = errors.New("cache not found")

func (c *CacheRepository) Get(name string, t layers.DNSType) (*layers.DNS, error) {
	c.mu.RLock()
	cn, ok := c.items[name]
	c.mu.RUnlock()
	if !ok || len(cn[t].Response.Answers) <= 0 {
		return nil, CacheNotFound
	}

	expire := time.Now().Unix()-cn[t].TimeToDie > 0

	if expire {
		logger.Println("stale cache")
		c.mu.Lock()

		if len(cache.items[name]) == 1 {
			delete(cache.items, name)
		} else {
			delete(cache.items[name], t)
		}
		c.mu.Unlock()
		logger.Println("purged " + name)
		return nil, CacheNotFound
	}

	return cn[t].Response, nil
}

func (c *CacheRepository) Set(name string, t layers.DNSType, dns AnswerCache) error {
	c.mu.Lock()

	if _, ok := c.items[name]; !ok {
		c.items[name] = map[layers.DNSType]AnswerCache{}
	}
	c.items[name][t] = dns
	c.mu.Unlock()

	return nil
}

func init() {
	l := log.New(os.Stdout, "[simpleDNS] ", log.LstdFlags|log.Lmicroseconds|log.Lshortfile)
	logger = l

	cache = NewCacheRepository()
}

func main() {

	logger.Println("Server listening  at localhost:15353")
	conn, err := net.ListenPacket("udp", "localhost:15353")
	if err != nil {
		logger.Println(err)
	}
	defer conn.Close()
	buffer := make([]byte, 512)
	for {
		length, addr, err := conn.ReadFrom(buffer)
		if err != nil {
			logger.Println(err)
			continue
		}

		p := gopacket.NewPacket(buffer[:length], layers.LayerTypeDNS, gopacket.Default)
		if p.ErrorLayer() != nil {
			logger.Println(p.ErrorLayer().Error())
			continue
		}

		dns := p.Layer(layers.LayerTypeDNS).(*layers.DNS)

		if len(dns.Questions) != 1 {
			logger.Println("Failed to parse 1 DNS answer")
			continue
		}

		name := string(dns.Questions[0].Name)
		c, err := cache.Get(name, dns.Questions[0].Type)
		if err == nil {
			logger.Println("use cache ")
			c.ID = dns.ID
			buf := gopacket.NewSerializeBuffer()
			opts := gopacket.SerializeOptions{FixLengths: true}
			if err := gopacket.SerializeLayers(buf, opts, c); err != nil {
				logger.Println(err)
				continue
			}

			if _, err := conn.WriteTo(buf.Bytes(), addr); err != nil {
				logger.Println(err)
				continue
			}
			continue
		}

		logger.Println(err)
		logger.Println("request " + dns.Questions[0].Type.String() + " " + string(dns.Questions[0].Name) + " " + dns.Questions[0].Class.String())

		answer := &layers.DNS{
			ID:     dns.ID,
			QR:     true,
			OpCode: layers.DNSOpCodeQuery,
			AA:     true,
			RD:     true,
			RA:     true,
		}
		answer.Answers = append(dns.Answers,
			layers.DNSResourceRecord{
				Name:  []byte("bootjp.me"),
				Type:  layers.DNSTypeA,
				Class: layers.DNSClassIN,
				TTL:   10,
				IP:    net.IPv4(192, 168, 0, 1),
			})

		buf := gopacket.NewSerializeBuffer()
		opts := gopacket.SerializeOptions{FixLengths: true}
		if err := gopacket.SerializeLayers(buf, opts, answer); err != nil {
			logger.Println(err)
			continue
		}

		if _, err := conn.WriteTo(buf.Bytes(), addr); err != nil {
			logger.Println(err)
			continue
		}

		if err := cache.Set(name, layers.DNSTypeA, AnswerCache{
			Response:  answer,
			TimeToDie: time.Now().Unix() + int64(answer.Answers[0].TTL),
		}); err != nil {
			logger.Println(err)
			continue
		}

	}
}
