package core

import (
	"errors"
	"math"
	"net"
	"sync"
	"time"

	"go.uber.org/zap/zapcore"

	"go.uber.org/zap"

	"github.com/jaytaylor/go-hostsfile"

	"golang.org/x/net/dns/dnsmessage"
)

type SimpleDNS struct {
	ListenAddr *net.UDPAddr
	Server     [2]*NameServer
	log        *zap.Logger
	cache      *CacheRepository
	Config     *Config
	staticHost map[string][]dnsmessage.Resource
}

func NewSimpleDNSServer(c *Config) (SimpleDNSServer, error) {
	lc := zap.NewProductionConfig()
	lc.Level = zap.NewAtomicLevelAt(zapcore.Level(c.LogLevel))
	lc.Sampling = nil
	lc.DisableCaller = true
	logger, _ := lc.Build()
	logger.Core()

	cr, err := NewCacheRepository(c.MaxCacheSize, logger)
	if err != nil {
		return nil, err
	}

	return &SimpleDNS{
		ListenAddr: c.ListenAddr,
		Server:     c.NameServer,
		log:        logger,
		cache:      cr,
		Config:     c,
		staticHost: map[string][]dnsmessage.Resource{},
	}, nil
}

type SimpleDNSServer interface {
	Run() error
}

const DnsUdpMaxPacketSize = 4096

var bufferPool = sync.Pool{
	New: func() interface{} { return make([]byte, DnsUdpMaxPacketSize) },
}

func (d *SimpleDNS) Run() error {
	if d.Config.UseHosts {
		hostsMap, err := hostsfile.ParseHosts(hostsfile.ReadHostsFile())
		if err != nil {
			return err
		}

		for ip, strings := range hostsMap {
			for _, hostname := range strings {

				b := &dnsmessage.AResource{}
				copy(b.A[:], ip)

				h, err := dnsmessage.NewName(hostname + ".")
				if err != nil {
					d.log.Error("failed create hosts record. skipped ", zap.Error(err), zap.String("target", hostname))
					continue
				}

				ans := dnsmessage.Resource{
					Header: dnsmessage.ResourceHeader{
						Name:  h,
						Type:  dnsmessage.TypeA,
						Class: dnsmessage.ClassINET,
					},
					Body: b,
				}

				d.staticHost[hostname] = append(d.staticHost[hostname], ans)
			}
		}
	}

	d.log.Info("Server listening at " + d.ListenAddr.String())

	conn, err := net.ListenPacket("udp", d.ListenAddr.String())
	if err != nil {
		return err
	}
	defer func() {
		_ = conn.Close()
		_ = d.log.Sync()
	}()

	for {
		buffer := bufferPool.Get().([]byte)
		length, addr, err := conn.ReadFrom(buffer[0:])
		if err != nil {
			d.log.Error(err.Error())
			continue
		}
		go d.handleRequest(conn, addr, buffer, length)
	}
}

var ErrServerUnReached = errors.New("name servers all unreached")

func (d *SimpleDNS) resolveStatic(m *dnsmessage.Message) (*dnsmessage.Message, bool) {

	name := m.Questions[0].Name

	if hosts, ok := d.staticHost[name.String()[:(len(name.String())-1)]]; ok {
		msg := &dnsmessage.Message{
			Header: dnsmessage.Header{
				Response:      true,
				Authoritative: true,
				RCode:         dnsmessage.RCodeSuccess,
			},
		}

		msg.Answers = hosts
		msg.ID = m.ID

		return msg, true
	}

	return nil, false
}

