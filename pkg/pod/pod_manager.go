package pod

import (
	"fmt"
	"os"
	"sync"

	invv1alpha1 "github.com/nokia/k8s-ipam/apis/inv/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	criv1 "k8s.io/cri-api/pkg/apis/runtime/v1"
)

type Manager interface {
	UpsertPod(nsn types.NamespacedName, pod *corev1.Pod)
	DeletePod(nsn types.NamespacedName)
	GetPod(nsn types.NamespacedName) (*PodCtx, error)
	ListPods() map[string]PodCtx
	UpsertContainer(nsn types.NamespacedName, containerName string, c *ContainerCtx)
}

type manager struct {
	m sync.RWMutex

	pods map[types.NamespacedName]PodCtx
}

type PodCtx struct {
	HostIP           string
	HostConnectivity invv1alpha1.HostConnectivity
	Containers       map[string]ContainerCtx
}

type ContainerCtx struct {
	Name   string
	ID     string
	Pid    string
	NSPath string
	State  criv1.ContainerState
}

func NewManager() Manager {
	return &manager{
		pods: map[types.NamespacedName]PodCtx{},
	}
}

func getHostConnectivity(pod *corev1.Pod) (invv1alpha1.HostConnectivity) {
	if len(pod.Status.ContainerStatuses) == 0 {
		return invv1alpha1.HostConnectivityUnknown
	}
	if !pod.Status.ContainerStatuses[0].Ready {
		return invv1alpha1.HostConnectivityUnknown
	}
	if len(pod.Status.PodIPs) == 0 {
		return invv1alpha1.HostConnectivityUnknown
	}
	switch {
	case pod.Status.HostIP != "" && pod.Status.HostIP != os.Getenv("NODE_IP"):
		return invv1alpha1.HostConnectivityRemote
	case pod.Status.HostIP != "" && pod.Status.HostIP == os.Getenv("NODE_IP"):
		return invv1alpha1.HostConnectivityLocal
	default:
		return invv1alpha1.HostConnectivityUnknown
	}
}

func (r *manager) UpsertPod(nsn types.NamespacedName, pod *corev1.Pod) {
	r.m.Lock()
	defer r.m.Unlock()

	r.pods[nsn] = PodCtx{
		HostIP:           pod.Status.HostIP,
		HostConnectivity: getHostConnectivity(pod),
		Containers:       map[string]ContainerCtx{},
	}
}

func (r *manager) DeletePod(nsn types.NamespacedName) {
	r.m.Lock()
	defer r.m.Unlock()

	delete(r.pods, nsn)
}

func (r *manager) GetPod(nsn types.NamespacedName) (*PodCtx, error) {
	r.m.RLock()
	defer r.m.RUnlock()

	podCtx, ok := r.pods[nsn]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return getNewPodCtx(&podCtx), nil
}

func (r *manager) ListPods() map[string]PodCtx {
	r.m.RLock()
	defer r.m.RUnlock()

	pods := map[string]PodCtx{}
	for podNSN, podCtx := range r.pods {
		newPodCtx := getNewPodCtx(&podCtx)
		pods[podNSN.String()] = *newPodCtx
	}
	return pods
}

func getNewPodCtx(podCtx *PodCtx) *PodCtx {
	newPodCtx := &PodCtx{
		HostIP:           podCtx.HostIP,
		HostConnectivity: podCtx.HostConnectivity,
		Containers:       map[string]ContainerCtx{},
	}
	for cName, cCtx := range podCtx.Containers {
		newPodCtx.Containers[cName] = cCtx
	} 
	return newPodCtx
}

func (r *manager) UpsertContainer(nsn types.NamespacedName, containerName string, c *ContainerCtx) {
	r.m.Lock()
	defer r.m.Unlock()

	if _, ok := r.pods[nsn]; !ok {
		r.pods[nsn] = PodCtx{
			// TBD what to do with HostIP ?
			Containers: map[string]ContainerCtx{},
		}
	}

	cCtx := *c
	cCtx.Name = containerName

	r.pods[nsn].Containers[containerName] = cCtx
}
