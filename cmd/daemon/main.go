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

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.

	"github.com/henderiw-nephio/wire-connector/pkg/cri"
	"github.com/henderiw-nephio/wire-connector/pkg/grpcserver"
	"github.com/henderiw-nephio/wire-connector/pkg/grpcserver/healthhandler"
	nodeepproxy "github.com/henderiw-nephio/wire-connector/pkg/wire/proxy/nodeep"
	wireproxy "github.com/henderiw-nephio/wire-connector/pkg/wire/proxy/wire"
	"github.com/henderiw-nephio/wire-connector/pkg/wire/wiredaemon"
	"github.com/henderiw-nephio/wire-connector/pkg/xdp"
	"golang.org/x/exp/slices"
	"golang.org/x/exp/slog"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	//+kubebuilder:scaffold:imports
)

var (
	setupLog = ctrl.Log.WithName("setup")
)

func main() {
	/*
		opts := zap.Options{
			Development: true,
			TimeEncoder: zapcore.ISO8601TimeEncoder,
		}
		opts.BindFlags(flag.CommandLine)
		flag.Parse()

		ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

		setupLog.Info("setup daemon")
	*/
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: new(slog.LevelVar),
		//AddSource: true,
	})).WithGroup("daemon main")
	slog.SetDefault(logger)
	slog.Info("start daemon")
	ctx := ctrl.SetupSignalHandler()

	cri, err := cri.New()
	if err != nil {
		slog.Error("cannot init cri", "err", err)
		os.Exit(1)
	}

	xdpapp, err := xdp.NewXdpApp()
	if err != nil {
		slog.Error("cannot setup xdp app cri", "err", err)
		os.Exit(1)
	}

	if err := xdpapp.Init(ctx); err != nil {
		slog.Error("cannot init xdp app cri", "err", err)
		os.Exit(1)
	}

	wd := wiredaemon.New(&wiredaemon.Config{
		XDP: xdpapp,
		CRI: cri,
	})

	nodeepp := nodeepproxy.New(&nodeepproxy.Config{
		Backend: wd,
	})
	wirep := wireproxy.New(&wireproxy.Config{
		Backend: wd,
	})

	wh := healthhandler.New()

	s := grpcserver.New(grpcserver.Config{
		Address:  ":" + strconv.Itoa(9999),
		Insecure: true,
	},
		grpcserver.WithWireGetHandler(wirep.WireGet),
		grpcserver.WithWireCreateHandler(wirep.WireCreate),
		grpcserver.WithWireDeleteHandler(wirep.WireDelete),
		grpcserver.WithWireWatchHandler(wirep.WireWatch),
		grpcserver.WithEndpointGetHandler(nodeepp.EndpointGet),
		grpcserver.WithEndpointCreateHandler(nodeepp.EndpointCreate),
		grpcserver.WithEndpointDeleteHandler(nodeepp.EndpointDelete),
		grpcserver.WithEndpointWatchHandler(nodeepp.EndpointWatch),
		grpcserver.WithWatchHandler(wh.Watch),
		grpcserver.WithCheckHandler(wh.Check),
	)

	// block
	if err := s.Start(ctx); err != nil {
		slog.Error("cannot start grpc server", "err", err)
		os.Exit(1)
	}
}

func parseReconcilers(reconcilers string) []string {
	return strings.Split(reconcilers, ",")
}

func reconcilerIsEnabled(reconcilers []string, reconciler string) bool {
	if slices.Contains(reconcilers, "*") {
		return true
	}
	if slices.Contains(reconcilers, reconciler) {
		return true
	}
	if v, found := os.LookupEnv(fmt.Sprintf("RECONCILER_%s", strings.ToUpper(reconciler))); found {
		if v == "true" {
			return true
		}
	}
	return false
}
