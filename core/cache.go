package core

import (
	"fmt"
	"sync"

	"go.uber.org/zap"

	"golang.org/x/net/dns/dnsmessage"

	lru "github.com/hashicorp/golang-lru"
)

type AnswerCache struct {
	Response  *dnsmessage.Message
	TimeToDie int64
}

func NewCacheRepository(size int, logger *zap.Logger) (*CacheRepository, error) {
	var eviction = func(key interface{}, value interface{}) {
		k := key.(string)
		go logger.Info("eviction", zap.String("key", k))
	}

	c, err := lru.NewWithEvict(size, eviction)
	if err != nil {
		logger.Info("failed create lru cache", zap.Error(err))
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
	log       *zap.Logger
	maxLength int
}

const FormatCacheKey = "%s:%s"

func (c *CacheRepository) key(name *dnsmessage.Name, t *dnsmessage.Type) string {
	return fmt.Sprintf(FormatCacheKey, name.String(), t.String())
}

func (c *CacheRepository) Get(unow int64, name *dnsmessage.Name, t *dnsmessage.Type) (*AnswerCache, bool) {
	key := c.key(name, t)
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
		go c.log.Info("purge cache",
			zap.String("type", t.String()),
			zap.String("name", name.String()),
		)
		c.items.Remove(key)
		return nil, false
	}

	return &cn, true
}

func (c *CacheRepository) Set(name *dnsmessage.Name, t *dnsmessage.Type, dns AnswerCache) error {
	add := make([]dnsmessage.Resource, len(dns.Response.Additionals))
	i := 0
	for _, additional := range dns.Response.Additionals {
		if additional.Header.Type == dnsmessage.TypeOPT {
			continue
		}
		add[i] = additional
		i++
	}
	dns.Response.Additionals = add[:i]
	_ = c.items.Add(c.key(name, t), dns)
	return nil
}
