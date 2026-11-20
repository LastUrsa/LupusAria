package bot

import (
	"context"
	"strings"

	"lupusaria/internal/announcements"
	"lupusaria/internal/raffle"
	"lupusaria/internal/recentstreamers"
)

// SettingsUpdate requests a refresh of saved settings on the chat loop.
type SettingsUpdate struct {
	Done chan error
}

// SetCommandSettings updates the settings read only by the chat message loop.
func (b *Bot) SetCommandSettings(cfg Config) {
	cfg = normalizeConfigPermissions(cfg)
	b.cfg.EnableMentions = cfg.EnableMentions
	b.cfg.EnableAsk = cfg.EnableAsk
	b.cfg.EnableLurk = cfg.EnableLurk
	b.cfg.EnableCommands = cfg.EnableCommands
	b.cfg.EnableReset = cfg.EnableReset
	b.cfg.MentionPermission = cfg.MentionPermission
	b.cfg.AskPermission = cfg.AskPermission
	b.cfg.LurkPermission = cfg.LurkPermission
	b.cfg.GamePermission = cfg.GamePermission
	b.cfg.CommandsPermission = cfg.CommandsPermission
	b.cfg.ResetPermission = cfg.ResetPermission
	b.cfg.WrapUpPermission = cfg.WrapUpPermission
}

func (b *Bot) SetRecentStreamers(ctx context.Context, service *recentstreamers.Service) {
	if b.stopRecent != nil {
		b.stopRecent()
	}
	b.recent = service
	b.startRecent(ctx)
}

func (b *Bot) startRecent(ctx context.Context) {
	b.recentCtx, b.stopRecent = context.WithCancel(ctx)
	if b.recent != nil {
		b.recent.StartChatterPolling(b.recentCtx)
	}
}

func (b *Bot) recentCommandContext(ctx context.Context) context.Context {
	if b.recentCtx != nil {
		return b.recentCtx
	}
	return ctx
}

func (b *Bot) SetAnnouncements(ctx context.Context, service *announcements.Service) {
	if b.stopAnnouncements != nil {
		b.stopAnnouncements()
	}
	b.annMu.Lock()
	b.ann = service
	b.annMu.Unlock()
	b.startAnnouncements(ctx)
}

func (b *Bot) startAnnouncements(ctx context.Context) {
	ctx, b.stopAnnouncements = context.WithCancel(ctx)
	if b.ann != nil {
		b.ann.Start(ctx)
	}
}

func (b *Bot) currentAnnouncements() *announcements.Service {
	if b == nil {
		return nil
	}
	b.annMu.RLock()
	defer b.annMu.RUnlock()
	return b.ann
}

func (b *Bot) SetRaffle(service *raffle.Service, epoch uint64) {
	b.cfg.Raffle = service
	b.raffleEpoch = epoch
}

func (b *Bot) SetSnapshotCrop(crop SnapshotCrop) {
	b.snapshotCrop.Store(&crop)
}

func (b *Bot) commandList() string {
	commands := []string{"!commands", "!game [analyze] [question]"}
	if b.cfg.EnableMentions {
		commands = append(commands, "@"+b.cfg.Name+" <message>")
	}
	if b.cfg.EnableAsk {
		commands = append(commands, "!ask <question>")
	}
	if b.cfg.EnableLurk {
		commands = append(commands, "!lurk [reason]")
	}
	if b.cfg.EnableReset {
		commands = append(commands, "!reset")
	}
	if b.cfg.WrapUp != nil {
		commands = append(commands, "!wrapup [preview|stats|reset]")
	}
	if b.cfg.Raffle != nil {
		commands = append(commands, "!rafadd", "!rafpick", "!rafclear")
	}
	if b.recent != nil {
		commands = append(commands, "!autoso [next|refresh|status]", "!soroulette")
	}
	return "Commands: " + strings.Join(commands, ", ") + "."
}
