package main

import (
	"fmt"
	"log"
	"net"
	"os"

	"github.com/google/gopacket/layers"

	"github.com/google/gopacket"
)

var logger *log.Logger

func init() {
	l := log.New(os.Stdout, "[Hello] ", log.LstdFlags|log.Lmicroseconds|log.Lshortfile)
	logger = l
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
			//logger.Println(p.ErrorLayer().Error())
			continue
		}

		dns := p.Layer(layers.LayerTypeDNS).(*layers.DNS)

		if len(dns.Questions) != 1 {
			logger.Println("Failed to parse 1 DNS answer")
			continue
		}

		fmt.Println("request ")
		fmt.Println(dns.Questions[0].Type.String() + " " + string(dns.Questions[0].Name) + " " + dns.Questions[0].Class.String())

		answer := &layers.DNS{ID: dns.ID, QR: true, OpCode: layers.DNSOpCodeQuery, AA: true, RD: true, RA: true}
		answer.Answers = append(dns.Answers,
			layers.DNSResourceRecord{
				Name:  []byte("bootjp.me"),
				Type:  layers.DNSTypeCNAME,
				Class: layers.DNSClassIN,
				TTL:   1024,
				CNAME: []byte("twitter.com/bootjp"),
			})

		buf := gopacket.NewSerializeBuffer()
		opts := gopacket.SerializeOptions{FixLengths: true}
		err = gopacket.SerializeLayers(buf, opts, answer)
		if err != nil {
			logger.Fatal(err)

		}

		conn.WriteTo(buf.Bytes(), addr)
	}
}
