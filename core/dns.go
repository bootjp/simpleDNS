package core

import (
	"errors"
	"log"
	"net"
	"strconv"
	"time"

	"github.com/miekg/dns"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

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

type SimpleDNS struct {
	Server [2]*NameServer
	log    *log.Logger
	cache  *CacheRepository
}

func NewSimpleDNSServer(ip [2]*NameServer, logger *log.Logger) (SimpleDNSServer, error) {
	cr, err := NewCacheRepository(logger)
	if err != nil {
		return nil, err
	}

	return &SimpleDNS{
		Server: ip,
		log:    logger,
		cache:  cr,
	}, nil
}

type SimpleDNSServer interface {
	Run() error
}

func (d *SimpleDNS) Run() error {
	d.log.Println("Server listening  at localhost:15353")

	conn, err := net.ListenPacket("udp", "localhost:15353")
	if err != nil {
		return err
	}
	defer func() {
		_ = conn.Close()
	}()

	buffer := make([]byte, 512)
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

func (d *SimpleDNS) resolve(name []byte, t layers.DNSType) (*layers.DNS, error) {
	m := &dns.Msg{}
	m.SetQuestion(dns.Fqdn(string(name)), uint16(t))

	c := dns.Client{}

	for attempt, server := range d.Server {
		ch := make(chan *layers.DNS, 1)
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

			res := layers.DNS{}
			err = res.DecodeFromBytes(rr, gopacket.NilDecodeFeedback)
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

	p := gopacket.NewPacket(buffer[:length], layers.LayerTypeDNS, gopacket.Default)
	if p.ErrorLayer() != nil {
		d.log.Println(p.ErrorLayer().Error())
		return
	}

	dnsReq := p.Layer(layers.LayerTypeDNS).(*layers.DNS)

	if len(dnsReq.Questions) != 1 {
		d.log.Println("Failed to parse 1 DNS answer")
		return
	}

	name := &dnsReq.Questions[0].Name

	c, ok := d.cache.Get(unow, *name, dnsReq.Questions[0].Type)
	if ok {
		d.log.Println("use cache")
		c.Response.ID = dnsReq.ID
		for i := range c.Response.Answers {
			c.Response.Answers[i].TTL = uint32(c.TimeToDie - unow)
		}
		if err := d.write(conn, addr, c.Response); err != nil {
			d.log.Println(err)
		}
		return
	}

	d.log.Println("request " + dnsReq.Questions[0].Type.String() + " " + string(dnsReq.Questions[0].Name) + " " + dnsReq.Questions[0].Class.String())

	dnsRes, err := d.resolve(*name, dnsReq.Questions[0].Type)
	if err != nil {
		d.log.Println(err)
		return
	}

	dnsRes.ID = dnsReq.ID

	if err := d.write(conn, addr, dnsRes); err != nil {
		d.log.Println(err)
		return
	}

	if err := d.cache.Set(*name, layers.DNSTypeA, AnswerCache{
		Response:  dnsRes,
		TimeToDie: unow + int64(dnsRes.Answers[0].TTL),
	}); err != nil {
		d.log.Println(err)
		return
	}
}

func (d *SimpleDNS) write(conn net.PacketConn, addr net.Addr, answer *layers.DNS) error {
	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{FixLengths: true}

	if err := gopacket.SerializeLayers(buf, opts, answer); err != nil {
		return err
	}

	if _, err := conn.WriteTo(buf.Bytes(), addr); err != nil {
		return err
	}
	return nil
}
