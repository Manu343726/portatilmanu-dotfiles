package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"dotfilesd/internal/pkg/daemon"
	"dotfilesd/internal/pkg/shared"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newRootCmd() *cobra.Command {
	var (
		rpcPort    string
		noVerify   bool
		logDir     string
		logLevel   string
		logMaxMB   int
		logBackup  int
		logAge     int
		scriptsDir string
	)

	cmd := &cobra.Command{
		Use:   "dotfilesd",
		Short: "dotfiles runtime daemon (Connect RPC)",
		RunE: func(cmd *cobra.Command, args []string) error {
			shared.CheckBuildHash(buildHash, noVerify, "dotfilesd")

			viper.SetConfigName("config")
			viper.SetConfigType("yaml")
			viper.AddConfigPath("$HOME/.config/dotfilesd")
			viper.AutomaticEnv()
			viper.SetEnvPrefix("DOTFILESD")

			if err := viper.ReadInConfig(); err != nil {
				if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
					fmt.Fprintf(os.Stderr, "config error: %v\n", err)
				}
			}

			rpcPort = firstNonEmpty(rpcPort, viper.GetString("port"), os.Getenv("DOTFILESD_PORT"), "9105")
			logDir = firstNonEmpty(logDir, viper.GetString("log.dir"), os.Getenv("DOTFILESD_LOG_DIR"), os.Getenv("HOME")+"/dotfilesd/logs")
			logDir = strings.Replace(logDir, "~", os.Getenv("HOME"), 1)
			logLevel = firstNonEmpty(logLevel, viper.GetString("log.level"), os.Getenv("DOTFILESD_LOG_LEVEL"), "info")
			logMaxMB = firstNonZeroInt(logMaxMB, viper.GetInt("log.max_size_mb"), 10)
			logBackup = firstNonZeroInt(logBackup, viper.GetInt("log.max_backups"), 5)
			logAge = firstNonZeroInt(logAge, viper.GetInt("log.max_age_days"), 30)
			scriptsDir = firstNonEmpty(scriptsDir, viper.GetString("scripts_dir"), os.Getenv("DOTFILESD_SCRIPTS_DIR"), "")

			d := daemon.New(daemon.Config{
				Port:       rpcPort,
				LogDir:     logDir,
				LogLevel:   logLevel,
				LogMaxMB:   logMaxMB,
				LogBackup:  logBackup,
				LogAge:     logAge,
				ScriptsDir: scriptsDir,
			})

			if err := d.Start(); err != nil && err != http.ErrServerClosed {
				slog.Error("daemon error", "error", err)
				return err
			}
			return nil
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	cmd.AddCommand(newVersionCmd())

	cmd.Flags().StringVarP(&rpcPort, "port", "p", "", "RPC port (env DOTFILESD_PORT, config: port)")
	cmd.Flags().BoolVar(&noVerify, "no-verify", false, "skip source version check")
	cmd.Flags().StringVar(&logDir, "log-dir", "", "log directory (config: log.dir)")
	cmd.Flags().StringVar(&logLevel, "log-level", "", "log level: trace|debug|info|warn|error (config: log.level)")
	cmd.Flags().IntVar(&logMaxMB, "log-max-size", 0, "max MB per log file (config: log.max_size_mb)")
	cmd.Flags().IntVar(&logBackup, "log-max-backups", 0, "max rotated files (config: log.max_backups)")
	cmd.Flags().IntVar(&logAge, "log-max-age", 0, "max days to keep logs (config: log.max_age_days)")

	return cmd
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstNonZeroInt(vals ...int) int {
	for _, v := range vals {
		if v != 0 {
			return v
		}
	}
	return 0
}
