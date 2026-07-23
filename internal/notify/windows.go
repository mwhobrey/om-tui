package notify

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/maxghenis/openmessage/internal/db"
)

// Default toast AppUserModelID. Unregistered IDs often silently no-op on
// Windows, so we default to PowerShell's registered AUMID (toasts branded as
// PowerShell). Override with OPENMESSAGES_WINDOWS_TOAST_APP_ID after registering
// your own Start Menu shortcut / AUMID.
const defaultWindowsAppID = `{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe`

type WindowsNotifier struct {
	logger       zerolog.Logger
	enabled      bool
	store        *db.Store
	mentionNames []string
	run          windowsToastRunner
	appID        string
	history      idHistory
}

// windowsToastRunner executes a toast. Tests replace this.
type windowsToastRunner func(appID, title, body, script string) error

func NewWindowsNotifier(logger zerolog.Logger, enabled bool, store *db.Store, identityName string) *WindowsNotifier {
	appID := strings.TrimSpace(osGetenv("OPENMESSAGES_WINDOWS_TOAST_APP_ID"))
	if appID == "" {
		appID = defaultWindowsAppID
	}
	return &WindowsNotifier{
		logger:       logger,
		enabled:      enabled,
		store:        store,
		mentionNames: mentionNamesForIdentity(identityName),
		run:          runPowerShellFile,
		appID:        appID,
		history:      newIDHistory(),
	}
}

func (n *WindowsNotifier) Enabled() bool {
	return n != nil && n.enabled
}

func (n *WindowsNotifier) NotifyIncomingMessage(message *db.Message) {
	if n == nil || !n.enabled || message == nil || message.IsFromMe {
		return
	}
	if !notificationAllowed(n.store, n.mentionNames, message) {
		return
	}
	messageID := strings.TrimSpace(message.MessageID)
	if messageID == "" || !n.history.remember(messageID) {
		return
	}

	title := notificationTitle(message)
	body := notificationBody(message)

	go func() {
		if err := n.notify(title, body); err != nil {
			n.logger.Warn().Err(err).Str("msg_id", messageID).Msg("Windows notification failed")
		}
	}()
}

func (n *WindowsNotifier) notify(title, body string) error {
	return n.run(n.appID, title, body, windowsToastPowerShellScript())
}

type windowsToastPayload struct {
	AppID string `json:"appId"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// runPowerShellFile writes the toast script to a temp .ps1 and executes it with
// -File -STA. Passing a multiline script via -Command is fragile on Windows
// (quoting / argv), which produced silent no-ops for OS toasts.
func runPowerShellFile(appID, title, body, script string) error {
	payload, err := json.Marshal(windowsToastPayload{
		AppID: appID,
		Title: title,
		Body:  body,
	})
	if err != nil {
		return fmt.Errorf("encode toast payload: %w", err)
	}
	dir := filepath.Join(os.TempDir(), "OpenMessage", "toast")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("toast temp dir: %w", err)
	}
	stamp := time.Now().UnixNano()
	scriptPath := filepath.Join(dir, fmt.Sprintf("toast-%d.ps1", stamp))
	payloadPath := filepath.Join(dir, fmt.Sprintf("toast-%d.json", stamp))
	if err := os.WriteFile(payloadPath, payload, 0o600); err != nil {
		return fmt.Errorf("write toast payload: %w", err)
	}
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		_ = os.Remove(payloadPath)
		return fmt.Errorf("write toast script: %w", err)
	}
	defer func() {
		_ = os.Remove(scriptPath)
		_ = os.Remove(payloadPath)
	}()

	cmd := exec.Command("powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-STA",
		"-ExecutionPolicy", "Bypass",
		"-File", scriptPath,
		"-PayloadPath", payloadPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func windowsToastPowerShellScript() string {
	return `
param(
  [Parameter(Mandatory = $true)][string]$PayloadPath
)
$ErrorActionPreference = 'Stop'
$raw = [System.IO.File]::ReadAllText($PayloadPath)
$p = $raw | ConvertFrom-Json
function Esc([string]$s) {
  if ($null -eq $s) { return '' }
  return (($s -replace '&','&amp;') -replace '<','&lt;' -replace '>','&gt;' -replace '"','&quot;')
}
$title = Esc ([string]$p.title)
$body = Esc ([string]$p.body)
$appId = [string]$p.appId
if ([string]::IsNullOrWhiteSpace($appId)) {
  $appId = '{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe'
}

function Show-Balloon {
  param([string]$TipTitle, [string]$TipBody)
  Add-Type -AssemblyName System.Windows.Forms
  Add-Type -AssemblyName System.Drawing
  $notify = New-Object System.Windows.Forms.NotifyIcon
  $notify.Icon = [System.Drawing.SystemIcons]::Information
  $notify.Visible = $true
  $notify.BalloonTipTitle = $TipTitle
  $notify.BalloonTipText = $TipBody
  $notify.ShowBalloonTip(5000)
  Start-Sleep -Seconds 6
  $notify.Dispose()
}

try {
  $xml = '<toast><visual><binding template="ToastGeneric"><text>' + $title + '</text><text>' + $body + '</text></binding></visual></toast>'
  [Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
  [Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom, ContentType = WindowsRuntime] | Out-Null
  $doc = New-Object Windows.Data.Xml.Dom.XmlDocument
  $doc.LoadXml($xml)
  $toast = [Windows.UI.Notifications.ToastNotification]::new($doc)
  [Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($appId).Show($toast)
} catch {
  Show-Balloon -TipTitle ([string]$p.title) -TipBody ([string]$p.body)
}
`
}

// osGetenv is overridable in tests.
var osGetenv = os.Getenv
