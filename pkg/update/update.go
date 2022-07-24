package update

import (
	"log"

	api "gopkg.in/ns1/ns1-go.v2/rest"
	"gopkg.in/ns1/ns1-go.v2/rest/model/dns"

	"errors"
	"time"
)

func deleteOld(client *api.Client, zone string, args string) error {
	client.Records.Delete(zone, args, "A")
	client.Records.Delete(zone, args, "SRV")

	//busy wait until the delete has propogated
	ticker := time.NewTimer(30 * time.Second)
	for {
		select {
		case <-ticker.C:
			return errors.New("delete hasnt propogated")

		default:
			a, _, _ := client.Records.Get(zone, args, "A")
			srv, _, _ := client.Records.Get(zone, args, "SRV")
			if a != nil && srv != nil {
				log.Println("Found A and SRV Records: " + a.Domain)
				return nil
			}
		}
	}
}

// ChangeIP changes the A and SRV records of a given client zone
func ChangeIP(newIP string, client *api.Client, zone string, args string, deleteOldIP bool) error {

	var err error
	newA := dns.NewRecord(zone, args, "A")

	newSRV := dns.NewRecord(zone, args, "SRV")

	newA.TTL = 600

	newSRV.TTL = 600

	newA.AddAnswer(dns.NewAv4Answer(newIP))
	newSRV.AddAnswer(dns.NewSRVAnswer(0, 0, 11774, args))

	if deleteOldIP {
		err = deleteOld(client, zone, args)
		if err != nil {
			return err
		}
	}
	_, errACreate := client.Records.Create(newA)
	_, errSRVCreate := client.Records.Create(newSRV)

	if errACreate != nil {
		return errACreate

	}
	if errSRVCreate != nil {
		return errSRVCreate

	}
	return nil
}
