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

package endpoint

import (
	"context"
	"fmt"

	"github.com/henderiw-nephio/wire-connector/pkg/wirer"
	wirecluster "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/cluster"
	wireendpoint "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/endpoint"
	wiretopology "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/topology"
	invv1alpha1 "github.com/nokia/k8s-ipam/apis/inv/v1alpha1"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type Endpoint struct {
	Topology string
	NodeName string
	IfName   string
}

type EpCache interface {
	GetClaimed(ctx context.Context, o client.Object) []invv1alpha1.Endpoint
	Claim(ctx context.Context, o client.Object, epReqs []Endpoint) error
	DeleteClaim(ctx context.Context, o client.Object) error
	GetEndpoint(epReq Endpoint) (invv1alpha1.Endpoint, error)
}

type Config struct {
	ClusterCache  wirer.Cache[wirecluster.Cluster]
	TopologyCache wirer.Cache[wiretopology.Topology]
	EndpointCache wirer.Cache[wireendpoint.Endpoint]
}

func New(cfg *Config) EpCache {
	return &ep{
		epCache:      cfg.EndpointCache,
		topoCache:    cfg.TopologyCache,
		clusterCache: cfg.ClusterCache,
	}
}

type ep struct {
	epCache      wirer.Cache[wireendpoint.Endpoint]
	topoCache    wirer.Cache[wiretopology.Topology]
	clusterCache wirer.Cache[wirecluster.Cluster]
}

func (r *ep) GetClaimed(ctx context.Context, o client.Object) []invv1alpha1.Endpoint {
	log := log.FromContext(ctx)
	log.Info("claim", "req", getCoreRef(o).String())

	claimedEps := []invv1alpha1.Endpoint{}
	for _, ep := range r.epCache.List() {
		if ep.Status.ClaimRef != nil &&
			ep.Status.ClaimRef.APIVersion == o.GetObjectKind().GroupVersionKind().GroupVersion().String() &&
			ep.Status.ClaimRef.Kind == o.GetObjectKind().GroupVersionKind().Kind &&
			ep.Status.ClaimRef.Name == o.GetName() &&
			ep.Status.ClaimRef.Namespace == o.GetNamespace() {
			claimedEps = append(claimedEps, ep.Endpoint)
		}
	}
	return claimedEps
}

func (r *ep) Claim(ctx context.Context, o client.Object, epReqs []Endpoint) error {
	log := log.FromContext(ctx)
	log.Info("claim", "req", getCoreRef(o).String())

	// claim the endpoints
	for _, epReq := range epReqs {
		ep, err := r.GetEndpoint(epReq)
		if err != nil {
			return err
		}
		if ep.IsAllocated(o) {
			return fmt.Errorf("endpoint allocated by another resource: %v", ep.Status.ClaimRef)
		}
		ep.Status.ClaimRef = getCoreRef(o)

		t, err := r.topoCache.Get(types.NamespacedName{Name: ep.Namespace})
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("topology not found for ep %s in %s", fmt.Sprintf("%s/%s", ep.Spec.NodeName, ep.Spec.InterfaceName), getCoreRef(o).String()))
		}

		c, err := r.clusterCache.Get(types.NamespacedName{Name: t.ClusterName})
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("cluster not found for ep %s in %s", fmt.Sprintf("%s/%s", ep.Spec.NodeName, ep.Spec.InterfaceName), getCoreRef(o).String()))
		}

		if err := c.Client.Status().Update(ctx, &ep); err != nil {
			return err
		}
	}

	// delete claims no longer needed
	for _, ep := range r.GetClaimed(ctx, o) {
		found := false
		for _, epReq := range epReqs {
			if ep.Namespace == epReq.Topology && ep.Spec.InterfaceName == epReq.IfName && ep.Spec.NodeName == epReq.NodeName {
				found = true
				break
			}
		}
		if !found {
			ep.Status.ClaimRef = nil
			t, err := r.topoCache.Get(types.NamespacedName{Name: ep.Namespace})
			if err != nil {
				return errors.Wrap(err, fmt.Sprintf("topology not found for ep %s in %s", fmt.Sprintf("%s/%s", ep.Spec.NodeName, ep.Spec.InterfaceName), getCoreRef(o).String()))
			}

			c, err := r.clusterCache.Get(types.NamespacedName{Name: t.ClusterName})
			if err != nil {
				return errors.Wrap(err, fmt.Sprintf("cluster not found for ep %s in %s", fmt.Sprintf("%s/%s", ep.Spec.NodeName, ep.Spec.InterfaceName), getCoreRef(o).String()))
			}

			if err := c.Client.Status().Update(ctx, &ep); err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *ep) DeleteClaim(ctx context.Context, o client.Object) error {
	log := log.FromContext(ctx)
	log.Info("delete claim", "req", getCoreRef(o).String())

	for _, ep := range r.epCache.List() {
		if ep.Endpoint.Status.ClaimRef != nil &&
			ep.Endpoint.Status.ClaimRef.APIVersion == o.GetObjectKind().GroupVersionKind().GroupVersion().String() &&
			ep.Endpoint.Status.ClaimRef.Kind == o.GetObjectKind().GroupVersionKind().Kind &&
			ep.Endpoint.Status.ClaimRef.Name == o.GetName() &&
			ep.Endpoint.Status.ClaimRef.Namespace == o.GetNamespace() {

			ep.Status.ClaimRef = nil

			t, err := r.topoCache.Get(types.NamespacedName{Name: ep.Namespace})
			if err != nil {
				return errors.Wrap(err, fmt.Sprintf("topology not found for ep %s in %s", fmt.Sprintf("%s/%s", ep.Endpoint.Spec.NodeName, ep.Endpoint.Spec.InterfaceName), getCoreRef(o).String()))
			}

			c, err := r.clusterCache.Get(types.NamespacedName{Name: t.ClusterName})
			if err != nil {
				return errors.Wrap(err, fmt.Sprintf("cluster not found for ep %s in %s", fmt.Sprintf("%s/%s", ep.Endpoint.Spec.NodeName, ep.Endpoint.Spec.InterfaceName), getCoreRef(o).String()))
			}

			if err := c.Client.Status().Update(ctx, &ep.Endpoint); err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *ep) GetEndpoint(epReq Endpoint) (invv1alpha1.Endpoint, error) {
	for _, ep := range r.epCache.List() {
		if ep.Namespace == epReq.Topology && ep.Spec.InterfaceName == epReq.IfName && ep.Spec.NodeName == epReq.NodeName {
			return ep.Endpoint, nil
		}
	}
	return invv1alpha1.Endpoint{}, fmt.Errorf("endpoint %s not found", fmt.Sprintf("%s/%s/%s", epReq.Topology, epReq.NodeName, epReq.IfName))
}
