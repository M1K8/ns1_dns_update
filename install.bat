@echo off
rem run this script as admin

if not exist main.exe (
    echo Build the main before installing by running "go build"
    goto :exit
)

sc create ns1-dns-update binpath= "%CD%\main.exe" start= auto DisplayName= "ns1-dns-update"
sc description ns1-dns-update "ns1-dns-update"
net start ns1-dns-update
sc query ns1-dns-update

echo Check log

:exit