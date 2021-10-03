package core

import (
	"log"
	"net"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

type SimpleDNS struct {
	Log *log.Logger
}

type SimpleDNSServer interface {
	Run() int
}

func (d *SimpleDNS) Run() int {
	cacheRepo := NewCacheRepository(d.Log)

	d.Log.Println("Server listening  at localhost:15353")
	conn, err := net.ListenPacket("udp", "localhost:15353")
	if err != nil {
		d.Log.Println(err)
	}
	defer conn.Close()
	buffer := make([]byte, 512)
	for {
		length, addr, err := conn.ReadFrom(buffer)
		if err != nil {
			d.Log.Println(err)
			continue
		}

		p := gopacket.NewPacket(buffer[:length], layers.LayerTypeDNS, gopacket.Default)
		if p.ErrorLayer() != nil {
			d.Log.Println(p.ErrorLayer().Error())
			continue
		}

		dns := p.Layer(layers.LayerTypeDNS).(*layers.DNS)

		if len(dns.Questions) != 1 {
			d.Log.Println("Failed to parse 1 DNS answer")
			continue
		}

		name := string(dns.Questions[0].Name)
		c, err := cacheRepo.Get(name, dns.Questions[0].Type)
		if err == nil {
			d.Log.Println("use cache")
			c.ID = dns.ID
			buf := gopacket.NewSerializeBuffer()
			opts := gopacket.SerializeOptions{FixLengths: true}
			if err := gopacket.SerializeLayers(buf, opts, c); err != nil {
				d.Log.Println(err)
				continue
			}

			if _, err := conn.WriteTo(buf.Bytes(), addr); err != nil {
				d.Log.Println(err)
				continue
			}
			continue
		}

		d.Log.Println(err)
		d.Log.Println("request " + dns.Questions[0].Type.String() + " " + string(dns.Questions[0].Name) + " " + dns.Questions[0].Class.String())

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

		buf := gopacket.NewSerializeBuffer()
		opts := gopacket.SerializeOptions{FixLengths: true}
		if err := gopacket.SerializeLayers(buf, opts, answer); err != nil {
			d.Log.Println(err)
			continue
		}

		if _, err := conn.WriteTo(buf.Bytes(), addr); err != nil {
			d.Log.Println(err)
			continue
		}

		if err := cacheRepo.Set(name, layers.DNSTypeA, AnswerCache{
			Response:  answer,
			TimeToDie: time.Now().Unix() + int64(answer.Answers[0].TTL),
		}); err != nil {
			d.Log.Println(err)
			continue
		}

	}
}
