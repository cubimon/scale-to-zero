package registry

import (
	"context"
	"fmt"
	"io"
	"log"

	"codeberg.org/miekg/dns"
	"codeberg.org/miekg/dns/dnsutil"
)

func (r *ServiceRegistry) startDNS() {
	dns.HandleFunc(".", func(c context.Context, w dns.ResponseWriter, req *dns.Msg) {
		m := req.Copy()
		dnsutil.SetReply(m, req)
		for _, q := range req.Question {
			if q.Header().Class != dns.TypeA {
				fmt.Println("Not Type A request")
				continue
			}
			service, exists := r.dnsMap[q.Header().Name]
			if !exists {
				fmt.Println("Unknown name " + q.Header().Name)
				continue
			}
			// preload on dns lookup
			service.mu.Lock()
			if service.state != StateStarted {
				r.startContainerUnsafe(service)
			}
			service.mu.Unlock()
			rr, err := dns.New(fmt.Sprintf("%s 60 IN A %s", q.Header().Name, service.ProxyIp))
			if err == nil {
				m.Answer = append(m.Answer, rr)
			}
		}
		m.Pack()
		io.Copy(w, m)
	})
	log.Fatal((&dns.Server{Addr: ":53", Net: "udp"}).ListenAndServe())
}
