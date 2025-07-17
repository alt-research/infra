package service

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"

	oprpc "github.com/ethereum-optimism/optimism/op-service/rpc"
	optls "github.com/ethereum-optimism/optimism/op-service/tls"
)

type ClientInfo struct {
	ClientName string
}

type clientInfoContextKey struct{}

func NewAuthMiddleware() oprpc.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientInfo := ClientInfo{}

			// PeerTLSInfo is attached to context by upstream op-service middleware
			peerTlsInfo := optls.PeerTLSInfoFromContext(r.Context())
			if peerTlsInfo.LeafCertificate == nil {
				http.Error(w, "client certificate was not provided", 401)
				return
			}
			// Note that the certificate is already verified by http server if we get here
			if len(peerTlsInfo.LeafCertificate.DNSNames) < 1 {
				http.Error(w, "client certificate verified but did not contain DNS SAN extension", 401)
				return
			}

			//clientInfo.ClientName = peerTlsInfo.LeafCertificate.DNSNames[0]
			dns := extractHostname(r.Host)
			fmt.Println("request:", "r-header", r.Header, "url", r.URL, "host", r.Host, "dns", dns, "tlsinfo0-dnsnames", peerTlsInfo.LeafCertificate.DNSNames)
			for _, name := range peerTlsInfo.LeafCertificate.DNSNames {
				if name == dns {
					clientInfo.ClientName = dns
				}
			}
			if clientInfo.ClientName == "" {
				s := fmt.Sprintf("client certificate provided but not in DNS SAN extension. current dns %s, DNSNames in certificate are: %s", dns, peerTlsInfo.LeafCertificate.DNSNames)
				http.Error(w, s, 401)
				return
			}

			ctx := context.WithValue(r.Context(), clientInfoContextKey{}, clientInfo)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func extractHostname(host string) string {
	if strings.Contains(host, ":") {
		h, _, err := net.SplitHostPort(host)
		if err == nil {
			return h
		}
	}
	return host
}

func ClientInfoFromContext(ctx context.Context) ClientInfo {
	info, _ := ctx.Value(clientInfoContextKey{}).(ClientInfo)
	return info
}
