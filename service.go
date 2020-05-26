package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
	"golang.org/x/sys/windows/svc/eventlog"
	event "golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"

	dnsUpdate "github.com/m1k8/DNSUpdate/app"
)

var elog debug.Log

type myservice struct{}

const svcName = "DNSUpdate"

// This is the name that will appear in the Services control panel
const svcNameLong = "NS1DNSUpdate"

// This is assigned the full SHA1 hash from GIT
const sha1ver = "ae6beead477da591ce8ca1b0bae912422e59ffc4"

func main() {
	isIntSess, err := svc.IsAnInteractiveSession()
	if err != nil {
		log.Fatalf("failed to determine if we are running in an interactive session: %v", err)
	}
	if !isIntSess {
		runService(svcName, false)
		return
	}

	if (len(os.Args) < 2 && os.Args[1] != "start") || (len(os.Args) < 4 && os.Args[1] == "start") {
		fmt.Fprintf(os.Stderr,
			"%s\n\n"+
				"usage: %s <command>\n"+
				"       where <command> is one of\n"+
				"       install, remove, debug, start, stop, pause or continue.\n"+
				"		for start, make sure to also supply the domain and api key",
			"invalid command", os.Args[0])
		os.Exit(2)
	}

	cmd := strings.ToLower(os.Args[1])

	switch cmd {
	case "debug":
		runService(svcName, true)
		return
	case "install":
		err = installService(svcName, svcNameLong)
	case "remove":
		err = removeService(svcName)
	case "start":
		err = startService(svcName, os.Args[2], os.Args[3])
	case "stop":
		err = controlService(svcName, svc.Stop, svc.Stopped)
	case "pause":
		err = controlService(svcName, svc.Pause, svc.Paused)
	case "continue":
		err = controlService(svcName, svc.Continue, svc.Running)
	default:
		fmt.Fprintf(os.Stderr,
			"%s\n\n"+
				"usage: %s <command>\n"+
				"       where <command> is one of\n"+
				"       install, remove, debug, start, stop, pause or continue.\n",
			"invalid command", os.Args[0])
		os.Exit(2)
	}
	if err != nil {
		fmt.Printf("failed to %s %s: %v", cmd, svcName, err)
	}
	return
}

func startService(name string, domain string, api string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("could not access service: %v", err)
	}
	defer s.Close()
	err = s.Start("is", "manual-started", domain, api)
	if err != nil {
		return fmt.Errorf("could not start service: %v", err)
	}
	return nil
}

func controlService(name string, c svc.Cmd, to svc.State) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("could not access service: %v", err)
	}
	defer s.Close()
	status, err := s.Control(c)
	if err != nil {
		return fmt.Errorf("could not send control=%d: %v", c, err)
	}
	timeout := time.Now().Add(10 * time.Second)
	for status.State != to {
		if timeout.Before(time.Now()) {
			return fmt.Errorf("timeout waiting for service to go to state=%d", to)
		}
		time.Sleep(300 * time.Millisecond)
		status, err = s.Query()
		if err != nil {
			return fmt.Errorf("could not retrieve service status: %v", err)
		}
	}
	return nil
}

func (m *myservice) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown | svc.AcceptPauseAndContinue
	changes <- svc.Status{State: svc.StartPending}
	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	gracefulExit := make(chan bool, 1)
	hasFinished := make(chan bool, 1)
	catastrophicFailure := make(chan bool, 1)

	firstRun := true

	event.InstallAsEventCreate("dnsUpdate", 20)

	log, _ := event.Open("dnsUpdate")

	domain := args[3]
	api := args[4]

	reconnectTicker := time.NewTicker(1 * time.Second)

loop:
	for {
		//check we can access the api on first start...
		//..and if we cant, wait until we can before proceeding, but stay receptive to svc.Stop etc
		if firstRun {
			select {
			case <-reconnectTicker.C:
				if dnsUpdate.CheckConnection(elog) {
					firstRun = false
					go dnsUpdate.Run(gracefulExit, hasFinished, catastrophicFailure, log, domain, api)
					reconnectTicker = time.NewTicker(1 * time.Minute)
				}
			}
		}
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
				// Testing deadlock from https://code.google.com/p/winsvc/issues/detail?id=4
				time.Sleep(100 * time.Millisecond)
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				// golang.org/x/sys/windows/svc.TestExample is verifying this output.
				testOutput := strings.Join(args, "-")
				testOutput += fmt.Sprintf("-%d", c.Context)
				elog.Info(1, testOutput)
				// let the app know we want to cleanly finish
				gracefulExit <- true
				// wait for everything to finish cleanly..
				time.Sleep(100 * time.Millisecond)
				<-hasFinished
				break loop
			case svc.Pause:
				changes <- svc.Status{State: svc.Paused, Accepts: cmdsAccepted}
			case svc.Continue:
				changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
			default:
				elog.Error(1, fmt.Sprintf("unexpected control request #%d", c))
			}
		case c := <-catastrophicFailure:
			if c {
				// this is a disconnect reported by the app before the service polled for it, ergo the app isnt running
				// meaning it needs to be restarted once there is a connection
				elog.Info(8, "Message received, polling...")
				firstRun = true
				continue loop
			}
			elog.Error(1, fmt.Sprint("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
			break loop
		}
	}
	changes <- svc.Status{State: svc.StopPending}
	return
}

func runService(name string, isDebug bool) {
	var err error
	if isDebug {
		elog = debug.New(name)
	} else {
		elog, err = eventlog.Open(name)
		if err != nil {
			return
		}
	}
	defer elog.Close()

	elog.Info(1, fmt.Sprintf("starting %s service", name))
	run := svc.Run
	if isDebug {
		run = debug.Run
	}
	err = run(name, &myservice{})
	if err != nil {
		elog.Error(1, fmt.Sprintf("%s service failed: %v", name, err))
		return
	}
	elog.Info(1, fmt.Sprintf("%s service stopped", name))
}

func exePath() (string, error) {
	var err error
	prog := os.Args[0]
	p, err := filepath.Abs(prog)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(p)
	if err == nil {
		if !fi.Mode().IsDir() {
			return p, nil
		}
		err = fmt.Errorf("%s is directory", p)
	}
	if filepath.Ext(p) == "" {
		var fi os.FileInfo

		p += ".exe"
		fi, err = os.Stat(p)
		if err == nil {
			if !fi.Mode().IsDir() {
				fmt.Printf("Service exe found!")
				return p, nil
			}
			err = fmt.Errorf("%s is directory", p)
		}
	}
	return "", err
}

func installService(name, desc string) error {
	exepath, err := exePath()
	if err != nil {
		return err
	}
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err == nil {
		s.Close()
		return fmt.Errorf("service %s already exists", name)
	}
	s, err = m.CreateService(name, exepath, mgr.Config{DisplayName: desc}, "is", "auto-started")
	if err != nil {
		return err
	}
	defer s.Close()
	err = eventlog.InstallAsEventCreate(name, eventlog.Error|eventlog.Warning|eventlog.Info)
	if err != nil {
		s.Delete()
		return fmt.Errorf("SetupEventLogSource() failed: %s", err)
	}
	return nil
}

func removeService(name string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("service %s is not installed", name)
	}
	defer s.Close()
	err = s.Delete()
	if err != nil {
		return err
	}
	err = eventlog.Remove(name)
	if err != nil {
		return fmt.Errorf("RemoveEventLogSource() failed: %s", err)
	}
	return nil
}
