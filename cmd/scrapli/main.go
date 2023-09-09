package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/scrapli/scrapligo/driver/opoptions"

	"github.com/scrapli/scrapligo/driver/options"
	"github.com/scrapli/scrapligo/logging"
	"github.com/scrapli/scrapligo/platform"
)

func main() {
	args := os.Args[1:]

	ip := "localhost"
	//port := 22
	if len(args) > 1 {
		ip = args[0]
	}
	/*
		if len(args) > 2 {
			var err error
			port, err = strconv.Atoi(args[1])
			if err != nil {
				panic(err)
			}
		}
	*/
	var channelLog bytes.Buffer

	li, _ := logging.NewInstance(
		logging.WithLevel(logging.Critical),
		logging.WithLogger(log.Print),
	)

	p, err := platform.NewPlatform(
		"nokia_srl",
		ip,
		//options.WithPort(port),
		options.WithAuthNoStrictKey(),
		options.WithAuthUsername("admin"),
		options.WithAuthPassword("NokiaSrl1!"),
		options.WithLogger(li),
		options.WithTransportType("system"),
		options.WithSSHConfigFileSystem(),
		options.WithChannelLog(&channelLog),
		options.WithTimeoutOps(5*time.Second),
	)
	if err != nil {
		panic(err)
	}

	d, err := p.GetNetworkDriver()
	if err != nil {
		panic(err)
	}

	err = d.Open()
	if err != nil {
		panic(err)
	}

	defer d.Close()

	privateKey := `-----BEGIN RSA PRIVATE KEY-----
MIIEpgIBAAKCAQEAywmqbUcq6JkSekEnxDfr318UUvG6xlBs5XNYQGeO7ebXI0jK
cZvSCzvx7PNLncuJvpopKSqOsNTa4fzck7mzrInEJUaG7HxQ+0qg13KUlqRdpuyr
XVTjHmFHXlM/9dGdhDJY4jq9iUm+695oURyPdNxwu4dJppIwKRJhdOYdr/XDS9Ba
VOOyRskYBq8sdWf6Zt0wE5xCzqMuPJ+FymF4PwhEC2DVZJdZwKKGGeuQWN+iFkWc
ZgkNiZVPGs5o5kFC+grds/pJzEZn8UjC/Vsjmv5B6N/4QOoRj3+huUq/mBy2WEKB
+x0L9nzoRsCAX6y3AsWYuMGO53tXNVU1AtCluQIDAQABAoIBAQCOZwgWPtz0aC/S
eRI4B/SyJNBvYEVcRBosT9rsyPUxHD916t66EcyevntucjFtycyhVyRZUBTmJ1Qh
PiVCovNGpxVgA3gsMwDlTrFCioV2pm9c3Q9PlXL54saYfJEWit8MUfePLB21RXjX
m5TUltBy6Q1UKY3Zexy2IcMipybZOqgDOxYOGCV3dw7M7YiY1KlpX34jKcuHE5sB
l/BU13H8IjkN6nR1/ZCrUQOrtkH/43KVO5dCD6ocU8ciW4KV5f78EgYVi3XqPx+a
zdVKYkFolfouE4GRFEEpdH2JIuVjeC0BO6lT8IHxtRZz0a5sSc9wUjwkMTYmT3jI
KZ62eJYRAoGBANPJAjRkYukjR9SsSwncen2IpxzdcI4EIWKJ5SIs+FVuyymb6Jvc
O6FnheUtF34PK5ZL3snnIh0rSGMFOLtVTSva+6N8qYIA6SxbllmOxj2pAmOOMXFT
i98IbDhUpO/60r5bxyuq43ZMYGujVymnNo/NYZFQB78FfcLx8WyUTXj9AoGBAPVt
JSWmGuw9U9wDDf66Ycg+rLq8tmG9c5OQALnnV0O7M6JXKtF/kc0pbB8bs4q3JWu7
Wh2XMk3I7aYGf77sh16gj3aXZCST29l9h1N1kXli5ZeQ74CXa87KnOvtMns6xIh2
EgbbLwrT8ueIjJvX3cvyXTnfHR/ptkApMzCst0ptAoGBAIcB0qf3fp5EYVwP4V4N
8P/phy59c2z08RtR8IGSzVQY5uZFf0ksYc8IoXBxCFLR9OVAxGtNLpANsX1+LKYv
QJy+Yj/cDmrTjdE7KWM6AuH3xZAVaytlKPsq8WIPg32AFaxH8XXC4HHfSnATllL1
R3DwakwqCmYZaAxIE7E18RU5AoGBAPCZZ2lZRduC48s0U2v9XA7rInqOtl1rVPq8
mXmmia4kv6HOwnNPFKiEizKT/ZdnpI/Qw69uoioPaKryhBmv16W00e/4ynvxV/4H
SbtP7qWJhnrn42O1DkNT7jJ7/plAK5t75IBEMAH1dpP1EaNWJGHj3/D0AaFfhQOx
YDW/nJChAoGBALaZSctlykh5haPDDOWG3q035vi/5cMV0Rlmeo+UeTpFjMZhCjFP
zvHigxLa2jCc31VZqZZ1qWs7Lk0HKG8twiZ6fCfDSOYhKrhumMyRb1FZY1hHXMyj
0ulXHepaczK9ydsYclf6ERXB0DphcexkLje/uZ5/i2ci//mplfxl/jAS
-----END RSA PRIVATE KEY-----`

	cert := `-----BEGIN CERTIFICATE-----
MIIC7DCCAdSgAwIBAgIQZta/anah/CUMAN4dTRHvtDANBgkqhkiG9w0BAQsFADAA
MB4XDTIzMDgyNDA0MjE1M1oXDTIzMTEyMjA0MjE1M1owADCCASIwDQYJKoZIhvcN
AQEBBQADggEPADCCAQoCggEBAMsJqm1HKuiZEnpBJ8Q3699fFFLxusZQbOVzWEBn
ju3m1yNIynGb0gs78ezzS53Lib6aKSkqjrDU2uH83JO5s6yJxCVGhux8UPtKoNdy
lJakXabsq11U4x5hR15TP/XRnYQyWOI6vYlJvuveaFEcj3TccLuHSaaSMCkSYXTm
Ha/1w0vQWlTjskbJGAavLHVn+mbdMBOcQs6jLjyfhcpheD8IRAtg1WSXWcCihhnr
kFjfohZFnGYJDYmVTxrOaOZBQvoK3bP6ScxGZ/FIwv1bI5r+Qejf+EDqEY9/oblK
v5gctlhCgfsdC/Z86EbAgF+stwLFmLjBjud7VzVVNQLQpbkCAwEAAaNiMGAwDgYD
VR0PAQH/BAQDAgWgMAwGA1UdEwEB/wQCMAAwQAYDVR0RAQH/BDYwNIIRbGVhZjIu
ZGVmYXVsdC5zdmOCH2xlYWYyLmRlZmF1bHQuc3ZjLmNsdXN0ZXIubG9jYWwwDQYJ
KoZIhvcNAQELBQADggEBAAz3QYTWIBBMlTKBw1DnmRUL8Yrd2Upo/VtgY1Z7EbGr
VS2XF3dEvlXfTs5gJz+Xx2LQwhKr5m+N8CG2mzdqc4Io95wxAGIRmTiCYL/aGxx8
97t2O+D3XSYicDyF5F/tEffbC/zo9zrcbpnHcWWNtNlgUL2WjHd2158Yh5ahy96c
TKCHIHlJC8If4dD/Pmj3oH1B6tR3sQsnzyc5QCI34WHoEfOPXEYR+fmTH/Wz4Mzw
3OMA5pwsi+jUDSeUE4Vxbsm79bYBHAqLSsqdBWd9fQW7871vL2zhPoDYml41xMve
xc2+BVL+GGx5Yx3xg5MXqnUmUuFL7U+0DxMK/sajaEY=
-----END CERTIFICATE-----`

	anchor := `-----BEGIN CERTIFICATE-----
MIIC7DCCAdSgAwIBAgIQZta/anah/CUMAN4dTRHvtDANBgkqhkiG9w0BAQsFADAA
MB4XDTIzMDgyNDA0MjE1M1oXDTIzMTEyMjA0MjE1M1owADCCASIwDQYJKoZIhvcN
AQEBBQADggEPADCCAQoCggEBAMsJqm1HKuiZEnpBJ8Q3699fFFLxusZQbOVzWEBn
ju3m1yNIynGb0gs78ezzS53Lib6aKSkqjrDU2uH83JO5s6yJxCVGhux8UPtKoNdy
lJakXabsq11U4x5hR15TP/XRnYQyWOI6vYlJvuveaFEcj3TccLuHSaaSMCkSYXTm
Ha/1w0vQWlTjskbJGAavLHVn+mbdMBOcQs6jLjyfhcpheD8IRAtg1WSXWcCihhnr
kFjfohZFnGYJDYmVTxrOaOZBQvoK3bP6ScxGZ/FIwv1bI5r+Qejf+EDqEY9/oblK
v5gctlhCgfsdC/Z86EbAgF+stwLFmLjBjud7VzVVNQLQpbkCAwEAAaNiMGAwDgYD
VR0PAQH/BAQDAgWgMAwGA1UdEwEB/wQCMAAwQAYDVR0RAQH/BDYwNIIRbGVhZjIu
ZGVmYXVsdC5zdmOCH2xlYWYyLmRlZmF1bHQuc3ZjLmNsdXN0ZXIubG9jYWwwDQYJ
KoZIhvcNAQELBQADggEBAAz3QYTWIBBMlTKBw1DnmRUL8Yrd2Upo/VtgY1Z7EbGr
VS2XF3dEvlXfTs5gJz+Xx2LQwhKr5m+N8CG2mzdqc4Io95wxAGIRmTiCYL/aGxx8
97t2O+D3XSYicDyF5F/tEffbC/zo9zrcbpnHcWWNtNlgUL2WjHd2158Yh5ahy96c
TKCHIHlJC8If4dD/Pmj3oH1B6tR3sQsnzyc5QCI34WHoEfOPXEYR+fmTH/Wz4Mzw
3OMA5pwsi+jUDSeUE4Vxbsm79bYBHAqLSsqdBWd9fQW7871vL2zhPoDYml41xMve
xc2+BVL+GGx5Yx3xg5MXqnUmUuFL7U+0DxMK/sajaEY=
-----END CERTIFICATE-----`

	banner := `................................................................
:                  Welcome to Nokia SR Linux!                  :
:              Open Network OS for the NetOps era.             :
:                                                              :
:    This is a freely distributed official container image.    :
:                      Use it - Share it                       :
:                                                              :
: Get started: https://learn.srlinux.dev                       :
: Container:   https://go.srlinux.dev/container-image          :
: Docs:        https://doc.srlinux.dev/22-11                   :
: Rel. notes:  https://doc.srlinux.dev/rn22-11-2               :
: YANG:        https://yang.srlinux.dev/v22.11.2               :
: Discord:     https://go.srlinux.dev/discord                  :
: Contact:     https://go.srlinux.dev/contact-sales            :
................................................................`

	configs := []string{
		"set / system tls server-profile k8s-profile",
		fmt.Sprintf("set / system tls server-profile k8s-profile key \"%s\"", privateKey),
		fmt.Sprintf("set / system tls server-profile k8s-profile certificate \"%s\"", cert),
		fmt.Sprintf("set / system tls server-profile k8s-profile trust-anchor \"%s\"", anchor),
		"set / system lldp admin state enable",
		"set / system gnmi-server admin-state enable",
		"set / system gnmi-server rate-limit 65000",
		"set / system gnmi-server trace-options [ common request response ]",
		"set / system gnmi-server network-instance mgmt admin-state enable",
		"set / system gnmi-server network-instance mgmt tls-profile k8s-profile",
		"set / system gnmi-server network-instance mgmt unix-socket admin-state enable",
		"set / system gribi-server admin-state enable",
		"set / system gribi-server network-instance mgmt admin-state enable",
		"set / system gribi-server network-instance mgmt tls-profile k8s-profile",
		"set / system json-rpc-server admin-state enable",
		"set / system json-rpc-server network-instance mgmt http admin-state enable",
		"set / system json-rpc-server network-instance mgmt https admin-state enable",
		"set / system json-rpc-server network-instance mgmt https tls-profile k8s-profile",
		"set / system p4rt-server admin-state enable",
		"set / system p4rt-server network-instance mgmt admin-state enable",
		"set / system p4rt-server network-instance mgmt tls-profile k8s-profile",
		fmt.Sprintf("set / system banner login-banner \"%s\"", banner),
		fmt.Sprintf("set / system banner motd-banner \"%s\"", banner),
		"commit save",
	}

	r, err := d.SendConfigs(configs, opoptions.WithFuzzyMatchInput())
	if err != nil {
		panic(err)
	} else {
		fmt.Println("Failed?: ", r.Failed)
	}

	prompt, err := d.GetPrompt()
	if err != nil {
		fmt.Printf("failed to get prompt; error: %+v\n", err)
		panic(err)
	}

	fmt.Printf("found prompt: %s\n\n\n", prompt)

	cb := make([]byte, channelLog.Len())
	_, _ = channelLog.Read(cb)
	fmt.Printf("Channel log output:\n%s", cb)
}
