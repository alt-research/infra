package service

import (
	"context"

	"github.com/ethereum-optimism/infra/op-signer/provider"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
)

type AltService struct {
	logger   log.Logger
	config   *provider.ProviderConfig
	provider provider.SignatureProvider
}

// Publickey returns the public key for the authenticated client
func (s *AltService) Publickey(ctx context.Context) (hexutil.Bytes, error) {
	clientInfo := ClientInfoFromContext(ctx)

	s.logger.Debug("getting public key for client", "client", clientInfo.ClientName)
	authConfig, err := s.config.GetAuthConfigForClient(clientInfo.ClientName, nil)
	if err != nil {
		MetricRPCTotal.WithLabelValues("alt_publickey", "error").Inc()
		return nil, rpc.HTTPError{StatusCode: 403, Status: "Forbidden", Body: []byte(err.Error())}
	}

	publicKey, err := s.provider.GetPublicKey(ctx, authConfig.KeyName)
	if err != nil {
		MetricRPCTotal.WithLabelValues("alt_publickey", "error").Inc()
		return nil, rpc.HTTPError{StatusCode: 404, Status: "GetPublicKey Failed", Body: []byte(err.Error())}
	}

	key, err := crypto.UnmarshalPubkey(publicKey)
	if err != nil {
		MetricRPCTotal.WithLabelValues("alt_publickey", "error").Inc()
		return nil, rpc.HTTPError{StatusCode: 500, Status: "Internal Server Error", Body: []byte(err.Error())}
	}

	if key == nil {
		MetricRPCTotal.WithLabelValues("alt_publickey", "error").Inc()
		return nil, rpc.HTTPError{StatusCode: 500, Status: "Internal Server Error", Body: []byte("unmarshaled public key is nil")}
	}

	MetricRPCTotal.WithLabelValues("alt_publickey", "success").Inc()
	return hexutil.Bytes(publicKey), nil
}

// Address returns the address for the authenticated client
func (s *AltService) Address(ctx context.Context) (hexutil.Bytes, error) {
	clientInfo := ClientInfoFromContext(ctx)

	s.logger.Debug("getting public key for client", "client", clientInfo.ClientName)
	authConfig, err := s.config.GetAuthConfigForClient(clientInfo.ClientName, nil)
	if err != nil {
		MetricRPCTotal.WithLabelValues("alt_address", "error").Inc()
		return nil, rpc.HTTPError{StatusCode: 403, Status: "Forbidden", Body: []byte(err.Error())}
	}

	publicKey, err := s.provider.GetPublicKey(ctx, authConfig.KeyName)
	if err != nil {
		MetricRPCTotal.WithLabelValues("alt_address", "error").Inc()
		return nil, rpc.HTTPError{StatusCode: 404, Status: "GetPublicKey Failed", Body: []byte(err.Error())}
	}

	key, err := crypto.UnmarshalPubkey(publicKey)
	if err != nil {
		MetricRPCTotal.WithLabelValues("alt_address", "error").Inc()
		return nil, rpc.HTTPError{StatusCode: 500, Status: "Internal Server Error", Body: []byte(err.Error())}
	}

	if key == nil {
		MetricRPCTotal.WithLabelValues("alt_address", "error").Inc()
		return nil, rpc.HTTPError{StatusCode: 500, Status: "Internal Server Error", Body: []byte("unmarshaled public key is nil")}
	}

	MetricRPCTotal.WithLabelValues("alt_address", "success").Inc()
	return hexutil.Bytes(crypto.PubkeyToAddress(*key).Bytes()), nil
}

// ReloadKey reloads a specific key from Vault for the authenticated client
func (s *AltService) ReloadKey(ctx context.Context) error {
	clientInfo := ClientInfoFromContext(ctx)

	s.logger.Debug("reloading key for client", "client", clientInfo.ClientName)
	authConfig, err := s.config.GetAuthConfigForClient(clientInfo.ClientName, nil)
	if err != nil {
		MetricRPCTotal.WithLabelValues("alt_reloadKey", "error").Inc()
		return rpc.HTTPError{StatusCode: 403, Status: "Forbidden", Body: []byte(err.Error())}
	}

	// Check if provider supports key reloading
	reloader, ok := s.provider.(provider.KeyReloader)
	if !ok {
		MetricRPCTotal.WithLabelValues("alt_reloadKey", "error").Inc()
		return rpc.HTTPError{StatusCode: 400, Status: "Bad Request", Body: []byte("provider does not support key reloading")}
	}

	if err := reloader.ReloadKey(ctx, authConfig.KeyName); err != nil {
		MetricRPCTotal.WithLabelValues("alt_reloadKey", "error").Inc()
		return rpc.HTTPError{StatusCode: 500, Status: "ReloadKey Failed", Body: []byte(err.Error())}
	}

	MetricRPCTotal.WithLabelValues("alt_reloadKey", "success").Inc()
	s.logger.Info("successfully reloaded key", "client", clientInfo.ClientName, "keyName", authConfig.KeyName)
	return nil
}
