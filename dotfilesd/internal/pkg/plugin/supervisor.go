package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// ---------------------------------------------------------------------------
// Plugin supervision — crash monitoring and auto-restart
// ---------------------------------------------------------------------------

const (
	// initialBackoff is the delay before the first restart attempt after a crash.
	initialBackoff = 1 * time.Second

	// maxBackoff caps the exponential backoff for repeated crashes.
	maxBackoff = 30 * time.Second

	// resetBackoffAfter is the minimum uptime required to reset the backoff
	// to its initial value. If a plugin stays alive this long, we assume
	// the crash-loop has been resolved.
	resetBackoffAfter = 60 * time.Second
)

// supervisePlugin monitors a plugin process and restarts it if it crashes.
// It runs in its own goroutine and returns when the parent context is
// cancelled or the plugin is successfully shut down.
//
// The sourceDir and cacheDir are needed to rebuild the plugin binary on
// restart. reRegister is a callback that atomically swaps the PluginInfo
// in the registry so that tool calls seamlessly switch to the new instance.
func supervisePlugin(
	ctx context.Context,
	name string,
	proc *Process,
	sourceDir, cacheDir, ctxURL, ctxToken string,
	reRegister func(name string, info *PluginInfo) *PluginInfo,
) {
	backoff := initialBackoff

	for {
		select {
		case <-ctx.Done():
			slog.Debug("supervisor: context cancelled, stopping supervision", "plugin", name)
			return
		case <-proc.Done():
			slog.Warn("plugin process exited, initiating restart", "plugin", name, "pid", proc.Cmd.Process.Pid)
		}

	restartLoop:
		for {
			slog.Info("restarting plugin", "name", name, "backoff", backoff)

			// Rebuild the plugin binary.
			result, err := (&Builder{CacheDir: cacheDir}).Build(name, sourceDir)
			if err != nil {
				slog.Error("plugin rebuild failed, will retry", "name", name, "error", err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
				}
				backoff = capBackoff(backoff)
				continue restartLoop
			}
			slog.Debug("plugin rebuild succeeded", "name", name, "from_cache", result.FromCache)

			// Relaunch the plugin.
			sessionID := fmt.Sprintf("plugin-%s", name)
			newProc, err := Launch(result.BinaryPath, ctxURL, ctxToken, sessionID, nil)
			if err != nil {
				slog.Error("plugin relaunch failed, will retry", "name", name, "error", err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
				}
				backoff = capBackoff(backoff)
				continue restartLoop
			}

			// Fetch descriptor from the new instance.
			client := NewClient(newProc.URL)
			desc, err := client.GetDescriptor(ctx)
			if err != nil {
				slog.Error("plugin re-describe failed, killing new instance", "name", name, "error", err)
				newProc.Kill()
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
				}
				backoff = capBackoff(backoff)
				continue restartLoop
			}

			// Atomically replace the registry entry.
			newInfo := &PluginInfo{
				Descriptor: desc,
				Client:     client,
				Process:    newProc,
				SourceDir:  sourceDir,
				CacheDir:   cacheDir,
			}
			old := reRegister(name, newInfo)

			// Kill the old process if it still exists (should already be dead,
			// but be safe).
			if old != nil && old.Process != nil && old.Process != proc {
				old.Process.Kill()
			}

			slog.Info("plugin restarted successfully", "name", name, "pid", newProc.Cmd.Process.Pid, "tools", len(desc.Tools))
			backoff = initialBackoff
			proc = newProc
			break restartLoop
		}
	}
}

// capBackoff doubles the backoff up to maxBackoff.
func capBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > maxBackoff {
		return maxBackoff
	}
	return next
}
