package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/sprout-foundry/sprout/pkg/gpulink"
)

func runGPUStart(cmd *cobra.Command, _ []string) error {
	st, slot, err := gpuTarget()
	if err != nil {
		return err
	}
	key, err := gpuAPIKey(st, slot)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), nil)).With("gpu", st.Name)
	agent := &gpulink.Agent{
		State:     st,
		APIKey:    key,
		EngineKey: gpulink.LoadEngineKeyFor(slot),
		Log:       log,
		ReloadKey: func() (string, error) { return gpulink.LoadAPIKeyFor(slot) },
	}
	if st.Fleet {
		// A fleet GPU keeps a tunnel to each of two platform replicas and
		// rotates its key monthly.
		agent.Tunnels = 2
		agent.RotateEvery = gpulink.FleetKeyRotation
		agent.RotatedAt = st.KeyRotatedAt
		agent.SaveKey = func(k string) error { _, err := gpulink.SaveAPIKeyFor(slot, k); return err }
		agent.OnRotated = func(at time.Time) {
			st.KeyRotatedAt = at
			if err := st.Save(); err != nil {
				log.Warn("recording the key rotation", "err", err)
			}
		}
	}
	if st.Engine == gpulink.EngineSinter {
		handler, model, unload, err := gpulink.SinterHandler(st.Model)
		if err != nil {
			return err
		}
		defer unload()
		agent.Local = handler
		log.Info("serving with sinter", "model", model)
	} else if _, err := gpulink.EngineModels(ctx, st.Engine, agent.EngineKey); err != nil {
		// The tunnel still opens: the engine may start later, and the
		// platform picks its models up within a minute.
		log.Warn("the engine is not answering; requests fail until it starts (or link again with `sprout gpu link --sinter`)", "engine", st.Engine, "err", err)
	}
	log.Info("starting", "platform", st.PlatformURL, "engine", st.Engine)
	return agent.Run(ctx)
}

func runGPUStatus(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()
	links, err := gpulink.LoadLinks()
	if err != nil {
		return err
	}
	if len(links) == 0 {
		_, _ = fmt.Fprintln(out, "No GPU is linked on this machine. Run `sprout gpu link --engine host:port`.")
		return nil
	}
	for i, st := range links {
		if i > 0 {
			_, _ = fmt.Fprintln(out)
		}
		printGPULinkStatus(cmd.Context(), out, st)
	}
	if gpulink.LegacyServiceInstalled() {
		_, _ = fmt.Fprintln(out, "\nThe single service an older sprout installed (sprout-gpu.service, or com.sprout.gpu) is still on disk; `sprout gpu install` (all links) replaces it with one service per link.")
	}
	return nil
}

func printGPULinkStatus(ctx context.Context, out io.Writer, st *gpulink.State) {
	slot := st.Slot()
	_, _ = fmt.Fprintf(out, "Linked as %q to %s\nAccount: %s\nEngine: %s\n", st.Name, st.PlatformURL, firstNonEmpty(st.Account, "(not recorded; link again to see it)"), st.Engine)
	if state, logs := gpulink.ServiceState(slot); state != "" {
		_, _ = fmt.Fprintf(out, "Service: %s\n", state)
		if logs != "" {
			_, _ = fmt.Fprintf(out, "Service log: %s\n", logs)
		}
	}
	if st.Engine == gpulink.EngineSinter {
		_, _ = fmt.Fprintf(out, "Model: %s (served in-process by `sprout gpu start --name %s`)\n", st.Model, slot)
		return
	}
	models, err := gpulink.EngineModels(ctx, st.Engine, gpulink.LoadEngineKeyFor(slot))
	if err != nil {
		_, _ = fmt.Fprintf(out, "The engine is not answering: %v\n", err)
		return
	}
	_, _ = fmt.Fprintf(out, "The engine serves: %s\n", strings.Join(models, ", "))
}

