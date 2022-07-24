package compare

import (
	"bytes"
	"errors"
	"io/ioutil"
	"log"
	"net/http"

	api "gopkg.in/ns1/ns1-go.v2/rest"
	"gopkg.in/ns1/ns1-go.v2/rest/model/dns"
)

func getNewIP(newIPChan chan<- string, errChan chan<- error) {
	rsp, newIPerr := http.Get("https://api.ipify.org")
	if newIPerr != nil {
		errChan <- newIPerr
		return
	}
	defer rsp.Body.Close()

	buf, ipParseErr := ioutil.ReadAll(rsp.Body)
	if ipParseErr != nil {
		errChan <- ipParseErr
		return
	}
	newIPChan <- string(bytes.TrimSpace(buf))
}

func getOldIP(oldIPChan chan<- string, zone *dns.Zone, client *api.Client, errChan chan<- error, domain string) {
	oldIPZ, httpres, getErr := client.Records.Get(zone.String(), domain, "A")

	if getErr != nil {
		oldIPChan <- ""
		errChan <- getErr
		return
	} else if httpres.StatusCode != 200 {
		errChan <- errors.New("response not OK")
		oldIPChan <- ""
		return
	} else {
		oldIP := oldIPZ.Answers[0].Rdata[0]
		oldIPChan <- oldIP
		return
	}
}

// GetOldNewIPs fetches the current local IP, and IP stored in the A record of the DNS zone
func GetOldNewIPs(zone *dns.Zone, client *api.Client, domain string) (string, string, error) {

	errChan := make(chan error)
	oldIPChan := make(chan string)
	newIPChan := make(chan string)

	go getNewIP(newIPChan, errChan)

	go getOldIP(oldIPChan, zone, client, errChan, domain)

	// has errors that need to be manually handled, so lets assess this first
	oldIP := <-oldIPChan
	newIP := <-newIPChan

	select {
	case e := <-errChan:
		log.Println(e)
		return "", "", e
	default:
		return oldIP, newIP, nil
	}
}
