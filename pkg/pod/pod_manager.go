package pod

import (
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	criv1 "k8s.io/cri-api/pkg/apis/runtime/v1"
)

type Manager interface {
	UpsertPod(nsn types.NamespacedName, pod *corev1.Pod)
	DeletePod(nsn types.NamespacedName)
	ListPods() map[string][]ContainerCtx
	UpsertContainer(nsn types.NamespacedName, containerName string, c *ContainerCtx)
}

type manager struct {
	m sync.RWMutex

	podByName map[types.NamespacedName]*podCtx
}

type podCtx struct {
	pod             *corev1.Pod
	containerByName map[string]ContainerCtx
}

type ContainerCtx struct {
	Name  string
	ID    string
	Pid   string
	State criv1.ContainerState
}

func NewManager() Manager {
	return &manager{
		podByName: map[types.NamespacedName]*podCtx{},
	}
}

func (r *manager) UpsertPod(nsn types.NamespacedName, pod *corev1.Pod) {
	r.m.Lock()
	defer r.m.Unlock()

	r.podByName[nsn] = &podCtx{
		pod:             pod,
		containerByName: map[string]ContainerCtx{},
	}
}

func (r *manager) DeletePod(nsn types.NamespacedName) {
	r.m.Lock()
	defer r.m.Unlock()

	delete(r.podByName, nsn)
}

func (r *manager) ListPods() map[string][]ContainerCtx {
	r.m.RLock()
	defer r.m.RUnlock()

	pods := map[string][]ContainerCtx{}
	for podNSN, pod := range r.podByName {
		pods[podNSN.String()] = make([]ContainerCtx, 0, len(pod.containerByName))
		for _, cCtx := range pod.containerByName {
			pods[podNSN.String()] = append(pods[podNSN.String()], cCtx)
		}
	}
	return pods
}

func (r *manager) UpsertContainer(nsn types.NamespacedName, containerName string, c *ContainerCtx) {
	r.m.Lock()
	defer r.m.Unlock()

	if _, ok := r.podByName[nsn]; !ok {
		r.podByName[nsn].containerByName = map[string]ContainerCtx{}
	}

	cCtx := *c
	cCtx.Name = containerName
	r.podByName[nsn].containerByName[containerName] = cCtx
}
