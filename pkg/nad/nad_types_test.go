package nad

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
)

func TestGetNadConfig(t *testing.T) {
	cases := map[string]struct {
		input []PluginConfigInterface
		want  string
	}{
		"Single": {
			input: []PluginConfigInterface{
				WirePlugin{
					PluginCniType: PluginCniType{
						Type: WirePluginType,
					},
					InterfaceName: "e1-1",
				},
			},
			want: `{"cniVersion":"0.3.1","plugins":[{"interfaceName":"e1-1","type":"wire"}]}`,
		},
		"Multiple": {
			input: []PluginConfigInterface{
				WirePlugin{
					PluginCniType: PluginCniType{
						Type: WirePluginType,
					},
					InterfaceName: "e1-1",
				},
				TuningPlugin{
					PluginCniType: PluginCniType{
						Type: TuningPluginType,
					},
					Capabilities: Capabilities{
						Mac: true,
					},
				},
			},
			want: `{"cniVersion":"0.3.1","plugins":[{"interfaceName":"e1-1","type":"wire"},{"mac":true,"type":"tuning"}]}`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			b, err := GetNadConfig(tc.input)
			if err != nil {
				assert.Error(t, err)
			}
			if diff := cmp.Diff(tc.want, string(b)); diff != "" {
				t.Errorf("-want, +got:\n%s", diff)
			}
		})
	}
}
