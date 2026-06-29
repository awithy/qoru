package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/awithy/qoru/internal/client"
	"github.com/awithy/qoru/internal/config"
	"github.com/awithy/qoru/internal/server"
	"github.com/spf13/cobra"
)

type rootOptions struct {
	configPath string
}

type runnerFunc func(context.Context, *config.Config, *slog.Logger) error

type commandRunners struct {
	client runnerFunc
	server runnerFunc
}

func NewRootCommand() *cobra.Command {
	return newRootCommand(commandRunners{
		client: runClient,
		server: runServer,
	})
}

func runClient(ctx context.Context, cfg *config.Config, logger *slog.Logger) error {
	return client.Run(ctx, cfg, logger)
}

func runServer(ctx context.Context, cfg *config.Config, logger *slog.Logger) error {
	return server.Run(ctx, cfg, logger)
}

func newRootCommand(runners commandRunners) *cobra.Command {
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
	cmd.AddCommand(newClientCommand(opts, runners.client))
	cmd.AddCommand(newServerCommand(opts, runners.server))
	cmd.AddCommand(newPrintConfigCommand(opts))

	return cmd
}

func newClientCommand(opts *rootOptions, runner runnerFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "client",
		Short: "Run in client mode",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfig(opts)
			if err != nil {
				return err
			}
			return runner(cmd.Context(), cfg, newLogger(cmd))
		},
	}
}

func newServerCommand(opts *rootOptions, runner runnerFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "server",
		Short: "Run in server mode",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfig(opts)
			if err != nil {
				return err
			}
			return runner(cmd.Context(), cfg, newLogger(cmd))
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

func newLogger(cmd *cobra.Command) *slog.Logger {
	return slog.New(slog.NewTextHandler(cmd.OutOrStdout(), nil))
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd := NewRootCommand()
	cmd.SetContext(ctx)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
