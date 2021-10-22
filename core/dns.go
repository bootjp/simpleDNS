package core

import (
	"errors"
	"log"
	"math"
	"net"
	"time"

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
	Hosts      map[string][]string
}

func NewSimpleDNSServer(c *Config, logger *zap.Logger) (SimpleDNSServer, error) {
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
		Hosts:      map[string][]string{},
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
			for _, s2 := range strings {
				d.Hosts[s2] = append(d.Hosts[s2], s)
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

	// reduce syscall using fixed unix timestamp
	unow := time.Now().Unix()

	var p dnsmessage.Parser
	header, err := p.Start(buffer[:length])

	if err != nil {
		d.log.Warn(err.Error())
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
			log.Println(err)
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

	// RFC8482 4.3
	var fallbackAny bool
	if *qType == dnsmessage.TypeALL {
		*qType = dnsmessage.TypeA
		fallbackAny = true
	}

	if hosts, ok := d.Hosts[name.String()[:(len(name.String())-1)]]; ok {
		msg := &dnsmessage.Message{
			Header: dnsmessage.Header{Response: true, Authoritative: true, RCode: dnsmessage.RCodeSuccess},
		}

		for _, host := range hosts {
			h, err := dnsmessage.NewName(name.String())
			if err != nil {
				d.log.Warn("cant create host struct", zap.String("host", host), zap.Error(err))
				return
			}

			b := &dnsmessage.AResource{}
			copy(b.A[:], host)
			msg.Answers = append(msg.Answers, dnsmessage.Resource{
				Header: dnsmessage.ResourceHeader{
					Name:  h,
					Type:  dnsmessage.TypeA,
					Class: dnsmessage.ClassINET,
				},
				Body: b,
			})
		}

		msg.ID = header.ID
		if err := d.write(conn, addr, msg); err != nil {
			d.log.Error("failed write packet", zap.Error(err))
		}

		return
	}

	c, ok := d.cache.Get(unow, name, qType)
	if ok {
		d.log.Info("use cache",
			zap.String("type", qType.String()),
			zap.String("name", name.String()),
		)

		c.Response.ID = header.ID
		for i := range c.Response.Answers {
			c.Response.Answers[i].Header.TTL = uint32(c.TimeToDie - unow)
		}
		if err := d.write(conn, addr, c.Response); err != nil {
			d.log.Error(err.Error())
		}
		return
	}

	go d.log.Info("request",
		zap.String("type", qType.String()),
		zap.String("name", name.String()),
	)

	dnsRes, err := d.resolve(name, qType)
	if err != nil {
		d.log.Error(err.Error())
		return
	}

	dnsRes.ID = header.ID

	if fallbackAny {
		for i := range dnsRes.Questions {
			dnsRes.Questions[i].Type = dnsmessage.TypeALL
		}
	}

	if err := d.write(conn, addr, dnsRes); err != nil {
		d.log.Error(err.Error())
		return
	}

	if len(dnsRes.Answers) == 0 {
		d.log.Warn("answer is empty",
			zap.String("type", qType.String()),
			zap.String("name", name.String()),
		)
		return
	}

	err = d.cache.Set(name, qType, AnswerCache{
		Response:  dnsRes,
		TimeToDie: unow + int64(d.minTTL(dnsRes)),
	})
	if err != nil {
		d.log.Warn(err.Error())
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
