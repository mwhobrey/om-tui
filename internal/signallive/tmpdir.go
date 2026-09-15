package signallive

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// signal-cli runs on the JVM; libsignal-client extracts ~21MB of native
// libraries into a fresh java.io.tmpdir subdirectory (libsignal*) on every
// invocation and does not reliably remove it on exit. Because the bridge
// invokes signal-cli repeatedly (receive polling, sends, metadata refresh),
// leaked directories can fill the disk (issue #27). Three defenses:
//
//  1. Every invocation runs with TMPDIR/java.io.tmpdir pointed at a private
//     per-run directory under signalTmpRoot(), removed as soon as the
//     process exits — no accumulation by construction.
//  2. signalTmpRoot() is swept at startup and periodically for entries old
//     enough that no live invocation can still own them (crash backstop).
//  3. A one-time sweep of the system temp dir removes libsignal* dirs
//     abandoned by earlier versions, so existing installs recover their
//     disk space without manual cleanup.

const (
	// signalRunTmpMaxAge must exceed the longest legitimate signal-cli run.
	// Receive polls finish in seconds and sends within sendTimeout, but a
	// `link` session can stay open while the user fetches their phone, so
	// keep a generous margin.
	signalRunTmpMaxAge = 30 * time.Minute

	signalTmpSweepInterval = 10 * time.Minute

	// legacyLibsignalMaxAge gates the one-time system temp dir sweep. A day
	// is far older than any live signal-cli process while young enough to
	// recover a runaway leak quickly.
	legacyLibsignalMaxAge = 24 * time.Hour

	signalTmpSweepEnvVar = "OPENMESSAGES_SIGNAL_TMP_SWEEP"

	// signal-cli 0.14.8 ships class-file 69 (Java 25). Older JDKs die with
	// UnsupportedClassVersionError; Java 8 also rejects --enable-native-access.
	minimumSignalCLIJavaMajor = 25
)

// signalTmpRoot lives under the system temp dir rather than the Signal
// config dir: Unpair removes the config dir wholesale (and tests remove
// their temp config dirs on cleanup), which would race with a lingering
// invocation still creating its run dir inside. The uid suffix keeps the
// root private per user on systems with a shared /tmp.
func signalTmpRoot() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("openmessage-signal-cli-%d", os.Getuid()))
}

// newSignalRunTmpDir creates a private temp dir for one signal-cli
// invocation. The returned cleanup removes it; callers must invoke cleanup
// only after the subprocess has exited.
func newSignalRunTmpDir() (string, func(), error) {
	root := signalTmpRoot()
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", nil, err
	}
	dir, err := os.MkdirTemp(root, "run-")
	if err != nil {
		return "", nil, err
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}

// signalCLIEnv rebases the subprocess's temp space onto dir. TMPDIR covers
// signal-cli itself plus anything it spawns; java.io.tmpdir (appended last
// to SIGNAL_CLI_OPTS so it wins) covers JVMs that derive their temp dir from
// the platform default instead of TMPDIR.
//
// JAVA_HOME is replaced when OPENMESSAGES_JAVA_HOME is set, or when a JDK
// ≥ 25 is found under well-known install roots (scoop temurin, Program
// Files\Java, Homebrew openjdk). Stale JAVA_HOME (this box: jdk1.8) makes
// signal-cli.bat ignore PATH java and die; PATH java 21 is still too old
// for signal-cli 0.14.8 (needs JRE 25).
func signalCLIEnv(base []string, dir string) []string {
	javaOpt := "-Djava.io.tmpdir=" + dir
	opts := javaOpt
	overrideJavaHome := strings.TrimSpace(os.Getenv("OPENMESSAGES_JAVA_HOME"))
	if overrideJavaHome == "" {
		overrideJavaHome = discoverSignalCLIJavaHomeFn()
	}
	env := make([]string, 0, len(base)+3)
	for _, kv := range base {
		switch {
		case strings.HasPrefix(kv, "TMPDIR="):
			continue
		case strings.HasPrefix(kv, "SIGNAL_CLI_OPTS="):
			if existing := strings.TrimSpace(strings.TrimPrefix(kv, "SIGNAL_CLI_OPTS=")); existing != "" {
				opts = existing + " " + javaOpt
			}
			continue
		case strings.HasPrefix(kv, "JAVA_HOME="):
			if overrideJavaHome != "" || javaHomeTooOldForSignalCLI(strings.TrimPrefix(kv, "JAVA_HOME=")) {
				continue
			}
		}
		env = append(env, kv)
	}
	env = append(env, "TMPDIR="+dir, "SIGNAL_CLI_OPTS="+opts)
	if overrideJavaHome != "" {
		env = append(env, "JAVA_HOME="+overrideJavaHome)
	}
	return env
}

