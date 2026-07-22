package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/rs/zerolog"

	"github.com/maxghenis/openmessage/internal/app"
	"github.com/maxghenis/openmessage/internal/tui"
)

// RunTUI starts the Google Messages terminal UI. It attaches to a running
// local API daemon or starts `serve --api --no-web` when none is available.
func RunTUI(logger zerolog.Logger, args ...string) error {
	for _, arg := range args {
		if arg != "" {
			return fmt.Errorf("unknown tui option: %s", arg)
		}
	}

	dataDir := app.DefaultDataDir()
	ctx, stop := signal.NotifyContext(context.Background(), interruptSignals()...)
	defer stop()

	logger.Info().Str("data_dir", dataDir).Msg("Ensuring local API daemon")
	session, err := tui.EnsureDaemon(ctx, dataDir)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := session.Close(); cerr != nil {
			logger.Warn().Err(cerr).Msg("Failed to stop owned daemon")
		}
	}()

	if session.Owned {
		fmt.Fprintf(os.Stderr, "Started local API daemon at %s (owned by this TUI)\n", session.BaseURL)
	} else {
		fmt.Fprintf(os.Stderr, "Attached to local API daemon at %s\n", session.BaseURL)
	}

	return tui.Run(session)
}
