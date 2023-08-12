package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/urfave/cli/v3"
	"io"
	"log"
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
	refreshInterval       time.Duration
	forcedRefreshInterval time.Duration
	subnetMask            string
	dynv6Host             string
	dynv6Token            string
}

type BdcResponse struct {
	IpString      string `json:"ipString"`
	IpType        string `json:"ipType"`
	IsBehindProxy bool   `json:"isBehindProxy"`
}

var (
	// publishAddress state
	lastAddr    = ""
	lastRefresh = time.Now()
)

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
				Name:        "publishAddress-interval",
				Category:    "GENERAL",
				Required:    false,
				Value:       20 * time.Second,
				Usage:       "The duration between refreshes if ip changed",
				Destination: &opts.refreshInterval,
				Sources:     cli.EnvVars("REFRESH_INTERVAL"),
			},
			&cli.DurationFlag{
				Name:        "force-publishAddress-interval",
				Category:    "GENERAL",
				Required:    false,
				Value:       2 * time.Minute,
				Usage:       "The duration between refreshes",
				Destination: &opts.forcedRefreshInterval,
				Sources:     cli.EnvVars("FORCED_REFRESH_INTERVAL"),
			},
			&cli.StringFlag{
				Name:        "subnet-mask",
				Category:    "GENERAL",
				Required:    false,
				Value:       "128",
				Usage:       "The ipv6 subnet mask",
				Destination: &opts.subnetMask,
				Sources:     cli.EnvVars("SUBNET_MASK"),
			},
			&cli.StringFlag{
				Name:        "dynv6-host",
				Category:    "DYNV6",
				Required:    true,
				Usage:       "The dynv6 host to publishAddress for",
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

	// start ticker for periodic refreshes
	ticker := time.NewTicker(o.refreshInterval)
	defer ticker.Stop()

	// keep refreshing
	for {
		select {
		case <-ticker.C:
			o.onTick()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (o *Options) onTick() {

	// get public address
	addr, err := o.fetchAddress()
	if err != nil {
		log.Printf(err.Error())
		return
	}

	forced := time.Since(lastRefresh) > o.forcedRefreshInterval
	changed := lastAddr != addr

	// return if update is not necessary
	if !forced && !changed {
		return
	}

	// refresh
	o.publishAddress(addr)
	lastRefresh = time.Now()
	lastAddr = addr
}

func (o *Options) fetchAddress() (string, error) {

	// send request
	res, err := http.Get("https://api-bdc.net/data/client-ip")
	if err != nil {
		return "", fmt.Errorf("error while getting public ip: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	// get public address
	var bdc BdcResponse
	dec := json.NewDecoder(res.Body)
	if err := dec.Decode(&bdc); err != nil {
		return "", fmt.Errorf("error while decoding public ip response: %v", err)
	}

	return fmt.Sprintf("%s/%s", bdc.IpString, o.subnetMask), nil
}

func (o *Options) publishAddress(addr string) {

	// send update request
	res, err := http.Get(fmt.Sprintf(
		"https://dynv6.com/api/update?hostname=%s&token=%s&ipv6=%s",
		o.dynv6Host,
		o.dynv6Token,
		addr,
	))
	if err != nil {
		log.Printf("error while updating ipv6 in Dynv6: %v", err)
		return
	}
	defer func() { _ = res.Body.Close() }()

	str, err := io.ReadAll(res.Body)
	if err != nil {
		log.Printf("error while reading Dynv6 response: %v", err)
		return
	}
	log.Printf("refreshed DynV6: %s", str)
}
