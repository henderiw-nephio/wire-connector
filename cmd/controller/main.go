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
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "github.com/henderiw-nephio/wire-connector/controllers/cluster-controller"
	"github.com/henderiw-nephio/wire-connector/controllers/ctrlconfig"
	_ "github.com/henderiw-nephio/wire-connector/controllers/link-controller"
	_ "github.com/henderiw-nephio/wire-connector/controllers/logicalinterconnect-controller"
	_ "github.com/henderiw-nephio/wire-connector/controllers/node-cache-controller"
	_ "github.com/henderiw-nephio/wire-connector/controllers/node-deployer"
	_ "github.com/henderiw-nephio/wire-connector/controllers/node-ep-controller"
	_ "github.com/henderiw-nephio/wire-connector/controllers/node-nodepool-controller"
	_ "github.com/henderiw-nephio/wire-connector/controllers/pod-cache-controller"
	_ "github.com/henderiw-nephio/wire-connector/controllers/topology-controller"
	_ "github.com/henderiw-nephio/wire-connector/controllers/vxlanindex-controller"
	_ "github.com/henderiw-nephio/wire-connector/controllers/wire-controller"
	"github.com/henderiw-nephio/wire-connector/pkg/grpcserver"
	"github.com/henderiw-nephio/wire-connector/pkg/grpcserver/healthhandler"
	"github.com/henderiw-nephio/wire-connector/pkg/node"
	"github.com/henderiw-nephio/wire-connector/pkg/node/srlinux"
	"github.com/henderiw-nephio/wire-connector/pkg/node/xserver"
	"github.com/henderiw-nephio/wire-connector/pkg/wirer"
	wirecluster "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/cluster"
	wiredaemon "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/daemon"
	wirenode "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/node"
	wirepod "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/pod"
	wireservice "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/service"
	wiretopology "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/topology"
	wireendpoint "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/endpoint"
	nodeepproxy "github.com/henderiw-nephio/wire-connector/pkg/wirer/proxy/nodeep"
	vxlanclient "github.com/henderiw-nephio/wire-connector/pkg/wirer/vxlan/client"
	wirecontroller "github.com/henderiw-nephio/wire-connector/pkg/wirer/wirecontroller"
	"github.com/henderiw/logger/log"
	"go.uber.org/zap/zapcore"

	//resolverproxy "github.com/henderiw-nephio/wire-connector/pkg/wire/proxy/resolver"
	wireproxy "github.com/henderiw-nephio/wire-connector/pkg/wirer/proxy/wire"
	reconciler "github.com/nephio-project/nephio/controllers/pkg/reconcilers/reconciler-interface"
	"golang.org/x/exp/slices"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	//+kubebuilder:scaffold:imports
)

