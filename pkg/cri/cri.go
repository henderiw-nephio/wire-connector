package cri

import (
	"context"
	"encoding/json"
	"reflect"
	"time"

	log "github.com/sirupsen/logrus"
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
	GetContainerPiD(ctx context.Context, containerID string) (string, error)
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
	return r.runtimeClient.ListContainers(ctx, filter)
}

func (r *cri) GetContainerPiD(ctx context.Context, containerID string) (string, error) {
	resp, err := r.runtimeClient.ContainerStatus(ctx, containerID, true)
	if err != nil {
		return "", err
	}

	x := map[string]string{}
	if err := json.Unmarshal([]byte(resp.GetInfo()["info"]), &x); err != nil {
		return "", err
	}
	log.Infof("leaf1: pid: %v", x["pid"])
	log.Infof("leaf1: pid type: %v", reflect.TypeOf(x["pid"]).Name())

	return x["pid"], nil
}
