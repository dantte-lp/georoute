package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const iosxePTYHeight = 2000

var (
	errIOSXEApplyRejected   = errors.New("iosxe apply rejected")
	errIOSXEPostcheckFailed = errors.New("iosxe postcheck failed")
	errIOSXESaveRejected    = errors.New("iosxe save rejected")
)

func applyIOSXEOverSSH(ctx context.Context, f cliFlags, candidate string, logger *slog.Logger) error {
	script := buildIOSXEApplyScript(f, candidate)
	out, err := runIOSXESSHScript(ctx, f, script)
	if err != nil {
		return fmt.Errorf("iosxe ssh apply: %w", err)
	}
	err = verifyIOSXEApplyOutput(f, out)
	if err != nil {
		return err
	}
	logger.Info("iosxe apply postcheck passed",
		slog.String("host", f.iosxeSSHHost),
		slog.String("prefix_list", f.iosxePrefixList),
		slog.String("route_map", f.iosxeRouteMap),
		slog.Bool("save_config", f.iosxeSaveConfig))
	if !f.iosxeSaveConfig {
		return nil
	}

	out, err = runIOSXESSHScript(ctx, f, "terminal length 0\nwrite memory\nexit\n")
	if err != nil {
		return fmt.Errorf("iosxe ssh save: %w", err)
	}
	if strings.Contains(out, "% Invalid") || strings.Contains(out, "% Ambiguous") {
		return fmt.Errorf("%w: %s", errIOSXESaveRejected, trimCommandOutput(out))
	}

	return nil
}

func buildIOSXEApplyScript(f cliFlags, candidate string) string {
	commands := []string{
		"terminal length 0",
		"configure terminal",
	}
	commands = append(commands, strings.Split(strings.TrimSpace(candidate), "\n")...)
	commands = append(commands,
		"end",
		"show ip prefix-list "+f.iosxePrefixList,
		"show route-map "+f.iosxeRouteMap,
	)
	if f.iosxeStaticRouteMap != "" {
		commands = append(commands, "show route-map "+f.iosxeStaticRouteMap)
	}
	if f.iosxeBGPAS != "" {
		commands = append(commands, "show running-config | section ^router bgp")
	}
	commands = append(commands, "exit")

	return strings.Join(commands, "\n") + "\n"
}

func verifyIOSXEApplyOutput(f cliFlags, out string) error {
	rejections := []string{"% Invalid", "% Ambiguous", "% Incomplete", "Failed"}
	for _, marker := range rejections {
		if strings.Contains(out, marker) {
			return fmt.Errorf("%w: marker=%q output=%s", errIOSXEApplyRejected, marker, trimCommandOutput(out))
		}
	}
	required := []string{f.iosxePrefixList, f.iosxeRouteMap}
	if f.iosxeStaticRouteMap != "" {
		required = append(required, f.iosxeStaticRouteMap)
	}
	if f.iosxeBGPAS != "" {
		required = append(required, "redistribute static route-map")
	}
	for _, marker := range required {
		if !strings.Contains(out, marker) {
			return fmt.Errorf("%w: missing %q in output=%s", errIOSXEPostcheckFailed, marker, trimCommandOutput(out))
		}
	}

	return nil
}

func runIOSXESSHScript(ctx context.Context, f cliFlags, script string) (string, error) {
	timeout := f.iosxeSSHTimeout
	if timeout <= 0 {
		timeout = defaultIOSXESSHTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client, err := dialIOSXESSH(runCtx, f, timeout)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = client.Close()
	}()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("new ssh session: %w", err)
	}
	defer func() {
		_ = session.Close()
	}()

	var out bytes.Buffer
	session.Stdout = &out
	session.Stderr = &out
	err = session.RequestPty("vt100", iosxePTYHeight, 80, ssh.TerminalModes{ssh.ECHO: 0})
	if err != nil {
		return "", fmt.Errorf("request pty: %w", err)
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("stdin pipe: %w", err)
	}
	err = session.Shell()
	if err != nil {
		return "", fmt.Errorf("start shell: %w", err)
	}

	waitCh := make(chan error, 1)
	go func() {
		_, writeErr := io.WriteString(stdin, script)
		closeErr := stdin.Close()
		if writeErr != nil {
			waitCh <- writeErr

			return
		}
		if closeErr != nil {
			waitCh <- closeErr

			return
		}
		waitCh <- session.Wait()
	}()

	select {
	case <-runCtx.Done():
		_ = session.Close()

		return out.String(), fmt.Errorf("ssh script timeout: %w", runCtx.Err())
	case err = <-waitCh:
		if err != nil {
			return out.String(), fmt.Errorf("ssh script failed: %w output=%s", err, trimCommandOutput(out.String()))
		}

		return out.String(), nil
	}
}

func dialIOSXESSH(ctx context.Context, f cliFlags, timeout time.Duration) (*ssh.Client, error) {
	key, err := os.ReadFile(f.iosxeSSHKeyFile)
	if err != nil {
		return nil, fmt.Errorf("read ssh key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("parse ssh key: %w", err)
	}
	hostKeyCallback, err := knownhosts.New(f.iosxeKnownHostsFile)
	if err != nil {
		return nil, fmt.Errorf("load known_hosts: %w", err)
	}
	addr := net.JoinHostPort(f.iosxeSSHHost, strconv.Itoa(f.iosxeSSHPort))
	config := &ssh.ClientConfig{
		User:            f.iosxeSSHUser,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: hostKeyCallback,
		Timeout:         timeout,
	}
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial ssh: %w", err)
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		_ = conn.Close()

		return nil, fmt.Errorf("ssh handshake: %w", err)
	}

	return ssh.NewClient(sshConn, chans, reqs), nil
}

func trimCommandOutput(out string) string {
	out = strings.TrimSpace(out)
	if len(out) <= limitErrorBody {
		return out
	}

	return out[:limitErrorBody]
}
