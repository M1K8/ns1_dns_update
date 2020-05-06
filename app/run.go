package dnsUpdate

import (
	"errors"
	"net/http"
	"time"

	event "golang.org/x/sys/windows/svc/eventlog"
	"gopkg.in/ns1/ns1-go.v2/rest"
	api "gopkg.in/ns1/ns1-go.v2/rest"
)

var ticker time.Ticker

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
func Run(gracefulExit <-chan bool, hasFinished chan bool, catastrophicFailure chan<- bool, log *event.Log, domain string, apiKey string) error {

	err := make(chan error, 5)

	ipUpdated := make(chan bool, 1)

	deleteOldIP := true

	httpClient := &http.Client{Timeout: time.Second * 10}
	client := api.NewClient(httpClient, api.SetAPIKey(apiKey))
	zone, httpres, clientErr := client.Zones.Get(domain) //zone

	if clientErr != nil {
		err <- clientErr
		log.Error(1, clientErr.Error())
	} else if httpres.StatusCode != 200 {
		err <- errors.New("response not OK")
		log.Error(2, "Cannot get current IP")
	}
	everythingIsBroken := false

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
			catastrophicFailure <- true
			return nil
		}
		select {
		case <-gracefulExit:
			//wait for the update to complete, or at least fail gracefully
			<-ipUpdated
			//alert the service we're ready to die
			hasFinished <- true
			return nil
		case loopError := <-err:
			// if the error is record missing, that means the getOld failed, which is fine in this case; it means we need to create one anyway
			if loopError != rest.ErrRecordMissing {
				<-ipUpdated
				hasFinished <- true
				return nil
			}

			//signal we dont want to delete the record; if we delete something that doesnt exist itll cry
			deleteOldIP = false

		default:

			select {
			case <-ticker.C:
				// as this is simply making REST requests, doesnt matter if the service dies halfway through, so no
				// need to worry about a graceful exit
				oldIP, newIP := GetOldNewIPs(zone, client, domain, err)

				select {
				case tempE := <-err:
					log.Error(3, tempE.Error())
					hasFinished <- true
					return nil
				default:

					if oldIP != newIP {
						//we're doing work, so we aren't finished, clear the channel if needed to show this
						// this clears the channel if its populated, or continues if it isnt without any waiting :)
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
							//if this is true, then it means the fetch of the old ip failed, meaning we can try to force overwrite it with the new IP
							//if the gathering of the new IP failed, there is likely a larger issue at hand such as network connectivity, so exit
							if failT {
								log.Error(4, e.Error())
								continue
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