var discoverSignalCLIJavaHomeFn = discoverSignalCLIJavaHome

func discoverSignalCLIJavaHome() string {
	return firstUsableSignalCLIJavaHome(signalCLIJavaHomeCandidates())
}

func signalCLIJavaHomeCandidates() []string {
	var out []string
	if runtime.GOOS == "windows" {
		out = append(out, javaHomesUnder(`C:\Program Files\Java`)...)
		out = append(out, javaHomesUnder(`C:\Program Files\Eclipse Adoptium`)...)
		if home, err := os.UserHomeDir(); err == nil {
			out = append(out, scoopJavaHomes(filepath.Join(home, "scoop", "apps"))...)
		}
		return out
	}
	for _, p := range []string{
		"/opt/homebrew/opt/openjdk@25",
		"/opt/homebrew/opt/openjdk",
		"/usr/local/opt/openjdk@25",
		"/usr/local/opt/openjdk",
	} {
		out = append(out, p)
	}
	return out
}

func javaHomesUnder(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			out = append(out, filepath.Join(root, entry.Name()))
		}
	}
	return out
}

func scoopJavaHomes(appsRoot string) []string {
	entries, err := os.ReadDir(appsRoot)
	if err != nil {
		return nil
	}
	var out []string
	for _, entry := range entries {
		if !entry.IsDir() || !looksLikeScoopJDKApp(entry.Name()) {
			continue
		}
		out = append(out, filepath.Join(appsRoot, entry.Name(), "current"))
	}
	return out
}

func looksLikeScoopJDKApp(name string) bool {
	n := strings.ToLower(name)
	for _, key := range []string{"temurin", "openjdk", "zulu", "graal", "liberica", "semeru", "microsoft-jdk"} {
		if strings.Contains(n, key) {
			return true
		}
	}
	return strings.Contains(n, "jdk") && !strings.Contains(n, "signal")
}

func firstUsableSignalCLIJavaHome(candidates []string) string {
	best := ""
	bestVer := 0
	for _, home := range candidates {
		if !javaHomeHasJava(home) {
			continue
		}
		ver := javaHomeMajorVersion(home)
		if ver >= minimumSignalCLIJavaMajor && ver >= bestVer {
			best = home
			bestVer = ver
		}
	}
	return best
}

func javaHomeHasJava(home string) bool {
	home = strings.TrimSpace(strings.Trim(home, `"`))
	if home == "" {
		return false
	}
	for _, name := range []string{"java", "java.exe"} {
		if _, err := os.Stat(filepath.Join(home, "bin", name)); err == nil {
			return true
		}
	}
	return false
}

var (
	javaMajorFromName = regexp.MustCompile(`(?i)(?:jdk-?|jre-?|temurin-?|openjdk-?|zulu-?|@)(\d{1,2})\b`)
	javaDottedVersion = regexp.MustCompile(`^(\d{1,2})(?:\.\d+)`)
)

func javaHomeMajorVersion(home string) int {
	home = strings.TrimSpace(strings.Trim(home, `"`))
	if home == "" {
		return 0
	}
	parts := strings.Split(filepath.ToSlash(filepath.Clean(home)), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if v := parseJavaMajorFromName(parts[i]); v > 0 {
			return v
		}
	}
	return 0
}

