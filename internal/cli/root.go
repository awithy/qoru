package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

type rootOptions struct {
	configPath string
}

func NewRootCommand() *cobra.Command {
	opts := &rootOptions{}

	cmd := &cobra.Command{
		Use:   "gorelay",
		Short: "Experimental chainable network relay/proxy",
		Long:  "gorelay is an experimental chainable network relay/proxy for TCP and UDP connections.",
	}

	cmd.PersistentFlags().StringVarP(&opts.configPath, "config", "c", "", "path to gorelay config file")

	return cmd
}

// Execute runs the root CLI command.
func Execute() {
	if err := NewRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
