package dnsUpdate

import (
	api "gopkg.in/ns1/ns1-go.v2/rest"
	"gopkg.in/ns1/ns1-go.v2/rest/model/dns"
)

var hasError bool

func deleteOld(client *api.Client, zone string, errChan chan<- error, failType chan<- bool, args string) {
	hasError = false
	_, errA := client.Records.Delete(zone, args, "A")
	_, errSRV := client.Records.Delete(zone, args, "SRV")

	if errA != nil {
		errChan <- errA
		hasError = true
	} else if errSRV != nil {
		errChan <- errSRV
		hasError = true
	}

	if hasError {
		//either a network error - the update will also fail; or the records dont exist - that is fine
		failType <- true
		return
	}

	//busy wait until the delete has propogated
	for {
		a, _, _ := client.Records.Get(zone, args, "A")
		srv, _, _ := client.Records.Get(zone, args, "SRV")
		if a == nil && srv == nil {
			break
		}
	}
}

// ChangeIP changes the A and SRV records of a given client zone
func ChangeIP(ipUpdated chan<- bool, newIP string, errChan chan<- error, client *api.Client, zone string, failType chan<- bool, args string, deleteOldIP bool) {

	newA := dns.NewRecord(zone, args, "A")

	newSRV := dns.NewRecord(zone, args, "SRV")

	newA.TTL = 600

	newSRV.TTL = 600

	newA.AddAnswer(dns.NewAv4Answer(newIP))
	newSRV.AddAnswer(dns.NewSRVAnswer(0, 0, 11774, args))

	if deleteOldIP {
		deleteOld(client, zone, errChan, failType, args)
	}
	_, errACreate := client.Records.Create(newA)
	_, errSRVCreate := client.Records.Create(newSRV)

	if errACreate != nil {
		errChan <- errACreate
		failType <- false
	} else if errSRVCreate != nil {
		errChan <- errSRVCreate
		failType <- false
	} else {
		//we're done here, mark this as "done" to allow for a graceful exit
		ipUpdated <- true
	}
}
