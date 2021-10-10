package core

import (
	"fmt"
	"log"
	"sync"

	lru "github.com/hashicorp/golang-lru"

	"github.com/google/gopacket/layers"
)

const FormatCacheKey = "%s:%s"

type AnswerCache struct {
	Response  *layers.DNS
	TimeToDie int64
}

func NewCacheRepository(size int, logger *log.Logger) (*CacheRepository, error) {
	c, err := lru.New(size)
	if err != nil {
		logger.Println(err)
		return nil, err
	}

	return &CacheRepository{
		items:     c,
		mu:        &sync.RWMutex{},
		log:       logger,
		maxLength: size,
	}, nil
}

type CacheRepository struct {
	items     *lru.Cache
	mu        *sync.RWMutex
	log       *log.Logger
	maxLength int
}

func (c *CacheRepository) Get(unow int64, name []byte, t layers.DNSType) (*AnswerCache, bool) {
	key := fmt.Sprintf(FormatCacheKey, name, t.String())
	v, ok := c.items.Get(key)
	if !ok {
		return nil, false
	}
	cn, ok := v.(AnswerCache)
	if !ok {
		return nil, false
	}

	expire := unow-cn.TimeToDie > 0
	if expire {
		c.log.Println("stale cache")
		c.items.Remove(key)
		return nil, false
	}

	return &cn, true
}

func (c *CacheRepository) Set(name []byte, t layers.DNSType, dns AnswerCache) error {
	_ = c.items.Add(fmt.Sprintf(FormatCacheKey, name, t.String()), dns)
	return nil
}
