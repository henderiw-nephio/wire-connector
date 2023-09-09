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

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/henderiw-nephio/wire-connector/pkg/proto/wirepb"
	"github.com/henderiw-nephio/wire-connector/pkg/wire/backend/vxlan"
	vxlanv1alpha1 "github.com/nokia/k8s-ipam/apis/resource/vxlan/v1alpha1"
	"github.com/nokia/k8s-ipam/pkg/backend"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	vxlanIndexName = "wirer"
)

type Client interface {
	CreateIndex(ctx context.Context, cr *vxlanv1alpha1.VXLANIndex) error
	DeleteIndex(ctx context.Context, cr *vxlanv1alpha1.VXLANIndex) error
	Claim(ctx context.Context, req *wirepb.WireRequest) (*uint32, error)
	DeleteClaim(ctx context.Context, req *wirepb.WireRequest) error
}

func New(cl client.Client) (Client, error) {
	vxlanbe, err := vxlan.New(cl)
	if err != nil {
		return nil, err
	}
	return &c{
		vxlanbe: vxlanbe,
	}, nil
}

type c struct{ vxlanbe backend.Backend }

func (r *c) CreateIndex(ctx context.Context, cr *vxlanv1alpha1.VXLANIndex) error {
	b, err := json.Marshal(cr)
	if err != nil {
		return err
	}
	return r.vxlanbe.CreateIndex(ctx, b)
}

func (r *c) DeleteIndex(ctx context.Context, cr *vxlanv1alpha1.VXLANIndex) error {
	b, err := json.Marshal(cr)
	if err != nil {
		return err
	}
	return r.vxlanbe.DeleteIndex(ctx, b)
}

func (r *c) Claim(ctx context.Context, req *wirepb.WireRequest) (*uint32, error) {
	var cr *vxlanv1alpha1.VXLANClaim
	cr = vxlanv1alpha1.BuildVXLANClaim(
		metav1.ObjectMeta{
			Name:      req.WireKey.Name,
			Namespace: req.WireKey.Namespace,
		},
		vxlanv1alpha1.VXLANClaimSpec{
			VXLANIndex: corev1.ObjectReference{
				Name:      vxlanIndexName,
				Namespace: os.Getenv("POD_NAMESPACE"),
			},
		},
		vxlanv1alpha1.VXLANClaimStatus{},
	)

	cr.AddOwnerLabelsToCR()
	var b []byte
	var err error
	b, err = json.Marshal(cr)
	if err != nil {
		return nil, err
	}
	b, err = r.vxlanbe.Claim(ctx, b)
	if err != nil {
		return nil, err
	}
	cr = &vxlanv1alpha1.VXLANClaim{}
	if err := json.Unmarshal(b, cr); err != nil {
		return nil, err
	}
	if cr.Status.VXLANID == nil {
		return nil, fmt.Errorf("no free vxlan id found")
	}
	return cr.Status.VXLANID, nil
}

func (r *c) DeleteClaim(ctx context.Context, req *wirepb.WireRequest) error {
	var cr *vxlanv1alpha1.VXLANClaim
	cr = vxlanv1alpha1.BuildVXLANClaim(
		metav1.ObjectMeta{
			Name:      req.WireKey.Name,
			Namespace: req.WireKey.Namespace,
		},
		vxlanv1alpha1.VXLANClaimSpec{
			VXLANIndex: corev1.ObjectReference{
				Name:      vxlanIndexName,
				Namespace: os.Getenv("POD_NAMESPACE"),
			},
		},
		vxlanv1alpha1.VXLANClaimStatus{},
	)

	cr.AddOwnerLabelsToCR()
	var b []byte
	var err error
	b, err = json.Marshal(cr)
	if err != nil {
		return err
	}
	return r.vxlanbe.DeleteClaim(ctx, b)
}
