package harness

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const serviceLabel = "com.lossless.serve"

func ServicePath(userHome string) string {
	if userHome == "" {
		userHome, _ = os.UserHomeDir()
	}
	if userHome == "" {
		userHome = os.Getenv("HOME")
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(userHome, "Library", "LaunchAgents", serviceLabel+".plist")
	case "linux":
		return filepath.Join(userHome, ".config", "systemd", "user", "lossless.service")
	default:
		return ""
	}
}

func serviceEnvPath(dataHome string) string {
	return filepath.Join(dataHome, "service.env")
}

func InstallUserService(exe, userHome, dataHome, url, token string) (string, error) {
	dest := ServicePath(userHome)
	if dest == "" {
		return "", fmt.Errorf("no user service on %s", runtime.GOOS)
	}
	if err := checkUnitString(exe, "executable"); err != nil {
		return "", err
	}
	if err := checkUnitString(dataHome, "home"); err != nil {
		return "", err
	}
	if err := writeServiceEnv(dataHome, url, token); err != nil {
		return "", err
	}
	body, err := serviceUnit(exe, dataHome, url, token)
	if err != nil {
		return "", err
	}
	if err := writeUserConfig(dest, body, 0o600); err != nil {
		return "", err
	}
	return dest, nil
}

func WriteServiceEnv(dataHome, url, token string) error {
	return writeServiceEnv(dataHome, url, token)
}

func writeServiceEnv(dataHome, url, token string) error {
	if err := checkUnitString(token, "token"); err != nil {
		return err
	}
	base := DaemonBase(url)
	if err := checkUnitString(base, "url"); err != nil {
		return err
	}
	if err := os.MkdirAll(dataHome, 0o700); err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "LOSSLESS_HOME=%s\n", strconv.Quote(dataHome))
	if base != defaultDaemon {
		fmt.Fprintf(&b, "LOSSLESS_URL=%s\n", strconv.Quote(base))
	}
	if token != "" {
		fmt.Fprintf(&b, "LOSSLESS_TOKEN=%s\n", strconv.Quote(token))
	}
	return writeUserConfig(serviceEnvPath(dataHome), []byte(b.String()), 0o600)
}

func StartUserService(dest string) error {
	if dest == "" {
		dest = ServicePath("")
	}
	if dest == "" {
		return fmt.Errorf("no user service on %s", runtime.GOOS)
	}
	switch runtime.GOOS {
	case "darwin":
		uid := strconv.Itoa(os.Getuid())
		target := "gui/" + uid + "/" + serviceLabel
		_ = exec.Command("launchctl", "bootout", target).Run()
		return exec.Command("launchctl", "bootstrap", "gui/"+uid, dest).Run()
	case "linux":
		_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
		return exec.Command("systemctl", "--user", "enable", "--now", "lossless.service").Run()
	default:
		return fmt.Errorf("no user service on %s", runtime.GOOS)
	}
}

func serviceUnit(exe, dataHome, url, token string) ([]byte, error) {
	switch runtime.GOOS {
	case "darwin":
		return launchdPlist(exe, dataHome, url, token), nil
	case "linux":
		return systemdUnit(exe, dataHome, url, token), nil
	default:
		return nil, fmt.Errorf("no user service on %s", runtime.GOOS)
	}
}

func launchdPlist(exe, dataHome, url, token string) []byte {
	log := filepath.Join(dataHome, "serve.log")
	var env strings.Builder
	add := func(k, v string) {
		if v == "" {
			return
		}
		fmt.Fprintf(&env, "    <key>%s</key>\n    <string>%s</string>\n", xmlEscape(k), xmlEscape(v))
	}
	add("LOSSLESS_HOME", dataHome)
	if base := DaemonBase(url); base != defaultDaemon {
		add("LOSSLESS_URL", base)
	}
	if token != "" {
		add("LOSSLESS_TOKEN", token)
	}
	envBlock := ""
	if env.Len() > 0 {
		envBlock = "  <key>EnvironmentVariables</key>\n  <dict>\n" + env.String() + "  </dict>\n"
	}
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>serve</string>
    <string>--watch</string>
    <string>--home</string>
    <string>%s</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
%s</dict>
</plist>
`, serviceLabel, xmlEscape(exe), xmlEscape(dataHome), xmlEscape(log), xmlEscape(log), envBlock))
}

func systemdUnit(exe, dataHome, url, token string) []byte {
	_ = url
	_ = token
	return []byte(fmt.Sprintf(`[Unit]
Description=lossless memory daemon
After=default.target

[Service]
EnvironmentFile=%s
ExecStart=%s serve --watch --home %s
Restart=on-failure

[Install]
WantedBy=default.target
`, systemdQuote(serviceEnvPath(dataHome)), systemdQuote(exe), systemdQuote(dataHome)))
}

func systemdQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		if r == '\\' || r == '"' || r == '$' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}
