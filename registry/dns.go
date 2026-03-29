package registry

import (
	"context"
	"fmt"
	"io"
	"log"

	"codeberg.org/miekg/dns"
	"codeberg.org/miekg/dns/dnsutil"
)

func forwardDNSRequest(ctx context.Context, req *dns.Msg, r *ServiceRegistry, writer dns.ResponseWriter) {
	client := new(dns.Client)
	msgUpstream, _, err := client.Exchange(ctx, req, "udp", r.cfg.DNS.Upstream+":53")
	if err != nil {
		log.Printf("Forwarding failed: %v", err)
		return
	}
	msgUpstream.Pack()
	io.Copy(writer, msgUpstream)
}

func (r *ServiceRegistry) handleDNSQuestion(
	ctx context.Context,
	q dns.RR,
	req *dns.Msg,
	writer dns.ResponseWriter,
	msg *dns.Msg) {
	if q.Header().Class == dns.TypeA {
		service, exists := r.dnsMap[q.Header().Name]
		if exists {
			// preload on dns lookup
			if service.isContainerService() {
				service.mu.Lock()
				if service.state != StateStarted {
					r.startContainerUnsafe(service)
				}
				service.mu.Unlock()
			}
			// respond
			rr, err := dns.New(fmt.Sprintf("%s 60 IN A %s", q.Header().Name, service.proxyIp))
			if err == nil {
				msg.Answer = append(msg.Answer, rr)
			}
			return
		}
	}
	fmt.Println("Unknown name " + q.Header().Name + ", forwarding now")
	forwardDNSRequest(ctx, req, r, writer)
}

func (r *ServiceRegistry) startDNS() {
	dns.HandleFunc(".", func(ctx context.Context, writer dns.ResponseWriter, req *dns.Msg) {
		msg := req.Copy()
		dnsutil.SetReply(msg, req)
		for _, q := range req.Question {
			r.handleDNSQuestion(ctx, q, req, writer, msg)
		}
		msg.Pack()
		io.Copy(writer, msg)
	})
	log.Fatal((&dns.Server{Addr: ":53", Net: "udp"}).ListenAndServe())
}
