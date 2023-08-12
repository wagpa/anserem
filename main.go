package main

import (
	"context"
	"encoding/json"
	"fmt"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	influxapi "github.com/influxdata/influxdb-client-go/v2/api"
	influxwrite "github.com/influxdata/influxdb-client-go/v2/api/write"
	"github.com/urfave/cli/v3"
	"io"
	"log"
	"net/http"
	"net/mail"
	"os"
	"strconv"
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
	dynv6Host             string
	dynv6Token            string
}

type BdcResponse struct {
	IpString      string `json:"ipString"`
	IpType        string `json:"ipType"`
	IsBehindProxy bool   `json:"isBehindProxy"`
}

const (
	host   = "http://localhost:8086"
	token  = "" // TODO DO NOT COMMIT THIS!
	org    = "influxtest"
	bucket = "anserem"
)

var (
	// refresh state
	lastAddr    = ""
	lastRefresh = time.Now()
	// indexdb
	client   influxdb2.Client
	queryApi influxapi.QueryAPI
	writeApi influxapi.WriteAPIBlocking
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
				Name:        "refresh-interval",
				Category:    "GENERAL",
				Required:    false,
				Value:       20 * time.Second,
				Usage:       "The duration between refreshes if ip changed",
				Destination: &opts.refreshInterval,
				Sources:     cli.EnvVars("REFRESH_INTERVAL"),
			},
			&cli.DurationFlag{
				Name:        "force-refresh-interval",
				Category:    "GENERAL",
				Required:    false,
				Value:       2 * time.Minute,
				Usage:       "The duration between refreshes",
				Destination: &opts.forcedRefreshInterval,
				Sources:     cli.EnvVars("FORCED_REFRESH_INTERVAL"),
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

	// initialize influxdb client
	client = influxdb2.NewClient(host, token)
	writeApi = client.WriteAPIBlocking(org, bucket)
	queryApi = client.QueryAPI(org)

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
	addr, err := o.publicAddress()
	if err != nil {
		log.Printf(err.Error())
		return
	}

	forced := time.Since(lastRefresh) > o.forcedRefreshInterval
	changed := lastAddr != addr

	// log tick
	point := influxwrite.NewPoint(
		"tick",
		map[string]string{
			"addr":      addr,
			"last_addr": lastAddr,
		},
		map[string]interface{}{
			"addr_changed": changed,
			"forced":       forced,
		},
		time.Now(),
	)
	if err := writeApi.WritePoint(context.Background(), point); err != nil {
		log.Printf("error while writing to indexdb: %v", err)
	}

	// return if update is not necessary
	if !forced && !changed {
		return
	}

	// refresh
	o.refresh(addr)
	lastRefresh = time.Now()
	lastAddr = addr
}

func (o *Options) publicAddress() (string, error) {

	// send request
	start := time.Now()
	res, err := http.Get("https://api-bdc.net/data/client-ip")
	took := time.Since(start)
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

	// log time in influxdb
	point := influxwrite.NewPoint(
		"dbc-request-duration",
		map[string]string{
			"response_code": strconv.Itoa(res.StatusCode),
		},
		map[string]interface{}{
			"ip":              bdc.IpString,
			"ip_type":         bdc.IpType,
			"is_behind_proxy": bdc.IsBehindProxy,
			"duration":        took.Milliseconds(),
		},
		time.Now(),
	)
	if err := writeApi.WritePoint(context.Background(), point); err != nil {
		log.Printf("error while writing to indexdb: %v", err)
	}

	return fmt.Sprintf("%s/128", bdc.IpString), nil
}

func (o *Options) refresh(addr string) {

	// send update request
	start := time.Now()
	res, err := http.Get(fmt.Sprintf(
		"https://dynv6.com/api/update?hostname=%s&token=%s&ipv6=%s",
		o.dynv6Host,
		o.dynv6Token,
		addr,
	))
	took := time.Since(start)
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

	// log time in influxdb
	point := influxwrite.NewPoint(
		"dynv6-request-duration",
		map[string]string{
			"hostname":      o.dynv6Host,
			"response_code": strconv.Itoa(res.StatusCode),
		},
		map[string]interface{}{
			"duration": took.Milliseconds(),
			"response": string(str),
			"address":  addr,
		},
		time.Now(),
	)
	if err := writeApi.WritePoint(context.Background(), point); err != nil {
		log.Printf("error while writing to indexdb: %v", err)
	}
}
