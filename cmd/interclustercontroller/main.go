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
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	"github.com/henderiw-nephio/wire-connector/pkg/node"
	"github.com/henderiw-nephio/wire-connector/pkg/node/srlinux"
	"github.com/henderiw-nephio/wire-connector/pkg/node/xserver"
	_ "github.com/henderiw-nephio/wire-connector/controllers/cluster-controller"
	"github.com/henderiw-nephio/wire-connector/controllers/ctrlconfig"
	_ "github.com/henderiw-nephio/wire-connector/controllers/node-cache-controller"
	_ "github.com/henderiw-nephio/wire-connector/controllers/pod-cache-controller"
	_ "github.com/henderiw-nephio/wire-connector/controllers/wire-controller"
	"github.com/henderiw-nephio/wire-connector/pkg/grpcserver"
	"github.com/henderiw-nephio/wire-connector/pkg/grpcserver/healthhandler"
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	wirecluster "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/cluster"
	wiredaemon "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/daemon"
	wirenode "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/node"
	wirepod "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/pod"
	wireservice "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/service"
	wiretopology "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/topology"
	wireproxy "github.com/henderiw-nephio/wire-connector/pkg/wire/proxy/wire"
	"github.com/henderiw-nephio/wire-connector/pkg/wire/wireclustercontroller"
	reconciler "github.com/nephio-project/nephio/controllers/pkg/reconcilers/reconciler-interface"
	"go.uber.org/zap/zapcore"
	"golang.org/x/exp/slices"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	//+kubebuilder:scaffold:imports
)

var (
	setupLog = ctrl.Log.WithName("setup")
)

func main() {
	var enabledReconcilersString string

	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// setup controllers
	runScheme := runtime.NewScheme()
	if err := scheme.AddToScheme(runScheme); err != nil {
		setupLog.Error(err, "cannot initializer schema")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: runScheme,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	setupLog.Info("setup controller")
	ctx := ctrl.SetupSignalHandler()

	c := wire.NewCache[wirecluster.Cluster]()
	svc := wire.NewCache[wireservice.Service]()
	t := wire.NewCache[wiretopology.Topology]()
	pd := wire.NewCache[wirepod.Pod]()
	d := wire.NewCache[wiredaemon.Daemon]()
	n := wire.NewCache[wirenode.Node]()

	wcc := wireclustercontroller.New(ctx, &wireclustercontroller.Config{
		DaemonCache:   d,
		PodCache:      pd,
		NodeCache:     n,
		ClusterCache:  c,
		ServiceCache:  svc,
		TopologyCache: t,
	})

	wp := wireproxy.New(&wireproxy.Config{
		Backend: wcc,
	})
	wh := healthhandler.New()

	s := grpcserver.New(grpcserver.Config{
		Address:  ":" + strconv.Itoa(9999),
		Insecure: true,
	},
		grpcserver.WithWireGetHandler(wp.WireGet),
		grpcserver.WithWireCreateHandler(wp.WireCreate),
		grpcserver.WithWireDeleteHandler(wp.WireDelete),
		grpcserver.WithWireWatchHandler(wp.WireWatch),
		grpcserver.WithWatchHandler(wh.Watch),
		grpcserver.WithCheckHandler(wh.Check),
	)

	// run the grpc server first before setting up the controllers
	// to ensure the client connection works
	go func() {
		if err := s.Start(ctx); err != nil {
			setupLog.Error(err, "cannot start grpcserver")
			os.Exit(1)
		}
	}()

	ctrlCfg := &ctrlconfig.Config{
		PodCache:      pd,
		DaemonCache:   d,
		NodeCache:     n,
		ClusterCache:  c,
		ServiceCache:  svc,
		TopologyCache: t,
	}

	enabledReconcilers := parseReconcilers(enabledReconcilersString)
	var enabled []string
	for name, r := range reconciler.Reconcilers {
		if !reconcilerIsEnabled(enabledReconcilers, name) {
			continue
		}
		if _, err = r.SetupWithManager(ctx, mgr, ctrlCfg); err != nil {
			setupLog.Error(err, "cannot setup with manager", "reconciler", name)
			os.Exit(1)
		}
		enabled = append(enabled, name)
	}

	if len(enabled) == 0 {
		setupLog.Info("no reconcilers are enabled; did you forget to pass the --reconcilers flag?")
	} else {
		setupLog.Info("enabled reconcilers", "reconcilers", strings.Join(enabled, ","))
	}

	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "cannot set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "cannot set up ready check")
		os.Exit(1)
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				setupLog.Info("clusters...")
				for nsn, cluster := range c.List() {
					setupLog.Info("cluster", "nsn", nsn, "cluster", cluster)
				}
				setupLog.Info("services...")
				for nsn, service := range svc.List() {
					setupLog.Info("service", "nsn", nsn, "service", service)
				}
				setupLog.Info("topologies...")
				for nsn, topology := range t.List() {
					setupLog.Info("topology", "nsn", nsn, "topology", topology)
				}
				setupLog.Info("pods...")
				for nsn, pod := range pd.List() {
					setupLog.Info("pod", "Name", nsn, "pod", pod)
				}
				setupLog.Info("daemons...")
				for nsn, daemon := range d.List() {
					setupLog.Info("daemon", "Name", nsn, "daemon", daemon)
				}

				time.Sleep(5 * time.Second)
			}
		}
	}()

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
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

func registerSupportedNodeProviders() node.NodeRegistry {
	nodeRegistry := node.NewNodeRegistry()
	srlinux.Register(nodeRegistry)
	xserver.Register(nodeRegistry)

	return nodeRegistry
}
