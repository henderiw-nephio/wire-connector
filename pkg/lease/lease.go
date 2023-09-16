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

package lease

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/henderiw-nephio/wire-connector/pkg/wirer"
	wirecluster "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/cluster"
	wiretopology "github.com/henderiw-nephio/wire-connector/pkg/wirer/cache/topology"
	"github.com/nephio-project/nephio/controllers/pkg/resource"
	"github.com/pkg/errors"
	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	defaultLeaseInterval = 1 * time.Second
	RequeueInterval      = 2 * defaultLeaseInterval
)

type Lease interface {
	AcquireLease(ctx context.Context, topology string, cr client.Object) error
}

type Config struct {
	ClusterCache  wirer.Cache[wirecluster.Cluster]
	TopologyCache wirer.Cache[wiretopology.Topology]
}

func New(leaseNSN types.NamespacedName, cfg *Config) Lease {
	return &lease{
		leasName:       leaseNSN.Name,
		leaseNamespace: leaseNSN.Namespace,
		clusterCache:   cfg.ClusterCache,
		topoCache:      cfg.TopologyCache,
	}
}

type lease struct {
	clusterCache wirer.Cache[wirecluster.Cluster]
	topoCache    wirer.Cache[wiretopology.Topology]

	leasName       string
	leaseNamespace string
}

func getHolderIdentity(cr client.Object) string {
	return fmt.Sprintf("%s.%s.%s",
		strings.ToLower(cr.GetObjectKind().GroupVersionKind().Kind),
		strings.ToLower(cr.GetNamespace()),
		strings.ToLower(cr.GetName()))
}

func (r *lease) getLease(cr client.Object) *coordinationv1.Lease {
	now := metav1.NowMicro()
	return &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.leasName,
			Namespace: r.leaseNamespace,
		},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity:       pointer.String(getHolderIdentity(cr)),
			LeaseDurationSeconds: pointer.Int32(int32(defaultLeaseInterval / time.Second)),
			AcquireTime:          &now,
			RenewTime:            &now,
		},
	}
}

func (r *lease) AcquireLease(ctx context.Context, topology string, cr client.Object) error {
	log := log.FromContext(ctx)
	log.Info("attempting to acquire lease to update the resource", "lease", r.leasName)
	interconnectLeaseNSN := types.NamespacedName{
		Name:      r.leasName,
		Namespace: r.leaseNamespace,
	}

	t, err := r.topoCache.Get(types.NamespacedName{Name: topology})
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("topology not found for topology %s", topology))
	}

	c, err := r.clusterCache.Get(types.NamespacedName{Name: t.ClusterName})
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("cluster not found for topology %s", topology))
	}

	lease := &coordinationv1.Lease{}
	if err := c.Client.Get(ctx, interconnectLeaseNSN, lease); err != nil {
		if resource.IgnoreNotFound(err) != nil {
			return err
		}
		log.Info("lease not found, creating it", "lease", r.leasName)

		lease = r.getLease(cr)
		if err := c.Client.Create(ctx, lease); err != nil {
			return err
		}
	}
	// get the lease again
	if err := c.Client.Get(ctx, interconnectLeaseNSN, lease); err != nil {
		return err
	}

	if lease == nil || lease.Spec.HolderIdentity == nil {
		return fmt.Errorf("lease nil or holderidentity nil")
	}

	now := metav1.NowMicro()
	if *lease.Spec.HolderIdentity != getHolderIdentity(cr) {
		// lease is held by another
		log.Info("lease held by another identity", "identity", *lease.Spec.HolderIdentity)
		if lease.Spec.RenewTime != nil {
			expectedRenewTime := lease.Spec.RenewTime.Add(time.Duration(*lease.Spec.LeaseDurationSeconds) * time.Second)
			if !expectedRenewTime.Before(now.Time) {
				log.Info("cannot acquire lease, lease held by another identity", "identity", *lease.Spec.HolderIdentity)
				return fmt.Errorf("cannot acquire lease, lease held by another identity: %s", *lease.Spec.HolderIdentity)
			}
		}
	}

	// take over the lease or update the lease
	log.Info("successfully acquired lease")
	newLease := r.getLease(cr)
	newLease.SetResourceVersion(lease.ResourceVersion)
	if err := c.Client.Update(ctx, newLease); err != nil {
		return err
	}
	return nil
}
