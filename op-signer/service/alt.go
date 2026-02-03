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
	config   provider.ProviderConfig
	provider provider.SignatureProvider
}

// SignTransaction will sign the given transaction with the key configured for the authenticated client
func (s *AltService) Publickey(ctx context.Context) (hexutil.Bytes, error) {
	clientInfo := ClientInfoFromContext(ctx)

	s.logger.Debug("getting public key for client", "client", clientInfo.ClientName)
	authConfig, err := s.config.GetAuthConfigForClient(clientInfo.ClientName, nil)
	if err != nil {
		return nil, rpc.HTTPError{StatusCode: 403, Status: "Forbidden", Body: []byte(err.Error())}
	}

	publicKey, err := s.provider.GetPublicKey(ctx, authConfig.KeyName)
	if err != nil {
		return nil, rpc.HTTPError{StatusCode: 404, Status: "GetPublicKey Failed", Body: []byte(err.Error())}
	}

	key, err := crypto.UnmarshalPubkey(publicKey)
	if err != nil {
		return nil, rpc.HTTPError{StatusCode: 500, Status: "Internal Server Error", Body: []byte(err.Error())}
	}

	if key == nil {
		return nil, rpc.HTTPError{StatusCode: 500, Status: "Internal Server Error", Body: []byte("unmarshaled public key is nil")}
	}

	return hexutil.Bytes(crypto.PubkeyToAddress(*key).Bytes()), nil
}
