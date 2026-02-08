package status

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"paqet/internal/conf"
	"time"

	"github.com/spf13/cobra"
)

var (
	confPath  string
	pprofAddr string
	jsonOut   bool
	timeout   time.Duration
)

func init() {
	Cmd.Flags().StringVarP(&confPath, "config", "c", "/etc/paqet/config.yaml", "Path to the configuration file (used to detect debug endpoints).")
	Cmd.Flags().StringVar(&pprofAddr, "pprof", "", "Debug HTTP bind address (host:port). If empty, uses config or defaults to 127.0.0.1:6060.")
	Cmd.Flags().BoolVar(&jsonOut, "json", false, "Print JSON from /debug/paqet/status instead of text.")
	Cmd.Flags().DurationVar(&timeout, "timeout", 2*time.Second, "HTTP timeout (e.g. 500ms, 2s).")
}

var Cmd = &cobra.Command{
	Use:   "status",
	Short: "Prints live status from the local debug endpoints",
	Run: func(cmd *cobra.Command, args []string) {
		if err := run(); err != nil {
			log.Fatalf("%v", err)
		}
	},
}

func run() error {
	addr, diagEnabled, err := resolveDebugAddr()
	if err != nil {
		return err
	}

	path := "/debug/paqet/text"
	if jsonOut {
		path = "/debug/paqet/status"
	}
	url := fmt.Sprintf("http://%s%s", addr, path)

	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to reach %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		hint := ""
		if !diagEnabled {
			hint = "\n\nHint: debug endpoints are disabled. Enable via `sudo paqet-ui` -> Diagnostics -> Enable debug endpoints (pprof+diag), then restart."
		}
		return fmt.Errorf("debug endpoint returned %s: %s%s", resp.Status, string(body), hint)
	}

	_, _ = os.Stdout.Write(body)
	return nil
}

func resolveDebugAddr() (addr string, diagEnabled bool, err error) {
	if pprofAddr != "" {
		return pprofAddr, true, nil
	}

	if confPath != "" {
		if _, statErr := os.Stat(confPath); statErr == nil {
			cfg, loadErr := conf.LoadFromFile(confPath)
			if loadErr != nil {
				return "", false, loadErr
			}
			if cfg.Debug.Pprof != "" {
				addr = cfg.Debug.Pprof
			}
			diagEnabled = cfg.Debug.Diag
		} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return "", false, statErr
		}
	}

	if addr == "" {
		addr = "127.0.0.1:6060"
	}
	return addr, diagEnabled, nil
}
