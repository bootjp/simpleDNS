package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/google/gopacket/layers"

	"github.com/google/gopacket"
)

var logger *log.Logger
var cache map[string]AnswerCache

type AnswerCache struct {
	Response  layers.DNS
	SolveTime time.Time
}

func init() {
	l := log.New(os.Stdout, "[simpleDNS] ", log.LstdFlags|log.Lmicroseconds|log.Lshortfile)
	logger = l

	cache = map[string]AnswerCache{}
}

func main() {

	fmt.Println("Server listening  at localhost:15353")
	conn, err := net.ListenPacket("udp", "localhost:15353")
	if err != nil {
		logger.Println(err)
	}
	defer conn.Close()
	buffer := make([]byte, 1500)
	for {
		length, addr, err := conn.ReadFrom(buffer)
		if err != nil {
			logger.Println(err)
			continue
		}

		p := gopacket.NewPacket(buffer[:length], layers.LayerTypeDNS, gopacket.Default)
		if p.ErrorLayer() != nil {
			logger.Println(p.ErrorLayer().Error())
			continue
		}

		dns := p.Layer(layers.LayerTypeDNS).(*layers.DNS)

		if len(dns.Questions) != 1 {
			logger.Println("Failed to parse 1 DNS answer")
			continue
		}

		name := string(dns.Questions[0].Name)
		cn, ok := cache[name]

		if ok {

			sub := time.Now().Unix() - cn.SolveTime.Unix()
			fmt.Println("sub", sub, cn.Response.Answers[0].TTL, sub >= int64(cn.Response.Answers[0].TTL))
			if sub >= int64(cn.Response.Answers[0].TTL) {
				logger.Println("stale cache")
				delete(cache, name)
				logger.Println("purged " + name)
				goto res
			}
			cn.Response.ID = dns.ID
			logger.Println("use cache")
			buf := gopacket.NewSerializeBuffer()
			opts := gopacket.SerializeOptions{FixLengths: true}
			err = gopacket.SerializeLayers(buf, opts, &cn.Response)
			if err != nil {
				logger.Println(err)
			}

			if _, err := conn.WriteTo(buf.Bytes(), addr); err != nil {
				logger.Println(err)
			}
			continue
		}
	res:

		logger.Println("request " + dns.Questions[0].Type.String() + " " + string(dns.Questions[0].Name) + " " + dns.Questions[0].Class.String())

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
		err = gopacket.SerializeLayers(buf, opts, answer)
		if err != nil {
			logger.Println(err)
		}

		if _, err := conn.WriteTo(buf.Bytes(), addr); err != nil {
			logger.Println(err)
		}
		cache[name] = AnswerCache{
			SolveTime: time.Now(),
			Response:  *answer,
		}
	}
}
