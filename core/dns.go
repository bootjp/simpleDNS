package core

import (
	"errors"
	"math"
	"net"
	"time"

	"go.uber.org/zap/zapcore"

	"go.uber.org/zap"

	"github.com/jaytaylor/go-hostsfile"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/miekg/dns"
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

const DnsUdpMaxPacketSize = 576

func (d *SimpleDNS) Run() error {
	if d.Config.UseHosts {
		hostsMap, err := hostsfile.ParseHosts(hostsfile.ReadHostsFile())
		if err != nil {
			return err
		}

		for s, strings := range hostsMap {
			for _, hostname := range strings {

				b := &dnsmessage.AResource{}
				copy(b.A[:], s)

				h, err := dnsmessage.NewName(hostname + ".")
				if err != nil {
					d.log.Error("failed create hosts record", zap.Error(err))
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

	buffer := make([]byte, DnsUdpMaxPacketSize)
	for {
		length, addr, err := conn.ReadFrom(buffer)
		if err != nil {
			d.log.Error(err.Error())
			continue
		}
		go d.handleRequest(conn, length, addr, buffer)
	}
}

var ErrServerUnReached = errors.New("name servers all unreached")

func (d *SimpleDNS) resolve(name *dnsmessage.Name, t *dnsmessage.Type) (*dnsmessage.Message, error) {

	// RFC8482 4.3
	var fallbackAny bool
	if *t == dnsmessage.TypeALL {
		*t = dnsmessage.TypeA
		fallbackAny = true
	}

	if hosts, ok := d.staticHost[name.String()[:(len(name.String())-1)]]; ok {
		msg := &dnsmessage.Message{
			Header: dnsmessage.Header{
				Response:      true,
				Authoritative: true,
				RCode:         dnsmessage.RCodeSuccess,
			},
		}

		msg.Answers = hosts

		return msg, nil
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
		return c.Response, nil
	}

	go d.log.Info("request",
		zap.String("type", t.String()),
		zap.String("name", name.String()),
	)

	m := &dns.Msg{}
	m.SetQuestion(name.String(), uint16(*t))

	c := dns.Client{}

	// TODO: refactor retry logic
	for attempt, server := range d.Server {
		ch := make(chan *dnsmessage.Message, 1)
		go func() {
			r, _, err := c.Exchange(m, server.String())
			if err != nil {
				d.log.Warn(err.Error())
				ch <- nil
				return
			}

			rr, err := r.Pack()
			if err != nil {
				d.log.Warn(err.Error())
				ch <- nil
				return
			}

			res := dnsmessage.Message{}
			err = res.Unpack(rr)
			if err != nil {
				d.log.Warn(err.Error())
				ch <- nil
				return
			}
			ch <- &res
		}()

		select {
		case m := <-ch:
			if m != nil {
				if fallbackAny {
					for i := range m.Questions {
						m.Questions[i].Type = dnsmessage.TypeALL
					}
				}
				err := d.cache.Set(name, t, AnswerCache{
					Response:  m,
					TimeToDie: unow + int64(d.minTTL(m)),
				})
				if err != nil {
					d.log.Warn(err.Error())
				}

				return m, nil
			}

			continue
		case <-time.After(1 * time.Second):
			if attempt > 0 {
				return nil, ErrServerUnReached
			}
			d.log.Error("retry switching secondary name server")
		}

	}

	// this is unreached
	return nil, ErrServerUnReached
}

func (d *SimpleDNS) handleRequest(conn net.PacketConn, length int, addr net.Addr, buffer []byte) {

	var p dnsmessage.Parser
	header, err := p.Start(buffer[:length])

	if err != nil {
		d.log.Error("failed decode packet", zap.Error(err), zap.Binary("raw", buffer))
		return
	}

	var name *dnsmessage.Name
	var qType *dnsmessage.Type
	for {
		q, err := p.Question()
		if err == dnsmessage.ErrSectionDone {
			break
		}
		if err != nil {
			d.log.Error("failed decode packet", zap.Error(err))
			break
		}

		name = &q.Name
		qType = &q.Type
		break
	}

	// todo move resolve
	// validate invalid query
	if name == nil || qType == nil {
		m := &dnsmessage.Message{}
		m.ID = header.ID

		_ = d.write(conn, addr, m)
		return
	}

	dnsRes, err := d.resolve(name, qType)
	if err != nil {
		d.log.Error(err.Error())
		return
	}

	dnsRes.ID = header.ID

	if err := d.write(conn, addr, dnsRes); err != nil {
		d.log.Error(err.Error())
		return
	}

	if len(dnsRes.Answers) == 0 {
		go d.log.Warn("answer is empty",
			zap.String("type", qType.String()),
			zap.String("name", name.String()),
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
