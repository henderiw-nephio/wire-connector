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
		fmt.Printf("link: %s\n", l.Attrs().Name)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(listCmd)
}
