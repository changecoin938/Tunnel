package run

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"paqet/internal/conf"
	"paqet/internal/diag"
	"paqet/internal/flog"

	"github.com/spf13/cobra"
)

var confPath string

func init() {
	Cmd.Flags().StringVarP(&confPath, "config", "c", "config.yaml", "Path to the configuration file.")
}

var Cmd = &cobra.Command{
	Use:   "run",
	Short: "Runs the client or server based on the config file.",
	Long:  `The 'run' command reads the specified YAML configuration file.`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := conf.LoadFromFile(confPath)
		if err != nil {
			log.Fatalf("Failed to load configuration: %v", err)
		}
		initialize(cfg)

		switch cfg.Role {
		case "client":
			startClient(cfg)
			return
		case "server":
			startServer(cfg)
			return
		}

		log.Fatalf("Failed to load configuration")
	},
}

func initialize(cfg *conf.Conf) {
	flog.SetLevel(cfg.Log.Level)
	diag.Enable(cfg.Debug.Diag)
	guard := false
	if cfg.Transport.KCP != nil && cfg.Transport.KCP.Guard != nil && *cfg.Transport.KCP.Guard {
		guard = true
	}
	if cfg.Debug.Diag {
		keyID := ""
		if cfg.Transport.KCP != nil && cfg.Transport.KCP.Key != "" {
			sum := sha256.Sum256([]byte(cfg.Transport.KCP.Key))
			keyID = hex.EncodeToString(sum[:8])
		}
		diag.SetConfig(diag.ConfigInfo{
			Role:      cfg.Role,
			Interface: cfg.Network.Interface_,
			IPv4Addr:  cfg.Network.IPv4.Addr_,
			IPv6Addr:  cfg.Network.IPv6.Addr_,
			ServerAddr: func() string {
				if cfg.Role == "client" {
					return cfg.Server.Addr_
				}
				return ""
			}(),
			ListenAddr: func() string {
				if cfg.Role == "server" {
					return cfg.Listen.Addr_
				}
				return ""
			}(),
			Pprof: cfg.Debug.Pprof,
			Guard: guard,
			Conns: cfg.Transport.Conn,
			KeyID: keyID,
		})
		diag.RegisterHTTP()
	}
	startPprof(cfg.Debug.Pprof)
}
