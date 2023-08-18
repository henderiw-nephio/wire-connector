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
	"strings"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	"github.com/henderiw-nephio/wire-connector/controllers/ctrlconfig"
	_ "github.com/henderiw-nephio/wire-connector/controllers/link"
	_ "github.com/henderiw-nephio/wire-connector/controllers/node"
	_ "github.com/henderiw-nephio/wire-connector/controllers/pod"
	"github.com/henderiw-nephio/wire-connector/pkg/cri"
	"github.com/henderiw-nephio/wire-connector/pkg/node"
	"github.com/henderiw-nephio/wire-connector/pkg/pod"
	"github.com/henderiw-nephio/wire-connector/pkg/xdp"
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

	cri, err := cri.New()
	if err != nil {
		setupLog.Error(err, "unable to initialize cri")
		os.Exit(1)
	}

	xdpapp, err := xdp.NewXdpApp()
	if err != nil {
		setupLog.Error(err, "cannot to set xdp app")
		os.Exit(1)
	}

	// THIS IS JUST A TEST -> AVOID RUNNING THE XDP INIT PER DAEMON
	if os.Getenv("NODE_NAME") == "topo-control-plane" {
		if err := xdpapp.Init(ctx); err != nil {
			setupLog.Error(err, "cannot init xdp app")
			os.Exit(1)
		}
	}

	podManager := pod.NewManager()
	nodeManager := node.NewManager()
	ctrlCfg := &ctrlconfig.ControllerConfig{
		PodManager:  podManager,
		NodeManager: nodeManager,
		CRI:         cri,
		XDP:         xdpapp,
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
			setupLog.Info("containers...")
			for podNsn, podCtx := range podManager.ListPods() {
				setupLog.Info("pod", "Name", podNsn, "HostConn", podCtx.HostConnectivity, "HostIP", podCtx.HostIP, "Containers", podCtx.Containers)
			}
			/*
				setupLog.Info("nodes...")
				for nodeName, n := range nodeManager.ListNodes() {
					setupLog.Info("node", "Name", nodeName, "NodeSpec", n.Status.Addresses)
				}
			*/
			time.Sleep(5 * time.Second)
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