func runGPUInstall(cmd *cobra.Command, _ []string) error {
	links, err := gpulink.LoadLinks()
	if err != nil {
		return err
	}
	if len(links) == 0 {
		return errors.New("no GPU is linked on this machine; run `sprout gpu link` first")
	}
	out := cmd.OutOrStdout()
	// The services run without a login session, where the OS keyring is out
	// of reach: older keys move to the file store now, while it opens.
	if err := gpulink.MoveKeysToFileStore(); err != nil {
		return fmt.Errorf("a GPU link key could not be moved where the services can read it (%w); run `sprout gpu link` again", err)
	}
	if gpuName != "" {
		st, _, err := gpuTarget()
		if err != nil {
			return err
		}
		path, err := gpulink.InstallService(st.Name)
		if err != nil {
			return fmt.Errorf("installing the service for %q: %w", st.Name, err)
		}
		_, _ = fmt.Fprintf(out, "Installed %s for %q.\n", path, st.Name)
		// The single service an older sprout installed is replaced only
		// once every link has its own instance; with one link that is now.
		if len(links) == 1 {
			if err := gpulink.ReplaceLegacyService(); err != nil {
				return err
			}
		} else {
			_, _ = fmt.Fprintln(out, "Other links are still served by the single service an older sprout installed; `sprout gpu install` (all links) replaces it.")
		}
		return nil
	}
	paths, err := gpulink.InstallAllServices(linkNames(links))
	if err != nil {
		return err
	}
	for i, path := range paths {
		_, _ = fmt.Fprintf(out, "Installed %s for %q.\n", path, links[i].Name)
	}
	if note := gpulink.ServiceNotice(); note != "" {
		_, _ = fmt.Fprintln(out, note)
	}
	return nil
}

// linkNames lists the links' names in the same order LoadLinks returned
// them, so installed paths pair with their links in the output.
func linkNames(links []*gpulink.State) []string {
	names := make([]string, len(links))
	for i, st := range links {
		names[i] = st.Name
	}
	return names
}

func runGPUUninstall(cmd *cobra.Command, _ []string) error {
	if gpuName != "" {
		st, slot, err := gpuTarget()
		if err != nil {
			return err
		}
		if err := gpulink.UninstallService(slot); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "The GPU link %q no longer runs at login.\n", st.Name)
		return nil
	}
	links, err := gpulink.LoadLinks()
	if err != nil {
		return err
	}
	if err := gpulink.UninstallService(""); err != nil {
		return err
	}
	if len(links) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "The GPU link no longer runs at login.")
		return nil
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "The GPU links (%d) no longer run at login.\n", len(links))
	return nil
}

func runGPUUnlink(cmd *cobra.Command, _ []string) error {
	st, slot, err := gpuTarget()
	if err != nil {
		return err
	}
	_ = gpulink.UninstallService(slot)
	if err := gpulink.RemoveLink(slot); err != nil {
		return err
	}
	name := st.Name
	if name == "" {
		name = "this GPU"
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Forgot the link %q on this machine. Remove %q under Account → My GPUs on the platform too.\n", name, name)
	return nil
}

func runGPURotateKey(cmd *cobra.Command, _ []string) error {
	st, slot, err := gpuTarget()
	if err != nil {
		return err
	}
	key, err := gpuAPIKey(st, slot)
	if err != nil {
		return err
	}
	next, err := gpulink.RotateKey(cmd.Context(), st.PlatformURL, st.LinkID, key)
	if err != nil {
		return err
	}
	keyStore, err := gpulink.SaveAPIKeyFor(slot, next)
	if err != nil {
		// The platform keeps the current key working until the new one is
		// used, so nothing is lost.
		return fmt.Errorf("storing the new key failed (%w); the current key keeps working", err)
	}
	st.KeyRotatedAt = time.Now().UTC()
	if err := st.Save(); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Rotated the key for %q (stored in the %s). A running agent keeps the old key until it restarts; the old key stops working once the new one connects.\n", st.Name, keyStore)
	return nil
}
