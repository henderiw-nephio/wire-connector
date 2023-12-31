package commands

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vishvananda/netlink"
)

// configCmd represents the config command.
var listCmd = &cobra.Command{
	Use:          "list",
	Short:        "list ip devices",
	Long:         "list ip devices",
	Aliases:      []string{"l"},
	SilenceUsage: true,
	RunE:         listRun,
}

func listRun(_ *cobra.Command, args []string) error {
	ll, err := netlink.LinkList()
	if err != nil {
		return err
	}

	for _, l := range ll {

		fmt.Printf("link: %s index/parentIndex: %d/%d type: %s mtu: %d flags: %s\n", l.Attrs().Name, l.Attrs().Index, l.Attrs().ParentIndex, l.Type(), l.Attrs().MTU, l.Attrs().Flags.String())
		fmt.Printf("  oper-state: %s\n", l.Attrs().OperState.String())
		if l.Type() == "vxlan" {
			fmt.Printf("  group %d\n", l.Attrs().Group)
			fmt.Printf("  txqlen %d\n", l.Attrs().TxQLen)
			vxlan, ok := l.(*netlink.Vxlan)
			if ok {
				fmt.Printf("  src ip: %s\n", vxlan.SrcAddr)
				fmt.Printf("  dst ip: %s\n", vxlan.Group.String())
				fmt.Printf("  port: %d\n", vxlan.Port)
				fmt.Printf("  vxlanid: %d\n", vxlan.VxlanId)
				fmt.Printf("  l2miss: %t\n", vxlan.L2miss)
				fmt.Printf("  l3miss: %t\n", vxlan.L3miss)
				fmt.Printf("  learning: %t\n", vxlan.Learning)

			}
		}
		if l.Attrs().Xdp.Attached == true {
			fmt.Printf("  xdp attached %t, xdp mode: %d\n", l.Attrs().Xdp.Attached, l.Attrs().Xdp.AttachMode)
		}

	}
	return nil
}

func init() {
	rootCmd.AddCommand(listCmd)
}
