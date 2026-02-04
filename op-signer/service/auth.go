package service

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"

	oprpc "github.com/ethereum-optimism/optimism/op-service/rpc"
	"github.com/ethereum/go-ethereum/log"
)

type ClientInfo struct {
	ClientName string
	ClientCN   string
}

type clientInfoContextKey struct{}

func tryExtractPath(url string, pathRootPrefix string) string {
	subPath := strings.Split(url, "/")

	res := make([]string, 0, len(subPath)+2)

	if pathRootPrefix != "" {
		pathRoots := strings.Split(pathRootPrefix, "/")
		pathRoots = append(pathRoots, subPath...)
		subPath = pathRoots
	}

	for i := 0; i < len(subPath); i++ {
		if subPath[i] != "" {
			res = append(res, subPath[i])
		}
	}

	return strings.Join(res, "/")
}
func NewAuthMiddleware(logger log.Logger, pathRootPrefix string) oprpc.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientInfo := ClientInfo{}

			logger.Debug("handler request", "r", r.Method, "host", r.Host, "ru", r.URL.String())

			subPath := tryExtractPath(r.URL.Path, pathRootPrefix)
			if subPath != "" {
				// NOTE: need use root path for rpc client
				r.URL = &url.URL{Path: ""}
				clientInfo.ClientName = subPath
			}

			clientCN := getClientCN(logger, r)
			if clientCN != "" {
				logger.Debug("handler request", "clientCN", clientCN)
				clientInfo.ClientCN = clientCN
			}

			ctx := context.WithValue(r.Context(), clientInfoContextKey{}, clientInfo)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// getClientCN extracts the Common Name from the client certificate in the request.
// Returns empty string if no certificate is present or if called from localhost.
func getClientCN(logger log.Logger, r *http.Request) string {
	logger.Debug("extracting client CN", "remoteAddr", r.RemoteAddr, "tls", r.TLS)

	// No TLS connection
	if r.TLS == nil {
		return ""
	}

	// Check if localhost (no client cert required)
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if host == "127.0.0.1" || host == "::1" {
		return ""
	}

	// No client certificate
	if len(r.TLS.PeerCertificates) == 0 {
		return ""
	}

	return r.TLS.PeerCertificates[0].Subject.CommonName
}

func ClientInfoFromContext(ctx context.Context) ClientInfo {
	info, _ := ctx.Value(clientInfoContextKey{}).(ClientInfo)
	return info
}