func main() {
	var enabledReconcilersString string

	// this is the strategic direction
	l := log.NewLogger(&log.HandlerOptions{Name: "wirer-controller", AddSource: false})
	slog.SetDefault(l)

	ctx := ctrl.SetupSignalHandler()
	ctx = log.IntoContext(ctx, l)

	log := l
	log.Info("start")

	// this is the controller runtime dependency for now until they move to slog
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
		log.Error("cannot initialize schema", "error", err)
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: runScheme,
	})
	if err != nil {
		log.Error("cannot start manager", "err", err)
		os.Exit(1)
	}

	vxlanClient, err := vxlanclient.New(mgr.GetClient())
	if err != nil {
		log.Error("cannot create vxlan client", "err", err)
		os.Exit(1)
	}
	log.Info("setup controller")

	c := wirer.NewCache[wirecluster.Cluster]()
	svc := wirer.NewCache[wireservice.Service]()
	t := wirer.NewCache[wiretopology.Topology]()
	pd := wirer.NewCache[wirepod.Pod]()
	d := wirer.NewCache[wiredaemon.Daemon]()
	n := wirer.NewCache[wirenode.Node]()
	ep := wirer.NewCache[wireendpoint.Endpoint]()

	wc, err := wirecontroller.New(ctx, &wirecontroller.Config{
		VXLANClient:   vxlanClient,
		ClusterCache:  c,
		ServiceCache:  svc,
		DaemonCache:   d,
		PodCache:      pd,
		NodeCache:     n,
		TopologyCache: t,
	})
	if err != nil {
		//setupLog.Error(err, "unable to start manager")
		log.Error("cannot start manager", "err", err)
		os.Exit(1)
	}

	np := nodeepproxy.New(ctx, &nodeepproxy.Config{
		Backend: wc,
	})
	wp := wireproxy.New(ctx, &wireproxy.Config{
		Backend: wc,
	})
	wh := healthhandler.New()

	s := grpcserver.New(ctx, grpcserver.Config{
		Address:  ":" + strconv.Itoa(9999),
		Insecure: true,
	},
		grpcserver.WithWireGetHandler(wp.WireGet),
		grpcserver.WithWireCreateHandler(wp.WireCreate),
		grpcserver.WithWireDeleteHandler(wp.WireDelete),
		grpcserver.WithWireWatchHandler(wp.WireWatch),
		grpcserver.WithEndpointGetHandler(np.EndpointGet),
		grpcserver.WithEndpointCreateHandler(np.EndpointCreate),
		grpcserver.WithEndpointDeleteHandler(np.EndpointDelete),
		grpcserver.WithEndpointWatchHandler(np.EndpointWatch),
		grpcserver.WithWatchHandler(wh.Watch),
		grpcserver.WithCheckHandler(wh.Check),
	)

	// run the grpc server first before setting up the controllers
	// to ensure the client connection works
	go func() {
		if err := s.Start(ctx); err != nil {
			//setupLog.Error(err, "cannot start grpcserver")
			log.Error("cannot start grpc server", "err", err)
			os.Exit(1)
		}
	}()

	ctrlCfg := &ctrlconfig.Config{
		VXLANClient:   vxlanClient,
		ClusterCache:  c,
		ServiceCache:  svc,
		PodCache:      pd,
		TopologyCache: t,
		DaemonCache:   d,
		NodeCache:     n,
		EndpointCache: ep,
		NodeRegistry:  registerSupportedNodeProviders(),
	}

	enabledReconcilers := parseReconcilers(enabledReconcilersString)
	var enabled []string
	for name, r := range reconciler.Reconcilers {
		if !reconcilerIsEnabled(enabledReconcilers, name) {
			continue
		}
		if _, err = r.SetupWithManager(ctx, mgr, ctrlCfg); err != nil {
			log.Error("cannot setup manager", "err", err, "reconciler", name)
			os.Exit(1)
		}
		enabled = append(enabled, name)
	}

	if len(enabled) == 0 {
		log.Info("no reconcilers are enabled; did you forget to pass the --reconcilers flag?")
	} else {
		log.Info("enabled reconcilers", "reconcilers", strings.Join(enabled, ","))
	}

	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		//setupLog.Error(err, "cannot set up health check")
		log.Error("cannot setup health check", "err", err)
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		//setupLog.Error(err, "cannot set up ready check")
		log.Error("cannot setup ready check", "err", err)
		os.Exit(1)
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				log.Info("clusters...")
				for nsn, cluster := range c.List() {
					log.Info("cluster", "nsn", nsn, "data", cluster.IsReady)
				}
				log.Info("services...")
				for nsn, service := range svc.List() {
					log.Info("service", "nsn", nsn, "data", service)
				}
				log.Info("topologies...")
				for nsn, topology := range t.List() {
					log.Info("topology", "nsn", nsn, "data", topology)
				}
				/*
					setupLog.Info("nodes...")
					for nsn, node := range n.List() {
						setupLog.Info("node", "Name", nsn, "node", node)
					}
				*/
				log.Info("pods...")
				for nsn, pod := range pd.List() {
					log.Info("pod", "nsn", nsn, "data", pod)
				}
				log.Info("daemons...")
				for nsn, daemon := range d.List() {
					log.Info("daemon", "nsn", nsn, "data", daemon)
				}
				time.Sleep(5 * time.Second)
			}
		}
	}()

	log.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		log.Error("cannot start manager", "err", err)
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