func parseJavaMajorFromName(name string) int {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" || n == "current" || n == "bin" || n == "latest" || n == "java" {
		return 0
	}
	if strings.Contains(n, "1.8") {
		return 8
	}
	if m := javaMajorFromName.FindStringSubmatch(n); len(m) == 2 {
		v, _ := strconv.Atoi(m[1])
		if v >= 8 {
			return v
		}
	}
	if m := javaDottedVersion.FindStringSubmatch(n); len(m) == 2 {
		v, _ := strconv.Atoi(m[1])
		if v >= 8 {
			return v
		}
	}
	return 0
}

func javaHomeTooOldForSignalCLI(home string) bool {
	if v := javaHomeMajorVersion(home); v > 0 {
		return v < minimumSignalCLIJavaMajor
	}
	home = strings.TrimSpace(strings.Trim(home, `"`))
	if home == "" {
		return false
	}
	lower := strings.ToLower(filepath.ToSlash(home))
	base := strings.ToLower(filepath.Base(filepath.Clean(home)))
	switch {
	case strings.Contains(base, "1.8"), strings.Contains(base, "jdk1.8"), strings.Contains(base, "jre1.8"):
		return true
	case strings.Contains(base, "jdk-8"), strings.Contains(base, "jdk8"):
		return true
	case strings.Contains(base, "jdk-11"), strings.Contains(base, "jdk11"), strings.Contains(lower, "/jdk-11"):
		return true
	default:
		return false
	}
}

func signalTmpSweepDisabled() bool {
	return strings.TrimSpace(os.Getenv(signalTmpSweepEnvVar)) == "0"
}

// sweepSignalTmpRoot removes entries under the app-owned tmp root older
// than maxAge. The bridge owns this directory outright, so every stale
// entry is a leftover from a crashed run regardless of its name.
func sweepSignalTmpRoot(logger zerolog.Logger, maxAge time.Duration) {
	if signalTmpSweepDisabled() {
		return
	}
	removed, bytes := sweepDirEntries(signalTmpRoot(), maxAge, func(string) bool { return true })
	if removed > 0 {
		logger.Info().
			Int("dirs", removed).
			Str("reclaimed", humanBytes(bytes)).
			Msg("Swept stale signal-cli temp dirs left by interrupted runs")
	}
}

// sweepLegacyLibsignalTemp clears libsignal* dirs that earlier OpenMessage
// versions leaked into the system temp dir (issue #27). Only dirs beyond
// legacyLibsignalMaxAge are touched: anything that old cannot belong to a
// live signal-cli run, and libsignal re-extracts on demand if another app
// somehow still references one.
func sweepLegacyLibsignalTemp(logger zerolog.Logger) {
	if signalTmpSweepDisabled() {
		return
	}
	removed, bytes := sweepDirEntries(os.TempDir(), legacyLibsignalMaxAge, func(name string) bool {
		return strings.HasPrefix(name, "libsignal")
	})
	if removed > 0 {
		logger.Warn().
			Int("dirs", removed).
			Str("reclaimed", humanBytes(bytes)).
			Msg("Removed libsignal temp dirs leaked by earlier versions (see issue #27)")
	}
}

func sweepDirEntries(root string, maxAge time.Duration, match func(name string) bool) (int, int64) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0, 0
	}
	cutoff := now().Add(-maxAge)
	removed := 0
	var reclaimed int64
	for _, entry := range entries {
		if !entry.IsDir() || !match(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		path := filepath.Join(root, entry.Name())
		size := dirSize(path)
		if err := os.RemoveAll(path); err != nil {
			continue
		}
		removed++
		reclaimed += size
	}
	return removed, reclaimed
}

func dirSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return strconv.FormatFloat(float64(n)/float64(div), 'f', 1, 64) + " " + string("KMGTPE"[exp]) + "iB"
}
