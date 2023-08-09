package anserem

import (
	"fmt"
	"github.com/urfave/cli/v3"
	"io"
	"log"
	"net"
	"net/http"
	"net/mail"
	"strings"
	"time"
)

type Options struct {
	ipPrefix              string
	refreshInterval       time.Duration
	forcedRefreshInterval time.Duration
	duckHost              string
	duckToken             string
	dynv6Host             string
	dynv6Token            string
}

var (
	lastRefresh          = time.UnixMilli(0)
	lastAddr    net.Addr = nil
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
			&cli.StringFlag{
				Name:        "ip-prefix",
				Category:    "GENERAL",
				Required:    false,
				Value:       "2a02:",
				Usage:       "The public ip prefix",
				Destination: &opts.ipPrefix,
				Sources:     cli.EnvVars("IP_PREFIX"),
			},
			&cli.DurationFlag{
				Name:        "refresh-interval",
				Category:    "GENERAL",
				Required:    false,
				Value:       1 * time.Minute,
				Usage:       "The duration between refreshes if ip changed",
				Destination: &opts.refreshInterval,
				Sources:     cli.EnvVars("REFRESH_INTERVAL"),
			},
			&cli.DurationFlag{
				Name:        "forced-refresh-interval",
				Category:    "GENERAL",
				Required:    false,
				Value:       1 * time.Hour,
				Usage:       "The duration between refreshes",
				Destination: &opts.forcedRefreshInterval,
				Sources:     cli.EnvVars("FORCED_REFRESH_INTERVAL"),
			},
			&cli.StringFlag{
				Name:        "duck-host",
				Category:    "DUCK_DNS",
				Required:    false,
				Value:       "",
				Usage:       "The DuckDNS host to refresh for",
				Destination: &opts.duckHost,
				Sources:     cli.EnvVars("DUCK_HOST"),
			},
			&cli.StringFlag{
				Name:        "duck-token",
				Category:    "DUCK_DNS",
				Required:    false,
				Value:       "",
				Usage:       "The DuckDNS token for the provided host",
				Destination: &opts.duckToken,
				Sources:     cli.EnvVars("DUCK_TOKEN"),
			},
			&cli.StringFlag{
				Name:        "dynv6-host",
				Category:    "DYNV6",
				Required:    false,
				Value:       "",
				Usage:       "The dynv6 host to refresh for",
				Destination: &opts.dynv6Host,
				Sources:     cli.EnvVars("DYNV6_HOST"),
			},
			&cli.StringFlag{
				Name:        "dynv6-token",
				Category:    "DYNV6",
				Required:    false,
				Value:       "",
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

func (o *Options) hasDynv6() bool {
	return o.dynv6Host != "" && o.dynv6Token != ""
}

func (o *Options) hasDuckDns() bool {
	return o.duckHost != "" && o.duckToken != ""
}

// start starts the application.
func (o *Options) start(ctx *cli.Context) error {

	// validate that at least one dyndns is configured
	if !o.hasDuckDns() && !o.hasDynv6() {
		return fmt.Errorf("at least one dyn dns has to be configured")
	}

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

func (o *Options) publicIp() (net.Addr, net.IP, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, nil, err
	}

	for _, addr := range addrs {

		// get ip from address
		ip, _, err := net.ParseCIDR(addr.String())
		if err != nil {
			return nil, nil, err
		}

		// ignore unresolvable addresses
		_, err = net.LookupAddr(ip.String())
		if err != nil {
			continue
		}

		// get first public ip
		if !ip.IsPrivate() && strings.HasPrefix(ip.String(), o.ipPrefix) {
			return addr, ip, nil
		}
	}
	return nil, nil, fmt.Errorf("failed to find public ip from %v", addrs)
}

func (o *Options) refresh() {

	// get current ipv6
	addr, ip, err := o.publicIp()
	if err != nil {
		log.Printf("error while getting public ip: %v", err)
		return
	}

	// check should refresh
	force := time.Now().Sub(lastRefresh) >= o.forcedRefreshInterval
	if !force && lastAddr.String() == addr.String() {
		log.Printf("address unchanged: %s", lastAddr)
		return
	}
	lastAddr = addr
	lastRefresh = time.Now()

	// refresh configured providers
	if o.hasDynv6() {
		o.refreshDynv6(addr)
	}
	if o.hasDuckDns() {
		o.refreshDuckDNS(ip)
	}
}

func (o *Options) refreshDuckDNS(ip net.IP) {

	// clear DuckDns
	res, err := http.Get(fmt.Sprintf(
		"https://www.duckdns.org/update?domains=%s&token=%s&clear=true",
		o.duckHost,
		o.duckToken,
	))
	if err != nil {
		log.Printf("error while clearing DuckDNS: %v", err)
		return
	}
	defer func() { _ = res.Body.Close() }()

	// update DuckDns
	res, err = http.Get(fmt.Sprintf(
		"https://www.duckdns.org/update?domains=%s&token=%s&ip=&ipv6=%s",
		o.duckHost,
		o.duckToken,
		ip.String(),
	))
	if err != nil {
		log.Printf("error while updating ipv6 in DuckDNS: %v", err)
		return
	}
	defer func() { _ = res.Body.Close() }()

	// log refresh
	str, err := io.ReadAll(res.Body)
	if err != nil {
		log.Printf("error while reading DuckDNS response: %v", err)
		return
	}
	log.Printf("refreshed DuckDNS: %s %s", str, ip.String())
}

func (o *Options) refreshDynv6(addr net.Addr) {

	// update
	res, err := http.Get(fmt.Sprintf(
		"https://dynv6.com/api/update?hostname=%s&token=%s&ipv6=%s",
		o.dynv6Host,
		o.dynv6Token,
		addr.String(),
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
	log.Printf("refreshed DynV6: %s %s", str, addr.String())
}
