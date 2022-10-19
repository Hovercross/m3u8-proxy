package main

import (
	"flag"
	"net/http"
	"net/url"
	"os"

	"github.com/hovercross/m3u8-proxy/proxy"
	"github.com/rs/zerolog/log"
)

func main() {
	log.Debug().Msg("Starting up")

	errCode := run()
	if errCode != 0 {
		log.Error().Int("error-code", errCode).Msg("process terminating with non-zero error code")
	} else {
		log.Info().Msg("process exiting successfully")
	}

	os.Exit(errCode)
}

func run() int {
	var upstreamParam string
	var listenParam string

	flag.StringVar(&upstreamParam, "upstream", "", "Upstream server to proxy requests to")
	flag.StringVar(&listenParam, "listen", ":8080", "Interface to listen on")
	flag.Parse()

	if upstreamParam == "" {
		log.Error().Msg("Upstream server is required")
		return 1
	}

	url, err := url.Parse(upstreamParam)

	if err != nil {
		log.Err(err).Msg("Unable to parse URL")
		return 1
	}

	proxy := proxy.New(url)
	http.HandleFunc("/", proxy)
	if err := http.ListenAndServe(listenParam, nil); err != nil {
		log.Err(err).Msg("Unable to launch server")
		return 1
	}

	return 0
}
