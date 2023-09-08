package xserver

import (
	"context"
	"fmt"
	"os"

	"github.com/henderiw-nephio/wire-connector/pkg/node"
	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	invv1alpha1 "github.com/nokia/k8s-ipam/apis/inv/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ServerProvider       = "x.server.com"
	defaultServerVariant = "server1"
	variantsCfgMapName   = "x.server.com-variants"
)

// Register registers the node in the NodeRegistry.
func Register(r node.NodeRegistry) {
	r.Register(ServerProvider, func(c client.Client, s *runtime.Scheme) node.Node {
		return &server{
			Client: c,
			scheme: s,
		}
	})
}

type server struct {
	client.Client
	scheme *runtime.Scheme
}

func (r *server) ToBeDeployed(ctx context.Context) bool {
	return false
}

func (r *server) GetProviderType(ctx context.Context) node.ProviderType {
	return node.ProviderTypeServer
}

func (r *server) GetNodeConfig(ctx context.Context, cr *invv1alpha1.Node) (*invv1alpha1.NodeConfig, error) {
	// get nodeConfig via paramRef
	nodeConfig, err := r.getNodeConfig(ctx, cr)
	if err != nil {
		return nil, err
	}

	// validate if the model returned exists in the variant list
	if err := r.checkVariants(ctx, cr, nodeConfig.GetModel(defaultServerVariant)); err != nil {
		return nil, err
	}
	return nodeConfig, nil
}

func (r *server) GetNodeModelConfig(ctx context.Context, nc *invv1alpha1.NodeConfig) *corev1.ObjectReference {
	return &corev1.ObjectReference{
		APIVersion: invv1alpha1.NodeKindAPIVersion,
		Kind:       invv1alpha1.NodeModelKind,
		Name:       fmt.Sprintf("%s-%s", ServerProvider, nc.GetModel(defaultServerVariant)),
		Namespace:  os.Getenv("POD_NAMESPACE"),
	}
}

func (r *server) GetNodeModel(ctx context.Context, nc *invv1alpha1.NodeConfig) (*invv1alpha1.NodeModel, error) {
	nm := &invv1alpha1.NodeModel{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      fmt.Sprintf("%s-%s", ServerProvider, nc.GetModel(defaultServerVariant)),
		Namespace: os.Getenv("POD_NAMESPACE"),
	}, nm); err != nil {
		return nil, err
	}
	return nm, nil
}

func (r *server) GetNetworkAttachmentDefinitions(ctx context.Context, cr *invv1alpha1.Node, nc *invv1alpha1.NodeConfig) ([]*nadv1.NetworkAttachmentDefinition, error) {
	// todo check node model and get interfaces from the model
	nads := []*nadv1.NetworkAttachmentDefinition{}
	return nads, nil
}

func (r *server) GetPodSpec(ctx context.Context, cr *invv1alpha1.Node, nc *invv1alpha1.NodeConfig, nads []*nadv1.NetworkAttachmentDefinition) (*corev1.Pod, error) {
	d := &corev1.Pod{}
	return d, nil
}

func (r *server) SetInitialConfig(ctx context.Context, cr *invv1alpha1.Node, ips []corev1.PodIP) error {
	return nil

}

func (r *server) getNodeConfig(ctx context.Context, cr *invv1alpha1.Node) (*invv1alpha1.NodeConfig, error) {
	if cr.Spec.NodeConfig != nil && cr.Spec.NodeConfig.Name != "" {
		nc := &invv1alpha1.NodeConfig{}
		if err := r.Get(ctx, types.NamespacedName{Name: cr.Spec.NodeConfig.Name, Namespace: os.Getenv("POD_NAMESPACE")}, nc); err != nil {
			return nil, err
		}
		return nc, nil

	}
	// the nodeConfig was not provided, we list all nodeConfigs in the cr namespace
	// we check if there is a nodeconfig with the name equal to the cr name + the provider matches
	// if still not found we look at a nodeconfig with name default that matches the provider
	// if still not found we return an empty nodeConfig, which populates the defaults

	opts := []client.ListOption{
		client.InNamespace(os.Getenv("POD_NAMESPACE")),
	}
	ncl := &invv1alpha1.NodeConfigList{}
	if err := r.List(ctx, ncl, opts...); err != nil {
		return nil, err
	}

	for _, nc := range ncl.Items {
		// if there is a nodeconfig with the exact name of the node -> we return this nodeConfig
		if nc.GetName() == cr.GetName() && cr.Spec.Provider == nc.Spec.Provider {
			return &nc, nil
		}
	}
	for _, nc := range ncl.Items {
		// if there is a nodeconfig with the name default -> we return this nodeConfig
		if nc.GetName() == "default" && cr.Spec.Provider == nc.Spec.Provider {
			return &nc, nil
		}

	}
	// if nothing is found we return an empty nodeconfig
	return &invv1alpha1.NodeConfig{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cr.GetNamespace(),
		},
	}, nil
}

func (r *server) checkVariants(ctx context.Context, cr *invv1alpha1.Node, model string) error {
	variants := &corev1.ConfigMap{}
	if err := r.Get(ctx, types.NamespacedName{Name: variantsCfgMapName, Namespace: os.Getenv("POD_NAMESPACE")}, variants); err != nil {
		return err
	}
	if _, ok := variants.Data[model]; !ok {
		return fmt.Errorf("cannot deploy pod, variant not provided in the configmap, got: %s", model)
	}
	return nil
}
