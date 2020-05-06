# **ns1_dns_update**

A Window service written in Go to detect and update the A record for a domain pointing to a locally hosted server

## **Building**
*Requires Go >v1.13*

**go build** in the root directory


## **Usage**

### From an elevated PowerShell terminal:

* **DNSUpdate.exe *install*** - installs the service
* **DNSUpdate.exe *start <domain> <api key>*** - starts the service

* **DNSUpdate.exe *stop*** - stops the servive
* **DNSUpdate.exe *remove*** - uninstalls the service


Made by (*heavily*) using the <ins>**gopkg.in/ns1/ns1-go.v2**</ins> and <ins>**golang.org/x/sys/windows/svc**</ins> packages.
