package core

import (
	"errors"
	"log"
	"math"
	"net"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/miekg/dns"
)

type SimpleDNS struct {
	ListenAddr *net.UDPAddr
	Server     [2]*NameServer
	log        *log.Logger
	cache      *CacheRepository
}

func NewSimpleDNSServer(c *Config, logger *log.Logger) (SimpleDNSServer, error) {
	cr, err := NewCacheRepository(c.MaxCacheSize, logger)
	if err != nil {
		return nil, err
	}

	return &SimpleDNS{
		ListenAddr: c.ListenAddr,
		Server:     c.NameServer,
		log:        logger,
		cache:      cr,
	}, nil
}

type SimpleDNSServer interface {
	Run() error
}

func (d *SimpleDNS) Run() error {
	d.log.Println("Server listening at 0.0.0.0:15353")

	conn, err := net.ListenPacket("udp", d.ListenAddr.String())
	if err != nil {
		return err
	}
	defer func() {
		_ = conn.Close()
	}()

	buffer := make([]byte, 1000)
	for {
		length, addr, err := conn.ReadFrom(buffer)
		if err != nil {
			d.log.Println(err)
			continue
		}
		d.handleRequest(conn, length, addr, buffer)
	}
}

var ErrServerUnReached = errors.New("name servers all unreached")

func (d *SimpleDNS) resolve(name *dnsmessage.Name, t *dnsmessage.Type) (*dnsmessage.Message, error) {
	m := &dns.Msg{}
	m.SetQuestion(name.String(), uint16(*t))

	c := dns.Client{}

	for attempt, server := range d.Server {
		ch := make(chan *dnsmessage.Message, 1)
		go func() {
			r, _, err := c.Exchange(m, server.String())
			if err != nil {
				d.log.Println(err)
				ch <- nil
				return
			}

			rr, err := r.Pack()
			if err != nil {
				d.log.Println(err)
				ch <- nil
				return
			}

			res := dnsmessage.Message{}
			err = res.Unpack(rr)
			if err != nil {
				d.log.Println(err)
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
			d.log.Println("retry switching secondary name server")
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
		d.log.Println(err)
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

	var fallbackAny bool

	if qType != nil && *qType == dnsmessage.TypeALL {
		*qType = dnsmessage.TypeA
		fallbackAny = true
	}

	c, ok := d.cache.Get(unow, name, qType)
	if ok {
		d.log.Println("use cache")
		c.Response.ID = header.ID
		for i := range c.Response.Answers {
			c.Response.Answers[i].Header.TTL = uint32(c.TimeToDie - unow)
		}
		if err := d.write(conn, addr, c.Response); err != nil {
			d.log.Println(err)
		}
		return
	}

	d.log.Println("request " + qType.String() + " " + name.String())

	dnsRes, err := d.resolve(name, qType)
	if err != nil {
		d.log.Println(err)
		return
	}

	dnsRes.ID = header.ID

	if fallbackAny {
		for i := range dnsRes.Questions {
			dnsRes.Questions[i].Type = dnsmessage.TypeALL
		}
	}

	if err := d.write(conn, addr, dnsRes); err != nil {
		d.log.Println(err)
		return
	}

	if len(dnsRes.Answers) == 0 {
		d.log.Println("answer is empty")
		return
	}

	err = d.cache.Set(name, qType, AnswerCache{
		Response:  dnsRes,
		TimeToDie: unow + int64(d.minTTL(dnsRes)),
	})
	if err != nil {
		d.log.Println(err)
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
