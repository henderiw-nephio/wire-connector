package node

import (
	"sync"

	corev1 "k8s.io/api/core/v1"
)

type Manager interface {
	UpsertNode(name string, node *corev1.Node)
	DeleteNode(name string)
	ListNodes() map[string]corev1.Node
}

type manager struct {
	m sync.RWMutex

	nodeByName map[string]corev1.Node
}

func NewManager() Manager {
	return &manager{
		nodeByName: map[string]corev1.Node{},
	}
}

func (r *manager) UpsertNode(name string, node *corev1.Node) {
	r.m.Lock()
	defer r.m.Unlock()

	r.nodeByName[name] = *node
}

func (r *manager) DeleteNode(name string) {
	r.m.Lock()
	defer r.m.Unlock()

	delete(r.nodeByName, name)
}

func (r *manager) ListNodes() map[string]corev1.Node {
	r.m.RLock()
	defer r.m.RUnlock()

	nodes := map[string]corev1.Node{}
	for nodeName, n := range r.nodeByName {
		nodes[nodeName] = n
	}
	return nodes
}
