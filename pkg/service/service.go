package service

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/m1k8/DNSUpdate/pkg/compare"
	"github.com/m1k8/DNSUpdate/pkg/update"
	"gopkg.in/ns1/ns1-go.v2/rest"
	api "gopkg.in/ns1/ns1-go.v2/rest"
	"gopkg.in/ns1/ns1-go.v2/rest/model/dns"
)

type Svc struct {
	zone   *dns.Zone
	domain string
	client *api.Client
	ticker time.Ticker
	done   chan bool
}

func NewSvc(a, d string) *Svc {

	httpClient := &http.Client{Timeout: time.Second * 10}
	client := api.NewClient(httpClient, api.SetAPIKey(a))
	zone, httpres, clientErr := client.Zones.Get(d)

	if clientErr != nil {
		if strings.Contains(clientErr.Error(), "tcp") {
			log.Println(clientErr.Error())
			return nil
		} else if httpres.StatusCode != 200 {
			log.Println(fmt.Sprintf("Error %d", httpres.StatusCode))
			return nil
		}
	}

	return &Svc{
		ticker: *time.NewTicker(30 * time.Minute),
		done:   make(chan bool),
		zone:   zone,
		domain: d,
		client: client,
	}
}

func (s *Svc) Start() {
	doDelete := true
	for {
		select {
		case <-s.ticker.C:
			old, new, err := compare.GetOldNewIPs(s.zone, s.client, s.domain)
			if err != nil {
				if err == rest.ErrRecordMissing {
					doDelete = false
				}
				log.Println("Error getting IP(s) - " + err.Error())
				continue
			}

			if old != new {
				err = update.ChangeIP(new, s.client, s.zone.String(), s.domain, doDelete)
				if err != nil {
					log.Println("Error updating IP - " + err.Error())
				}
			}

		case <-s.done:
			log.Println("Finishing!")
			return
		}
	}

}

func (s *Svc) Stop() {
	s.done <- true
}
