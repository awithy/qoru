package cli

import (
	"fmt"
	"os"

	"github.com/awithy/qoru/internal/config"
	"github.com/spf13/cobra"
)

type rootOptions struct {
	configPath string
}

func NewRootCommand() *cobra.Command {
	opts := &rootOptions{}

	cmd := &cobra.Command{
		Use:   "qoru",
		Short: "Experimental chainable network relay/proxy",
		Long:  "qoru is an experimental chainable network relay/proxy for TCP and UDP connections.",
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Help()
		},
	}

	cmd.PersistentFlags().StringVarP(&opts.configPath, "config", "c", "", "path to qoru config file")
	cmd.AddCommand(newClientCommand(opts))
	cmd.AddCommand(newServerCommand(opts))
	cmd.AddCommand(newPrintConfigCommand(opts))

	return cmd
}

func newClientCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "client",
		Short: "Run in client mode",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, path, err := loadConfig(opts)
			if err != nil {
				return err
			}
			if err := config.ValidateClient(cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "starting client node %s using %s\n", cfg.NodeID, path)
			return nil
		},
	}
}

func newServerCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "server",
		Short: "Run in server mode",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, path, err := loadConfig(opts)
			if err != nil {
				return err
			}
			if err := config.ValidateServer(cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "starting server node %s using %s\n", cfg.NodeID, path)
			return nil
		},
	}
}

func newPrintConfigCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "print-config",
		Short: "Print resolved config and exit",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfig(opts)
			if err != nil {
				return err
			}
			if err := config.ValidateForMode(cfg); err != nil {
				return err
			}
			out, err := config.MarshalYAML(*cfg)
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(out)
			return err
		},
	}
}

func loadConfig(opts *rootOptions) (*config.Config, string, error) {
	path, ok := config.ResolvePath(opts.configPath)
	if !ok {
		return nil, "", fmt.Errorf("no config file found; specify one with --config")
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, "", err
	}
	return cfg, path, nil
}

// Execute runs the root CLI command.
func Execute() {
	if err := NewRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
