package core

import (
	"log"
	"net"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

type SimpleDNS struct {
	log   *log.Logger
	cache *Repository
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

func (d *SimpleDNS) handleRequest(conn net.PacketConn, length int, addr net.Addr, buffer []byte) {

	unow := time.Now().Unix()

	p := gopacket.NewPacket(buffer[:length], layers.LayerTypeDNS, gopacket.Default)
	if p.ErrorLayer() != nil {
		d.log.Println(p.ErrorLayer().Error())
		return
	}

	dns := p.Layer(layers.LayerTypeDNS).(*layers.DNS)

	if len(dns.Questions) != 1 {
		d.log.Println("Failed to parse 1 DNS answer")
		return
	}

	name := &dns.Questions[0].Name

	c, ok := d.cache.Get(unow, *name, dns.Questions[0].Type)
	if ok {
		d.log.Println("use cache")
		c.Response.ID = dns.ID
		for i, _ := range c.Response.Answers {
			c.Response.Answers[i].TTL = uint32(c.TimeToDie - unow)
		}
		if err := d.write(conn, addr, c.Response); err != nil {
			d.log.Println(err)
		}
		return
	}

	d.log.Println("request " + dns.Questions[0].Type.String() + " " + string(dns.Questions[0].Name) + " " + dns.Questions[0].Class.String())

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

	if err := d.write(conn, addr, answer); err != nil {
		d.log.Println(err)
	}

	if err := d.cache.Set(*name, layers.DNSTypeA, AnswerCache{
		Response:  answer,
		TimeToDie: unow + int64(answer.Answers[0].TTL),
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
