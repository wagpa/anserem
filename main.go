package main

import (
	"context"
	"fmt"
	"github.com/urfave/cli/v3"
	"io"
	"log"
	"net"
	"net/http"
	"net/mail"
	"os"
	"time"
)

// main is the entry point and starts the application process.
func main() {
	log.Println("starting application")
	if err := Command().Run(context.Background(), os.Args); err != nil {
		_, _ = fmt.Fprint(os.Stderr, err)
	}
}

type Options struct {
	refreshInterval time.Duration
	dynv6Host       string
	dynv6Token      string
}

// Command is the entry point and defines the application itself.
func Command() *cli.Command {
	// declare a new options object to store everything in
	var opts Options

	// initialize the application
	app := &cli.Command{
		Name:    "anserem",
		Version: "v1.0.0",
		Authors: []any{
			&mail.Address{
				Name:    "Paul Wagner",
				Address: "hydrofin@gmail.com",
			},
		},
		UseShortOptionHandling: true,
		Suggest:                true,
		EnableShellCompletion:  true,
		Flags: []cli.Flag{
			&cli.DurationFlag{
				Name:        "refresh-interval",
				Category:    "GENERAL",
				Required:    false,
				Value:       5 * time.Minute,
				Usage:       "The duration between refreshes if ip changed",
				Destination: &opts.refreshInterval,
				Sources:     cli.EnvVars("REFRESH_INTERVAL"),
			},
			&cli.StringFlag{
				Name:        "dynv6-host",
				Category:    "DYNV6",
				Required:    true,
				Usage:       "The dynv6 host to refresh for",
				Destination: &opts.dynv6Host,
				Sources:     cli.EnvVars("DYNV6_HOST"),
			},
			&cli.StringFlag{
				Name:        "dynv6-token",
				Category:    "DYNV6",
				Required:    true,
				Usage:       "The dynv6 token for the provided host",
				Destination: &opts.dynv6Token,
				Sources:     cli.EnvVars("DYNV6_TOKEN"),
			},
		},
		Action: opts.start,
	}

	// return the newly created application
	return app
}

// start starts the application.
func (o *Options) start(ctx *cli.Context) error {

	ticker := time.NewTicker(o.refreshInterval)
	defer ticker.Stop()

	// initial refresh
	o.refresh()

	// keep refreshing
	for {
		select {
		case <-ticker.C:
			o.refresh()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (o *Options) refresh() {

	// TODO remove after debugging
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, addr := range addrs {
			log.Printf("found address %v", addr)
		}
	}

	// send update request
	res, err := http.Get(fmt.Sprintf(
		"https://dynv6.com/api/update?hostname=%s&token=%s&ipv6=auto",
		o.dynv6Host,
		o.dynv6Token,
	))
	if err != nil {
		log.Printf("error while updating ipv6 in Dynv6: %v", err)
		return
	}
	defer func() { _ = res.Body.Close() }()

	// log refresh
	str, err := io.ReadAll(res.Body)
	if err != nil {
		log.Printf("error while reading Dynv6 response: %v", err)
		return
	}
	log.Printf("refreshed DynV6: %s", str)
}
