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
	"github.com/henderiw-nephio/wire-connector/pkg/wire"
	wirecluster "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/cluster"
	wiredaemon "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/daemon"
	wirenode "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/node"
	wirepod "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/pod"
	wireservice "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/service"
	wiretopology "github.com/henderiw-nephio/wire-connector/pkg/wire/cache/topology"
	nodeepproxy "github.com/henderiw-nephio/wire-connector/pkg/wire/proxy/nodeep"
	vxlanclient "github.com/henderiw-nephio/wire-connector/pkg/wire/vxlan/client"
	wirecontroller "github.com/henderiw-nephio/wire-connector/pkg/wire/wirecontroller"
	"go.uber.org/zap/zapcore"

	//resolverproxy "github.com/henderiw-nephio/wire-connector/pkg/wire/proxy/resolver"
	wireproxy "github.com/henderiw-nephio/wire-connector/pkg/wire/proxy/wire"
	reconciler "github.com/nephio-project/nephio/controllers/pkg/reconcilers/reconciler-interface"
	"golang.org/x/exp/slices"
	"golang.org/x/exp/slog"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	//+kubebuilder:scaffold:imports
)

/*
var (
	setupLog = ctrl.Log.WithName("setup")
)
*/

func main() {
	var enabledReconcilersString string

	/*
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: new(slog.LevelVar),
		//AddSource: true,
	}))
	*/
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: new(slog.LevelVar),
		//AddSource: true,
	})).WithGroup("controller main")
	slog.SetDefault(logger)

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
		slog.Error("cannot initialize schema", "err", err)
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: runScheme,
	})
	if err != nil {
		slog.Error("cannot start manager", "err", err)
		os.Exit(1)
	}

	vxlanClient, err := vxlanclient.New(mgr.GetClient())
	if err != nil {
		slog.Error("cannot create vxlan client", "err", err)
		os.Exit(1)
	}
	slog.Info("setup controller")
	ctx := ctrl.SetupSignalHandler()

	c := wire.NewCache[wirecluster.Cluster]()
	svc := wire.NewCache[wireservice.Service]()
	t := wire.NewCache[wiretopology.Topology]()
	pd := wire.NewCache[wirepod.Pod]()
	d := wire.NewCache[wiredaemon.Daemon]()
	n := wire.NewCache[wirenode.Node]()

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
		slog.Error("cannot start manager", "err", err)
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
			slog.Error("cannot start grpc server", "err", err)
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
		NodeRegistry:  registerSupportedNodeProviders(),
	}

	enabledReconcilers := parseReconcilers(enabledReconcilersString)
	var enabled []string
	for name, r := range reconciler.Reconcilers {
		if !reconcilerIsEnabled(enabledReconcilers, name) {
			continue
		}
		if _, err = r.SetupWithManager(ctx, mgr, ctrlCfg); err != nil {
			//setupLog.Error(err, "cannot setup with manager", "reconciler", name)
			slog.Error("cannot setup manager", "err", err, "reconciler", name)
			os.Exit(1)
		}
		enabled = append(enabled, name)
	}

	if len(enabled) == 0 {
		slog.Info("no reconcilers are enabled; did you forget to pass the --reconcilers flag?")
	} else {
		slog.Info("enabled reconcilers", "reconcilers", strings.Join(enabled, ","))
	}

	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		//setupLog.Error(err, "cannot set up health check")
		slog.Error("cannot setup health check", "err", err)
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		//setupLog.Error(err, "cannot set up ready check")
		slog.Error("cannot setup ready check", "err", err)
		os.Exit(1)
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				slog.Info("clusters...")
				for nsn, cluster := range c.List() {
					slog.Info("cluster", "nsn", nsn, "data", cluster.IsReady)
				}
				slog.Info("services...")
				for nsn, service := range svc.List() {
					slog.Info("service", "nsn", nsn, "data", service)
				}
				slog.Info("topologies...")
				for nsn, topology := range t.List() {
					slog.Info("topology", "nsn", nsn, "data", topology)
				}
				/*
					setupLog.Info("nodes...")
					for nsn, node := range n.List() {
						setupLog.Info("node", "Name", nsn, "node", node)
					}
				*/
				slog.Info("pods...")
				for nsn, pod := range pd.List() {
					slog.Info("pod", "nsn", nsn, "data", pod)
				}
				slog.Info("daemons...")
				for nsn, daemon := range d.List() {
					slog.Info("daemon", "nsn", nsn, "data", daemon)
				}
				time.Sleep(5 * time.Second)
			}
		}
	}()

	slog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		slog.Error("cannot start manager", "err", err)
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
