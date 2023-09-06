package nad

import (
	"encoding/json"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
)

const (
	CniVersion = "0.3.1"
	// cni types
	WirePluginType   = "wire"
	TuningPluginType = "tuning"
)

type NadConfig struct {
	CniVersion string           `json:"cniVersion,omitempty"`
	Plugins    []map[string]any `json:"plugins,omitempty"`
}

// key = type; plugin should become a init fn
var Plugins = map[string]PluginConfigInterface{
	"WirePlugin":   WirePlugin{},
	"TuningPlugin": TuningPlugin{},
}

type PluginConfigInterface interface{}

type WirePlugin struct {
	PluginCniType
	InterfaceName string `json:"interfaceName,omitempty"`
	MTU           int    `json:"mtu,omitempty"`
}

type TuningPlugin struct {
	PluginCniType
	Capabilities
}

type Capabilities struct {
	Ips bool `json:"ips,omitempty"`
	Mac bool `json:"mac,omitempty"`
}

type PluginCniType struct {
	Type string `json:"type,omitempty"`
	Name string `json:"name,omitempty"`
}

func GetNadConfig(plugins []PluginConfigInterface) ([]byte, error) {
	nadConfig := NadConfig{
		CniVersion: CniVersion,
		Plugins:    []map[string]any{},
	}

	for _, plugin := range plugins {
		b, err := json.Marshal(plugin)
		if err != nil {
			return nil, err
		}
		x := map[string]any{}
		if err := json.Unmarshal(b, &x); err != nil {
			return nil, err
		}
		nadConfig.Plugins = append(nadConfig.Plugins, x)
	}
	return json.Marshal(nadConfig)
}

func GetNadAnnotation(nads []*nadv1.NetworkAttachmentDefinition) ([]byte, error) {
	a := []NadAnnotationEntry{}
	for _, nad := range nads {
		a = append(a, NadAnnotationEntry{
			Name: nad.GetName(),
		})
	}
	return json.Marshal(a)
}

type NadAnnotation []NadAnnotationEntry

type NadAnnotationEntry struct {
	Name string `json:"name,omitempty"`
}
