package dnsUpdate

import (
	"bytes"
	"errors"
	"io/ioutil"
	"net/http"
	"time"

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

func getOldIP(oldIPChan chan<- string, zone *dns.Zone, client *api.Client, errChan chan<- error, args string) {
	oldIPZ, httpres, getErr := client.Records.Get(zone.String(), args, "A")

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
func GetOldNewIPs(zone *dns.Zone, client *api.Client, args string, errChan chan error) (string, string) {

	oldIPChan := make(chan string)
	newIPChan := make(chan string)

	oldIPErrorChan := make(chan error, 1)

	go getNewIP(newIPChan, errChan)

	go getOldIP(oldIPChan, zone, client, oldIPErrorChan, args)

	// has errors that need to be manually handled, so lets assess this first
	oldIP := <-oldIPChan

	//have a 10s timeout for getting new ip
	timer := time.NewTimer(10 * time.Second)

	select {
	case <-oldIPErrorChan:
		select {
		case err := <-errChan:
			//rethrow the error
			errChan <- err
			//we have an old ip error, so keep looping until things hopefully get back to normal
			return "", ""

		default:
			for {
				select {
				case newIP := <-newIPChan:
					return "", newIP
				case <-timer.C:
					//we have a new ip error (most likely a timeout), so keep looping until things hopefully get back to normal
					return "", ""

				}
			}

		}
	default:
		for {
			select {
			case newIP := <-newIPChan:
				return oldIP, newIP
			case <-timer.C:
				//we have a new ip error (most likely a timeout), so keep looping until things hopefully get back to normal
				return "", ""

			}
		}
	}
}
