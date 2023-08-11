/*
Copyright 2022 Nokia.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/henderiw-nephio/wire-connector/pkg/proto/wirepb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	defaultTimeout = 2 * time.Second
	maxMsgSize     = 512 * 1024 * 1024
)

type Client interface {
	Start(ctx context.Context) error
	Stop()
	Get(ctx context.Context, req *wirepb.WireRequest) (*wirepb.WireResponse, error)
	Create(ctx context.Context, req *wirepb.WireRequest) (*wirepb.EmptyResponse, error)
	Delete(ctx context.Context, req *wirepb.WireRequest) (*wirepb.EmptyResponse, error)
	// WireWatch
}

type Config struct {
	Address    string
	Username   string
	Password   string
	Proxy      bool
	NoTLS      bool
	TLSCA      string
	TLSCert    string
	TLSKey     string
	SkipVerify bool
	Insecure   bool
	MaxMsgSize int
}

func New(ctx context.Context, cfg *Config) (Client, error) {
	l := ctrl.Log.WithName("wire-client")

	if cfg == nil {
		return nil, fmt.Errorf("cannot create client with empty configw")
	}

	return &client{
		cfg: cfg,
		l:   l,
	}, nil
}

type client struct {
	cfg    *Config
	cancel context.CancelFunc
	conn   *grpc.ClientConn
	client wirepb.WireClient
	//logger
	l logr.Logger
}

func (r *client) Stop() {
	r.cancel()
}

func (r *client) Start(ctx context.Context) error {
	r.l.Info("starting...")

	opts, err := r.getGRPCOpts()
	if err != nil {
		return err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
	r.cancel = cancel

	conn, err := grpc.DialContext(timeoutCtx, r.cfg.Address, opts...)
	if err != nil {
		return err
	}
	defer conn.Close()
	r.client = wirepb.NewWireClient(r.conn)

	go func() {
		for {
			select {
			case <-timeoutCtx.Done():
				return
			}
		}
	}()
	return nil
}

func (r *client) Get(ctx context.Context, req *wirepb.WireRequest) (*wirepb.WireResponse, error) {
	return r.client.Get(ctx, req)
}

func (r *client) Delete(ctx context.Context, req *wirepb.WireRequest) (*wirepb.EmptyResponse, error) {
	return r.client.Delete(ctx, req)
}

func (r *client) Create(ctx context.Context, req *wirepb.WireRequest) (*wirepb.EmptyResponse, error) {
	return r.client.Create(ctx, req)
}

func (r *client) getGRPCOpts() ([]grpc.DialOption, error) {
	var opts []grpc.DialOption
	fmt.Printf("grpc client config: %v\n", r.cfg)
	if r.cfg.Insecure {
		//opts = append(opts, grpc.WithInsecure())
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		tlsConfig, err := r.newTLS()
		if err != nil {
			return nil, err
		}
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	}
	return opts, nil
}

func (r *client) newTLS() (*tls.Config, error) {
	tlsConfig := &tls.Config{
		Renegotiation:      tls.RenegotiateNever,
		InsecureSkipVerify: r.cfg.SkipVerify,
	}
	//err := loadCerts(tlsConfig)
	//if err != nil {
	//	return nil, err
	//}
	return tlsConfig, nil
}

/*
func loadCerts(tlscfg *tls.Config) error {
	if c.TLSCert != "" && c.TLSKey != "" {
		certificate, err := tls.LoadX509KeyPair(*c.TLSCert, *c.TLSKey)
		if err != nil {
			return err
		}
		tlscfg.Certificates = []tls.Certificate{certificate}
		tlscfg.BuildNameToCertificate()
	}
	if c.TLSCA != nil && *c.TLSCA != "" {
		certPool := x509.NewCertPool()
		caFile, err := ioutil.ReadFile(*c.TLSCA)
		if err != nil {
			return err
		}
		if ok := certPool.AppendCertsFromPEM(caFile); !ok {
			return errors.New("failed to append certificate")
		}
		tlscfg.RootCAs = certPool
	}
	return nil
}
*/
