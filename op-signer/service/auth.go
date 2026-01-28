package service

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	oprpc "github.com/ethereum-optimism/optimism/op-service/rpc"
	"github.com/ethereum/go-ethereum/log"
)

type ClientInfo struct {
	ClientName string
}

type clientInfoContextKey struct{}

func tryExtractPath(url string) string {
	subPath := strings.Split(url, "/")

	res := make([]string, 0, len(subPath)+1)

	for i := 0; i < len(subPath); i++ {
		if subPath[i] != "" {
			res = append(res, subPath[i])
		}
	}

	return strings.Join(res, "/")
}
func NewAuthMiddleware(logger log.Logger) oprpc.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientInfo := ClientInfo{}

			logger.Debug("handler request", "r", r.Method, "host", r.Host, "ru", r.URL.String())

			subPath := tryExtractPath(r.URL.Path)
			if subPath != "" {
				// NOTE: need use root path for rpc client
				r.URL = &url.URL{Path: ""}
				clientInfo.ClientName = subPath
			}

			ctx := context.WithValue(r.Context(), clientInfoContextKey{}, clientInfo)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func ClientInfoFromContext(ctx context.Context) ClientInfo {
	info, _ := ctx.Value(clientInfoContextKey{}).(ClientInfo)
	return info
}
