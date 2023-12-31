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

package vxlan

import (
	"context"

	"github.com/go-logr/logr"
	invv1alpha1 "github.com/nokia/k8s-ipam/apis/inv/v1alpha1"
	resourcev1alpha1 "github.com/nokia/k8s-ipam/apis/resource/common/v1alpha1"
	vxlanv1alpha1 "github.com/nokia/k8s-ipam/apis/resource/vxlan/v1alpha1"
	"github.com/nokia/k8s-ipam/pkg/db"
	"github.com/nokia/k8s-ipam/pkg/backend"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

type Storage interface {
	Get() backend.Storage[*vxlanv1alpha1.VXLANClaim, map[string]labels.Set]
}

type storageConfig struct {
	client client.Client
	cache  backend.Cache[db.DB[uint32]]
}

func newCMStorage(cfg *storageConfig) (Storage, error) {
	r := &cm{
		c:     cfg.client,
		cache: cfg.cache,
	}

	be, err := backend.NewCMBackend[*vxlanv1alpha1.VXLANClaim, map[string]labels.Set](&backend.CMConfig{
		Client:      cfg.client,
		GetData:     r.GetData,
		RestoreData: r.RestoreData,
		Prefix:      "vxlan",
	})
	if err != nil {
		return nil, err
	}

	r.be = be

	return r, nil
}

type cm struct {
	c     client.Client
	be    backend.Storage[*vxlanv1alpha1.VXLANClaim, map[string]labels.Set]
	cache backend.Cache[db.DB[uint32]]
	l     logr.Logger
}

func (r *cm) Get() backend.Storage[*vxlanv1alpha1.VXLANClaim, map[string]labels.Set] {
	return r.be
}

func (r *cm) GetData(ctx context.Context, ref corev1.ObjectReference) ([]byte, error) {
	r.l = log.FromContext(ctx)
	ca, err := r.cache.Get(ref, false)
	if err != nil {
		r.l.Error(err, "cannot get db info")
		return nil, err
	}

	data := map[uint32]labels.Set{}
	for _, entry := range ca.GetAll() {
		data[entry.ID()] = entry.Labels()
	}
	b, err := yaml.Marshal(data)
	if err != nil {
		r.l.Error(err, "cannot marshal data")
	}
	return b, nil
}

func (r *cm) RestoreData(ctx context.Context, ref corev1.ObjectReference, cm *corev1.ConfigMap) error {
	r.l = log.FromContext(ctx)
	claims := map[uint32]labels.Set{}
	if err := yaml.Unmarshal([]byte(cm.Data[backend.ConfigMapKey]), &claims); err != nil {
		r.l.Error(err, "unmarshal error from configmap data")
		return err
	}
	r.l.Info("restore data", "ref", ref, "claims", claims)

	// Get
	ca, err := r.cache.Get(ref, true)
	if err != nil {
		return err
	}

	linkList := &invv1alpha1.LinkList{}
	if err := r.c.List(ctx, linkList); err != nil {
		return errors.Wrap(err, "cannot get link list")
	}
	r.restoreVXLANs(ctx, ca, claims, linkList)

	return nil
}

func (r *cm) restoreVXLANs(ctx context.Context, ca db.DB[uint32], claims map[uint32]labels.Set, linkList *invv1alpha1.LinkList) {
	var restoreFunc func(ctx context.Context, ca db.DB[uint32], vxlanID uint32, labels labels.Set, linkList *invv1alpha1.LinkList)
	ownerGVK := invv1alpha1.LinkKindGVKString
	restoreFunc = r.restoreDynamicVXLANs

	for vxlanID, labels := range claims {
		r.l.Info("restore claims", "vxlanID", vxlanID, "labels", labels)
		// handle the claims owned by the network instance
		if labels[resourcev1alpha1.NephioOwnerGvkKey] == ownerGVK {
			restoreFunc(ctx, ca, vxlanID, labels, linkList)
		}
	}
}

func (r *cm) restoreDynamicVXLANs(ctx context.Context, ca db.DB[uint32], vxlanID uint32, labels labels.Set, linkList *invv1alpha1.LinkList) {
	r.l = log.FromContext(ctx).WithValues("type", "vxlanClaims", "vxlanID", vxlanID)
	for _, claim := range linkList.Items {
		r.l.Info("restore Dynamic cliams", "claim", claim.GetName())
		if labels[resourcev1alpha1.NephioNsnNameKey] == claim.GetName() &&
			labels[resourcev1alpha1.NephioNsnNamespaceKey] == claim.GetNamespace() {

			r.l.Info("restored Dynamic VXLAN", "VXLANID", vxlanID)
			ca.Set(db.NewEntry(vxlanID, labels))
		}
	}
}

func newNopCMStorage() Storage {
	return &nopcm{
		be: backend.NewNopStorage[*vxlanv1alpha1.VXLANClaim, map[string]labels.Set](),
	}
}

type nopcm struct {
	be backend.Storage[*vxlanv1alpha1.VXLANClaim, map[string]labels.Set]
}

func (r *nopcm) Get() backend.Storage[*vxlanv1alpha1.VXLANClaim, map[string]labels.Set] {
	return r.be
}
