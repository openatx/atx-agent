package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/franela/goreq"
	"github.com/miekg/dns"
)

func dnsLookupHost(hostname string) (ip net.IP, err error) {
	for _, dnsServer := range []string{"114.114.114.114", "8.8.4.4"} {
		ip, err = dnsLookupHostWithDNS(hostname, dnsServer)
		if err == nil {
			return
		}
	}
	defaultDNSResolver := getProperty("net.dns1")
	if defaultDNSResolver == "" {
		return
	}
	return dnsLookupHostWithDNS(hostname, defaultDNSResolver)
}

func dnsLookupHostWithDNS(hostname string, dnsServer string) (ip net.IP, err error) {
	if !strings.HasSuffix(hostname, ".") {
		hostname += "."
	}
	m1 := new(dns.Msg)
	m1.Id = dns.Id()
	m1.RecursionDesired = true
	m1.Question = []dns.Question{
		{hostname, dns.TypeA, dns.ClassINET},
	}
	c := new(dns.Client)
	c.SingleInflight = true

	in, _, err := c.Exchange(m1, dnsServer+":53")
	if err != nil {
		return nil, err
	}
	if len(in.Answer) == 0 {
		return nil, errors.New("dns return empty answer")
	}
	log.Println("dns:"+dnsServer, in.Answer[0])
	if t, ok := in.Answer[0].(*dns.A); ok {
		return t.A, nil
	}
	if t, ok := in.Answer[0].(*dns.CNAME); ok {
		return dnsLookupHostWithDNS(t.Target, dnsServer)
	}
	return nil, errors.New("dns resolve failed: " + hostname)
}

func init() {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		DualStack: true,
	}

	// manualy dns resolve
	newDialContext := func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, _ := net.SplitHostPort(addr)
		if net.ParseIP(host) == nil {
			ip, err := dnsLookupHost(host)
			if err != nil {
				return nil, err
			}
			addr = ip.String() + ":" + port
		}
		return dialer.DialContext(ctx, network, addr)
	}
	http.DefaultTransport.(*http.Transport).DialContext = newDialContext
	goreq.DefaultTransport.(*http.Transport).DialContext = newDialContext
}
