/*
Copyright 2023 The Nephio Authors.

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

package clusterwatchcontroller

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	errCreateCache      = "cannot create cache"
	errStartCache       = "cannot start cache/crash cache"
	errCreateController = "cannot create controller"
	errStartController  = "cannot start controller/crash controller"
	errCreateWatch      = "cannot create watch"
)

type Config struct {
	Name        string
	Reconcilers []Reconciler
	RESTConfig  *rest.Config
	RESTmapper  meta.RESTMapper
	Scheme      *runtime.Scheme
}

type Reconciler struct {
	Object     client.Object
	Reconciler reconcile.Reconciler
	Owns       []client.Object
	Watches    []Watch
}

type Watch struct {
	Object       client.Object
	EventHandler handler.EventHandler
}

type Controller interface {
	Error() error
	Stop()
	Start(ctx context.Context) error
}

func New(mgr manager.Manager, cfg *Config) Controller {
	return &ctlr{
		mgr:    mgr,
		Config: cfg,
		// initialize
		cancel: nil,
		err:    nil,
	}
}

type ctlr struct {
	*Config
	mgr    manager.Manager
	cancel context.CancelFunc
	err    error
	l      logr.Logger
}

func (r *ctlr) Error() error {
	return r.err
}

func (r *ctlr) Stop() {
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
}

func (r *ctlr) Start(ctx context.Context) error {
	log := log.FromContext(ctx).WithValues("name", r.Name)
	ctx, cancel := context.WithCancel(ctx)
	r.cancel = cancel

	cache, err := cache.New(r.RESTConfig, cache.Options{Scheme: r.Scheme, Mapper: r.RESTmapper})
	if err != nil {
		log.Error(err, errCreateCache)
		return fmt.Errorf("%s err: %s", errCreateCache, err)
	}
	go func() {
		<-r.mgr.Elected()
		log.Info("start cache")
		r.err = cache.Start(ctx)
		if r.err != nil {
			log.Error(err, errStartCache)
		}
		if r.cancel != nil {
			r.cancel()
		}
	}()
	for _, reconciler := range r.Reconcilers {
		reconciler := reconciler
		ctrl, err := controller.NewUnmanaged(r.Name, r.mgr, controller.Options{
			Reconciler: reconciler.Reconciler,
		})
		if err != nil {
			log.Error(err, errCreateController)
			return fmt.Errorf("%s, err: %s", errCreateController, err)
		}
		// For watch
		hdler := &handler.EnqueueRequestForObject{}
		allPredicates := append([]predicate.Predicate{}, []predicate.Predicate{}...)
		if err := ctrl.Watch(source.Kind(cache, reconciler.Object), hdler, allPredicates...); err != nil {
			return fmt.Errorf("%s, err: %s", errCreateWatch, err)
		}

		// Own watches
		for _, o := range reconciler.Owns {
			allPredicates := append(allPredicates, []predicate.Predicate{}...)
			if err := ctrl.Watch(
				source.Kind(cache, o),
				handler.EnqueueRequestForOwner(r.Scheme, r.RESTmapper, o),
				allPredicates...,
			); err != nil {
				return fmt.Errorf("%s, err: %s", errCreateWatch, err)
			}
		}

		// Watches
		for _, watch := range reconciler.Watches {
			allPredicates := append(allPredicates, []predicate.Predicate{}...)
			if err := ctrl.Watch(
				source.Kind(cache, watch.Object),
				watch.EventHandler,
				allPredicates...,
			); err != nil {
				return fmt.Errorf("%s, err: %s", errCreateWatch, err)
			}
		}

		go func() {
			<-r.mgr.Elected()
			r.l.Info("start controller", "name", reflect.TypeOf(reconciler.Object).Name())
			r.err = ctrl.Start(ctx)
			if r.err != nil {
				log.Error(err, errStartController)
			}
			if r.cancel != nil {
				r.cancel()
			}
		}()
	}
	return nil
}
