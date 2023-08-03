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
	ListPods(ctx context.Context, filter *criv1.PodSandboxFilter) ([]*criv1.PodSandbox, error)
	ListContainers(ctx context.Context, filter *criv1.ContainerFilter) ([]*criv1.Container, error)
	GetContainerInfo(ctx context.Context, containerID string) (*ContainerInfo, error)
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

func (r *cri) ListPods(ctx context.Context, filter *criv1.PodSandboxFilter) ([]*criv1.PodSandbox, error) {
	return r.runtimeClient.ListPodSandbox(ctx, filter)
}

func (r *cri) ListContainers(ctx context.Context, filter *criv1.ContainerFilter) ([]*criv1.Container, error) {
	return r.runtimeClient.ListContainers(ctx, filter)
}

func (r *cri) GetContainerInfo(ctx context.Context, containerID string) (*ContainerInfo, error) {
	resp, err := r.runtimeClient.ContainerStatus(ctx, containerID, true)
	if err != nil {
		return nil, err
	}

	status, err := marshalContainerStatus(resp.Status)
	if err != nil {
		return nil, err
	}

	containerInfo := &ContainerInfo{}
	containerInfo.PiD, err = outputStatusInfo(status, resp.Info, `{{.info.pid}}`)
	if err != nil {
		return nil, err
	}
	containerInfo.PodName, err = outputStatusInfo(status, resp.Info, `{{ index .info.config.labels "io.kubernetes.pod.name"}}`)
	if err != nil {
		return nil, err
	}
	containerInfo.Namespace, err = outputStatusInfo(status, resp.Info, `{{ index .info.config.labels "io.kubernetes.pod.namespace"}}`)
	if err != nil {
		return nil, err
	}
	return containerInfo, nil
}

func (r *cri) GetContainerPodName(ctx context.Context, containerID string) (string, error) {
	resp, err := r.runtimeClient.ContainerStatus(ctx, containerID, true)
	if err != nil {
		return "", err
	}

	status, err := marshalContainerStatus(resp.Status)
	if err != nil {
		return "", err
	}

	return outputStatusInfo(status, resp.Info, "{{ index .info.config.labels 'io.kubernetes.pod.name'}}")
}
