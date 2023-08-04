package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vishvananda/netlink"
)

// prefix to delete
var prefix string

// configCmd represents the config command.
var deleteCmd = &cobra.Command{
	Use:          "delete",
	Short:        "delete ip devices",
	Long:         "delete ip devices",
	Aliases:      []string{"l"},
	SilenceUsage: true,
	RunE:         deleteRun,
}

func deleteRun(_ *cobra.Command, args []string) error {
	ll, err := netlink.LinkList()
	if err != nil {
		return err
	}

	for _, l := range ll {
		if prefix != "" && strings.HasPrefix(l.Attrs().Name, prefix) {
			fmt.Printf("delete link: %s\n", l.Attrs().Name)
			if err := netlink.LinkDel(l); err != nil {
				fmt.Printf("cannot delete link: %s\n", l.Attrs().Name)
			}
		}
	}
	return nil
}

func init() {
	rootCmd.AddCommand(deleteCmd)
	deleteCmd.PersistentFlags().StringVarP(&prefix, "prefix", "p", "", "link prefix")
}
