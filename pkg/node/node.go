package node

import (
	"context"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	invv1alpha1 "github.com/nokia/k8s-ipam/apis/inv/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

// Node is an interface that defines the behavior of a node.
type Node interface {
	// deployment
	GetPodSpec(ctx context.Context, cr *invv1alpha1.Node, nc *invv1alpha1.NodeConfig, nads []*nadv1.NetworkAttachmentDefinition) (*corev1.Pod, error)
	GetNetworkAttachmentDefinitions(ctx context.Context, cr *invv1alpha1.Node, nc *invv1alpha1.NodeConfig) ([]*nadv1.NetworkAttachmentDefinition, error)
	SetInitialConfig(ctx context.Context, cr *invv1alpha1.Node, ips []corev1.PodIP) error
	// node configuration
	GetNodeConfig(ctx context.Context, cr *invv1alpha1.Node) (*invv1alpha1.NodeConfig, error)
	GetNodeModelConfig(ctx context.Context, nc *invv1alpha1.NodeConfig) *corev1.ObjectReference
	GetNodeModel(ctx context.Context, nc *invv1alpha1.NodeConfig) (*invv1alpha1.NodeModel, error)
	GetProviderType(ctx context.Context) ProviderType
}

type ProviderType string

const (
	ProviderTypeServer  ProviderType = "server"
	ProviderTypeNetwork ProviderType = "network"
)
