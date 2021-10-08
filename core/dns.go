package core

import (
	"log"
	"net"
	"time"

	"github.com/miekg/dns"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

type SimpleDNS struct {
	log   *log.Logger
	cache *CacheRepository
}

func NewSimpleDNSServer(logger *log.Logger) (SimpleDNSServer, error) {
	cr, err := NewCacheRepository(logger)
	if err != nil {
		return nil, err
	}
	return &SimpleDNS{
		log:   logger,
		cache: cr,
	}, nil
}

type SimpleDNSServer interface {
	Run() int
}

func (d *SimpleDNS) Run() int {
	d.log.Println("Server listening  at localhost:15353")

	conn, err := net.ListenPacket("udp", "localhost:15353")
	if err != nil {
		d.log.Println(err)
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

func (d *SimpleDNS) resolve(name []byte, t layers.DNSType) (*layers.DNS, error) {
	m := &dns.Msg{}
	m.SetQuestion(dns.Fqdn(string(name)), uint16(t))

	c := dns.Client{}
	r, _, err := c.Exchange(m, "1.1.1.1:53")
	if err != nil {
		d.log.Fatalln(err)
		return nil, err
	}

	rr, err := r.Pack()
	if err != nil {
		d.log.Fatalln(err)
	}

	res := layers.DNS{}

	err = res.DecodeFromBytes(rr, gopacket.NilDecodeFeedback)
	if err != nil {
		d.log.Fatalln(err)

	}

	return &res, nil
}

func (d *SimpleDNS) handleRequest(conn net.PacketConn, length int, addr net.Addr, buffer []byte) {

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
		for i, _ := range c.Response.Answers {
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