func (d *SimpleDNS) resolve(msg *dnsmessage.Message) (*dnsmessage.Message, error) {

	t := &msg.Questions[0].Type
	name := &msg.Questions[0].Name

	if d.Config.UseHosts {
		if res, ok := d.resolveStatic(msg); ok {
			return res, nil
		}
	}

	unow := time.Now().Unix()
	if c, ok := d.cache.Get(unow, name, t); ok {
		go d.log.Info("use cache",
			zap.String("type", t.String()),
			zap.String("name", name.String()),
		)

		for i := range c.Response.Answers {
			c.Response.Answers[i].Header.TTL = uint32(c.TimeToDie - unow)
		}
		c.Response.Header.ID = msg.ID
		return c.Response, nil
	}

	go d.log.Info("request",
		zap.String("type", t.String()),
		zap.String("name", name.String()),
	)

	// as a reference https://github.com/coredns/coredns/blob/e0110264cce4d7cd4b8a5aee9a547646ee9742e5/plugin/forward/forward.go#L100
	deadline := time.Now().Add(5 * time.Second)

	for try := 1; time.Now().Before(deadline) && try <= len(d.Server); try++ {

		conn, err := net.Dial("udp", d.Server[try-1].String())
		if err != nil {
			d.log.Error("failed connect server", zap.Error(err))
			continue
		}

		timeout := time.Now().Add(time.Second * 2)
		err = conn.SetDeadline(timeout)
		if err != nil {
			d.log.Error("set deadline", zap.Error(err))
			continue
		}
		err = conn.SetReadDeadline(timeout)
		if err != nil {
			d.log.Error("set deadline", zap.Error(err))
			continue
		}
		err = conn.SetWriteDeadline(timeout)
		if err != nil {
			d.log.Error("set deadline", zap.Error(err))
			continue
		}

		buf, err := msg.Pack()
		if err != nil {
			d.log.Error("failed pack", zap.Error(err))
			continue
		}

		_, err = conn.Write(buf)
		if err != nil {
			d.log.Error("failed read packet", zap.Error(err))
			continue
		}

		buffer := bufferPool.Get().([]byte)
		_, err = conn.Read(buffer)

		if err != nil {
			d.log.Error("failed pack upstream packet", zap.Error(err))
			continue
		}

		res := dnsmessage.Message{}
		err = res.Unpack(buffer)
		if err != nil {
			bufferPool.Put(buffer[0:cap(buffer)])
			d.log.Error("failed unpack upstream packet", zap.Error(err))
			continue
		}

		bufferPool.Put(buffer[0:cap(buffer)])

		err = d.cache.Set(name, t, AnswerCache{
			Response:  &res,
			TimeToDie: unow + int64(d.minTTL(&res)),
		})

		if err != nil {
			d.log.Error("failed put cache", zap.Error(err))
		}

		return &res, nil
	}

	return nil, ErrServerUnReached
}

func (d *SimpleDNS) handleRequest(conn net.PacketConn, addr net.Addr, buffer []byte, length int) {
	defer func() {
		buffer = buffer[0:cap(buffer)]
		bufferPool.Put(buffer)
	}()
	m := dnsmessage.Message{}
	if err := m.Unpack(buffer[:length]); err != nil {
		d.log.Error("failed decode packet", zap.Error(err), zap.Binary("raw", buffer[0:]))
		return
	}

	dnsRes, err := d.resolve(&m)
	if err != nil {
		d.log.Error("failed resolve host", zap.Error(err))
		return
	}

	if err := d.write(conn, addr, dnsRes); err != nil {
		d.log.Error("failed write packet", zap.Error(err))
		return
	}

	if len(dnsRes.Answers) == 0 {
		go d.log.Warn("answer is empty",
			zap.String("type", m.Questions[0].Type.String()),
			zap.String("name", m.Questions[0].Name.String()),
		)
		return
	}

}

func (d *SimpleDNS) minTTL(dns *dnsmessage.Message) uint32 {
	min := uint32(math.MaxUint32)
	for _, ans := range dns.Answers {
		if min > ans.Header.TTL {
			min = ans.Header.TTL
		}
	}

	return min
}

func (d *SimpleDNS) write(conn net.PacketConn, addr net.Addr, answer *dnsmessage.Message) error {
	buf, err := answer.Pack()
	if err != nil {
		return err
	}

	if _, err := conn.WriteTo(buf, addr); err != nil {
		return err
	}
	return nil
}
