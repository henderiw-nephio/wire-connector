package cri

import (
	"context"
	"time"

	internalapi "k8s.io/cri-api/pkg/apis"
	criv1 "k8s.io/cri-api/pkg/apis/runtime/v1"
	"k8s.io/kubernetes/pkg/kubelet/cri/remote"
)

const (
	defaultTimeout  = 2 * time.Second
	RuntimeEndpoint = "unix:///var/run/containerd/containerd.sock"
	ImageEndpoint   = "unix:///var/run/containerd/containerd.sock"
)

type CRI interface {
	ListContainers(ctx context.Context, filter *criv1.ContainerFilter) ([]*criv1.Container, error)
}

type cri struct {
	timeout       time.Duration
	runtimeClient internalapi.RuntimeService
	imageClient   internalapi.ImageManagerService
}

func New() (CRI, error) {
	runtimeClient, err := remote.NewRemoteRuntimeService(RuntimeEndpoint, defaultTimeout, nil)
	if err != nil {
		return nil, err
	}
	imageClient, err := remote.NewRemoteImageService(ImageEndpoint, defaultTimeout, nil)
	if err != nil {
		return nil, err
	}

	return &cri{
		timeout:       defaultTimeout,
		runtimeClient: runtimeClient,
		imageClient:   imageClient,
	}, nil
}

func (r *cri) ListContainers(ctx context.Context, filter *criv1.ContainerFilter) ([]*criv1.Container, error) {
	return r.runtimeClient.ListContainers(context.TODO(), filter)
}
