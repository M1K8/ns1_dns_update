package dnsUpdate

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/sys/windows/svc/debug"
	event "golang.org/x/sys/windows/svc/eventlog"
	"gopkg.in/ns1/ns1-go.v2/rest"
	api "gopkg.in/ns1/ns1-go.v2/rest"
	"gopkg.in/ns1/ns1-go.v2/rest/model/dns"
)

var ticker time.Ticker

//CheckConnection checks the connection to the api host to ensure we have an active connection
func CheckConnection(elog debug.Log) bool {
	_, err := http.Get("http://api.nsone.net")
	if err != nil {
		if elog != nil {
			elog.Info(1, "Not connected to the internet!")
		}
		return false
	}
	return true
}

// for testing / debugging
//func main() {
//	gracefulExit := make(chan bool)
//	hasFinished := make(chan bool)
//	catastrophicFailure := make(chan bool, 1)
//	event.InstallAsEventCreate("dnsUpdate", 20)
//
//	log, _ := event.Open("dnsUpdate")
//
//	Run(gracefulExit, hasFinished, catastrophicFailure, log, "", "")
//}

// Run is the main method of the service that occsionally checks if the local IP has changed, and updates the DNS record if necessary.
func Run(gracefulExit <-chan bool, hasFinished chan bool, catastrophicFailure chan<- bool, log *event.Log, domain string, apiKey string) {

	err := make(chan error, 5)

	ipUpdated := make(chan bool, 1)

	deleteOldIP := true

	everythingIsBroken := false

	httpClient := &http.Client{Timeout: time.Second * 10}
	client := api.NewClient(httpClient, api.SetAPIKey(apiKey))

	var zone *dns.Zone
	zone, httpres, clientErr := client.Zones.Get(domain) //zone

	if clientErr != nil {
		if strings.Contains(clientErr.Error(), "tcp") {
			log.Error(1, clientErr.Error())
			catastrophicFailure <- true
			return
		} else if httpres.StatusCode != 200 {
			log.Error(2, fmt.Sprintf("Error %d", httpres.StatusCode))
			everythingIsBroken = true
		}
	}

	//have an initial ticker to set things off
	ticker := time.NewTicker(10 * time.Millisecond)

	//incase we instantly want to exit, set the ipUpdated channel to true
	ipUpdated <- true

	// true = failed on getting, false = failed on setting.
	failType := make(chan bool, 1)

	//main loop
	for {
		if everythingIsBroken {
			hasFinished <- true
			catastrophicFailure <- false
			return
		}

		if !CheckConnection(log) {
			log.Error(1, "Internets dead")
			catastrophicFailure <- true
			return
		}
		select {
		case <-gracefulExit:
			//wait for the update to complete, or at least fail gracefully
			<-ipUpdated
			//alert the service we're ready to die
			hasFinished <- true
			return
		case loopError := <-err:
			if strings.Contains(loopError.Error(), "tcp") {
				//internets ded
				log.Warning(2, "Internet disconnected")
				catastrophicFailure <- true
				return
			} else if loopError != rest.ErrRecordMissing {
				log.Error(3, loopError.Error())
				<-ipUpdated
				catastrophicFailure <- false
				hasFinished <- true
				return
			} else {
				//if the error is record missing, that means the getOld failed, which is fine in this case; it means we need to create one anyway
				//signal we dont want to delete the record; if we delete something that doesnt exist itll cry
				deleteOldIP = false
			}

		default:
			select {
			case <-ticker.C:
				// as this is simply making REST requests, doesnt matter if the service dies halfway through, so no
				// need to worry about a graceful exit
				oldIP, newIP := GetOldNewIPs(zone, client, domain, err)

				select {
				case tempE := <-err:
					if strings.Contains(tempE.Error(), "tcp") {
						//internets ded
						log.Warning(2, "Internet disconnected")
						hasFinished <- true
						catastrophicFailure <- true
						return
					}
					log.Error(3, tempE.Error())
					hasFinished <- true
					return
				default:

					if oldIP != newIP {
						//we're doing work, so we aren't finished, clear the channel if needed to show this
						// this clears the channel if its populated, or continues if it isnt without any waiting
						select {
						case <-hasFinished:
						case <-ipUpdated:
						default:
						}
						//

						ChangeIP(ipUpdated, newIP, err, client, zone.String(), failType, domain, deleteOldIP)
						hasFinished <- true

						select {
						case failT := <-failType:
							e := <-err
							//if this is true, then it means the fetch of the old ip on the dns side failed, meaning we can try to force overwrite it with the new IP
							//if the gathering of the new IP failed, there is likely a larger issue at hand such as network connectivity, so exit
							if failT {
								log.Error(4, e.Error())
								continue
							}

							if strings.Contains(e.Error(), "tcp") {
								//internets ded
								log.Warning(2, "Internet disconnected")
								hasFinished <- true
								catastrophicFailure <- true
								return
							}
							//else
							//updating failed, something is wrong, fail gracefully
							log.Error(5, e.Error())
							everythingIsBroken = true
							continue

						default:
							ticker.Stop()
							ticker = time.NewTicker(2 * time.Hour) //Unlikely to have multiple changes within 2 hr
							deleteOldIP = true
							continue
						}

					} else {
						//we arent working but we are "finished", so mark the channel so we can cleanly die
						//...but only if the channel isnt already full i.e. first loop iteration was an ip change
						select {
						case <-hasFinished:
						default:
						}
						hasFinished <- false
						//

						ticker.Stop()
						ticker = time.NewTicker(2 * time.Minute) //Time randomly chosen by choosing an integer between 9 and 11
						continue
					}
				}

			default:
				continue
			}
		}
	}
}
