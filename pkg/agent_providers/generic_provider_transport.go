package providers

import (
	"net"
	"net/http"
	"time"
)

// providerTransportIdleConn is how long an idle pooled connection is kept
// before the transport closes it. http.DefaultTransport uses 90 s, which
// outlives the idle limits of Cloudflare-fronted providers and home NATs: a
// connection pooled and then reused after that window is silently reset by
// the middlebox, so the next request's first read fails. A shorter window
// keeps the pool from handing out a connection the peer has already dropped.
const providerTransportIdleConn = 30 * time.Second

// providerTransportKeepAlive is the TCP keepalive period. Detecting a dead
// peer at the OS level (a NAT hole or a reset upstream) means the transport
// fails fast on a genuinely-dead connection instead of hanging on a read.
const providerTransportKeepAlive = 15 * time.Second

// newProviderTransport builds the dedicated http.Transport a GenericProvider's
// streaming and non-streaming clients use instead of http.DefaultTransport.
// Each client gets its own instance so a long-lived stream on one does not
// evict or stall the pooled connections of the other.
func newProviderTransport() *http.Transport {
	dialer := &net.Dialer{
		KeepAlive: providerTransportKeepAlive,
		Timeout:   30 * time.Second,
	}
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		IdleConnTimeout:       providerTransportIdleConn,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}
