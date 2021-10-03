package core

import (
	"errors"
	"log"
	"sync"
	"time"

	"github.com/google/gopacket/layers"
)

type AnswerCache struct {
	Response  *layers.DNS
	TimeToDie int64
}

type DNSCache map[layers.DNSType]AnswerCache

func NewCacheRepository(logger *log.Logger) *Repository {
	return &Repository{
		items: map[string]DNSCache{},
		mu:    &sync.RWMutex{},
		log:   logger,
	}
}

type Repository struct {
	items map[string]DNSCache
	mu    *sync.RWMutex
	log   *log.Logger
}

var NotFound = errors.New("core not found")

func (c *Repository) Get(name string, t layers.DNSType) (*layers.DNS, error) {
	c.mu.RLock()
	cn, ok := c.items[name]
	c.mu.RUnlock()
	if !ok || len(cn[t].Response.Answers) <= 0 {
		return nil, NotFound
	}

	expire := time.Now().Unix()-cn[t].TimeToDie > 0

	if expire {
		c.log.Println("stale cache")
		c.mu.Lock()

		if len(c.items[name]) == 1 {
			delete(c.items, name)
		} else {
			delete(c.items[name], t)
		}
		c.mu.Unlock()
		c.log.Println("purged " + name)
		return nil, NotFound
	}

	return cn[t].Response, nil
}

func (c *Repository) Set(name string, t layers.DNSType, dns AnswerCache) error {
	c.mu.Lock()

	if _, ok := c.items[name]; !ok {
		c.items[name] = map[layers.DNSType]AnswerCache{}
	}
	c.items[name][t] = dns
	c.mu.Unlock()

	return nil
}
