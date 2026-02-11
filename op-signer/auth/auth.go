package auth

import (
	"net/http"

	oprpc "github.com/ethereum-optimism/optimism/op-service/rpc"
	"github.com/ethereum/go-ethereum/log"
	"golang.org/x/crypto/bcrypt"
)

func NewAuthByPasswordMiddleware(logger log.Logger, apiPasswordHash []byte) oprpc.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, password, ok := r.BasicAuth()

			if !ok || !verifyAPIPassword(logger, password, apiPasswordHash) {
				logger.Warn("API: Unauthorized access attempt", "addr", r.RemoteAddr)
				http.Error(w, "Unauthorized password", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// verifyAPIPassword checks if the provided password matches the stored bcrypt hash
func verifyAPIPassword(logger log.Logger, password string, apiPasswordHash []byte) bool {
	// Compare provided password with stored hash
	err := bcrypt.CompareHashAndPassword(apiPasswordHash, []byte(password))

	if err != nil {
		logger.Warn("API: Invalid password", "err", err)
	}

	return err == nil
}
