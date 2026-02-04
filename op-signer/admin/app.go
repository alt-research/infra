package admin

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"github.com/ethereum/go-ethereum/log"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ethereum-optimism/optimism/op-service/httputil"
	opmetrics "github.com/ethereum-optimism/optimism/op-service/metrics"
	oprpc "github.com/ethereum-optimism/optimism/op-service/rpc"
	"github.com/ethereum-optimism/optimism/op-service/tls/certman"
)

type AdminApp struct {
	log log.Logger

	version string

	registry *prometheus.Registry
	service  *AdminService

	rpc *oprpc.Server
}

func NewAdminApp(logger log.Logger, registry *prometheus.Registry) *AdminApp {
	return &AdminApp{
		log:      logger,
		registry: registry,
	}
}

// SetVersion sets the version of the admin app
func (s *AdminApp) SetVersion(version string) {
	s.version = version
}

func (s *AdminApp) Service() *AdminService {
	return s.service
}

func (s *AdminApp) Init(cfg *Config) error {
	if err := s.initRPC(cfg); err != nil {
		return fmt.Errorf("failed to initialize RPC: %w", err)
	}

	return nil
}

func (s *AdminApp) initRPC(cfg *Config) error {
	var httpOptions = []httputil.Option{}

	if cfg.TLSConfig.Enabled {
		caCert, err := os.ReadFile(cfg.TLSConfig.TLSCaCert)
		if err != nil {
			return fmt.Errorf("failed to read tls ca cert: %s", string(caCert))
		}
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)

		cm, err := certman.New(s.log, cfg.TLSConfig.TLSCert, cfg.TLSConfig.TLSKey)
		if err != nil {
			return fmt.Errorf("failed to read tls cert or key: %w", err)
		}
		if err := cm.Watch(); err != nil {
			return fmt.Errorf("failed to start certman watcher: %w", err)
		}

		tlsConfig := &tls.Config{
			GetCertificate: cm.GetCertificate,
			ClientCAs:      caCertPool,
			ClientAuth:     tls.VerifyClientCertIfGiven, // necessary for k8s healthz probes, but we check the cert in service/auth.go
		}
		serverTlsConfig := &httputil.ServerTLSConfig{
			Config:    tlsConfig,
			CLIConfig: &cfg.TLSConfig,
		}

		httpOptions = append(httpOptions, httputil.WithServerTLS(serverTlsConfig))
	} else {
		s.log.Warn("TLS disabled. This is insecure and only supported for local development. Please enable TLS in production environments!")
	}

	rpcCfg := cfg.RPCConfig
	s.rpc = oprpc.ServerFromConfig(
		&oprpc.ServerConfig{
			AppVersion: s.version,
			Host:       rpcCfg.ListenAddr,
			Port:       rpcCfg.ListenPort,
			RpcOptions: []oprpc.Option{
				oprpc.WithHTTPRecorder(opmetrics.NewPromHTTPRecorder(s.registry, "admin")),
				oprpc.WithLogger(s.log),
			},
			HttpOptions: httpOptions,
		},
	)

	var err error
	s.service, err = NewAdminService(s.log)
	if err != nil {
		return fmt.Errorf("failed to create signer service: %w", err)
	}
	s.service.RegisterAPIs(s.rpc)

	if err := s.rpc.Start(); err != nil {
		return fmt.Errorf("error starting RPC server: %w", err)
	}
	s.log.Info("Started op-signer RPC server", "addr", s.rpc.Endpoint())

	return nil
}

// RPCServer returns the underlying RPC server to allow external management such as stopping it
func (s *AdminApp) RPCServer() *oprpc.Server {
	return s.rpc
}
