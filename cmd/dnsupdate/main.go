package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/judwhite/go-svc"
	"github.com/m1k8/DNSUpdate/pkg/service"
)

// implements svc.Service
type program struct {
	LogFile *os.File
	ctx     context.Context
	s       *service.Svc
}

func (p *program) Context() context.Context {
	return p.ctx
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	prg := program{
		ctx: ctx,
	}

	defer func() {
		if prg.LogFile != nil {
			if closeErr := prg.LogFile.Close(); closeErr != nil {
				log.Printf("error closing '%s': %v\n", prg.LogFile.Name(), closeErr)
			}
		}
	}()

	// call svc.Run to start your program/service
	// svc.Run will call Init, Start, and Stop
	if err := svc.Run(&prg); err != nil {
		log.Fatal(err)
	}
}

func (p *program) Init(env svc.Environment) error {
	// write to "example.log" when running as a Windows Service
	if env.IsWindowsService() {
		dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
		if err != nil {
			return err
		}

		logPath := filepath.Join(dir, "dns.log")

		f, err := os.OpenFile(logPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
		if err != nil {
			return err
		}

		p.LogFile = f

		log.SetOutput(f)
	}

	p.s = service.NewSvc("apiKey", "domain")

	return nil
}

func (p *program) Start() error {
	log.Printf("Starting...\n")
	go p.s.Start()
	return nil
}

func (p *program) Stop() error {
	log.Printf("Stopping...\n")
	p.s.Stop()
	log.Printf("Stopped.\n")
	return nil
}
