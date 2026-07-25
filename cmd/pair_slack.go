package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/rs/zerolog"

	"github.com/maxghenis/openmessage/internal/app"
)

// RunPairSlack pairs a Slack workspace via a user token (xoxp-...).
func RunPairSlack(logger zerolog.Logger, args ...string) error {
	token := ""
	name := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--token":
			if i+1 >= len(args) {
				return fmt.Errorf("--token requires a value")
			}
			i++
			token = args[i]
		case "--name":
			if i+1 >= len(args) {
				return fmt.Errorf("--name requires a value")
			}
			i++
			name = args[i]
		case "-h", "--help":
			fmt.Println(`Usage: openmessage pair slack [--token xoxp-...] [--name "Workspace"]

Paste a Slack user token with scopes for channels/groups/im history and chat:write.
Credentials are stored encrypted in the river vault (DPAPI on Windows).`)
			return nil
		default:
			return fmt.Errorf("unknown argument %q", args[i])
		}
	}
	if strings.TrimSpace(token) == "" {
		fmt.Print("Slack user token (xoxp-...): ")
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			return err
		}
		token = strings.TrimSpace(line)
	}
	if token == "" {
		return fmt.Errorf("token required")
	}

	a, err := app.New(logger)
	if err != nil {
		return err
	}
	defer a.Close()

	riverRow, err := a.PairSlackRiver(token, name)
	if err != nil {
		return err
	}
	fmt.Printf("\nPaired Slack river %q (%s)\n", riverRow.DisplayName, riverRow.ID)
	fmt.Println("Credentials stored in the river vault. Run: openmessage serve --api")
	fmt.Println("In the TUI, press [ / ] to switch rivers.")
	return nil
}
