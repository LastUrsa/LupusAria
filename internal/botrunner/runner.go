package botrunner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"lupusaria/internal/adalerts"
	"lupusaria/internal/ai"
	"lupusaria/internal/announcements"
	"lupusaria/internal/bot"
	"lupusaria/internal/config"
	"lupusaria/internal/knowledge"
	"lupusaria/internal/raffle"
	"lupusaria/internal/recentstreamers"
	"lupusaria/internal/twitch"
	"lupusaria/internal/wrapup"
)

type Options struct {
	SettingsUpdates <-chan bot.SettingsUpdate

	RaffleRepository  *raffle.Repository
	RaffleChanged     func(raffle.Snapshot)
	RaffleReady       func(*raffle.Service, *raffle.Repository)
	RaffleUnavailable func(string)

	MediaActionRedeem func(context.Context, twitch.ChannelPointRedeemEvent)
	WrapUp            *wrapup.Service
}

func Run(ctx context.Context, envPath string, logger *slog.Logger) error {
	return RunWithOptions(ctx, envPath, logger, Options{})
}

func RunWithOptions(ctx context.Context, envPath string, logger *slog.Logger, options Options) error {
	if logger == nil {
		logger = slog.Default()
	}

	cfg, err := config.Load(envPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	aiClient, err := ai.NewClient(cfg.AI)
	if err != nil {
		return fmt.Errorf("initialize ai client: %w", err)
	}

	if err := knowledge.EnsureFile(cfg.Bot.KnowledgePath); err != nil {
		logger.Warn("failed to create default knowledge file; continuing without it", "path", cfg.Bot.KnowledgePath, "error", err)
	}
	knowledgeBase, err := knowledge.Load(cfg.Bot.KnowledgePath)
	if err != nil {
		logger.Warn("failed to load knowledge base; continuing without it", "path", cfg.Bot.KnowledgePath, "error", err)
	} else {
		logger.Info("loaded knowledge base", "path", cfg.Bot.KnowledgePath, "sections", len(knowledgeBase.Sections))
	}

	if cfg.Twitch.RefreshToken != "" {
		tokenSet, err := twitch.NewAuthManager(twitch.AuthConfig{
			ClientID:                     cfg.Twitch.ClientID,
			ClientSecret:                 cfg.Twitch.ClientSecret,
			RefreshToken:                 cfg.Twitch.RefreshToken,
			StatePath:                    cfg.Twitch.TokenStatePath,
			PreferConfiguredRefreshToken: preferConfiguredRefreshToken(envPath, cfg.Twitch.TokenStatePath),
		}).Refresh(ctx)
		if err != nil {
			logger.Warn("failed to refresh twitch token; using configured access token", "error", err)
		} else {
			cfg.Twitch.OAuthToken = "oauth:" + tokenSet.AccessToken
			logger.Info("refreshed twitch access token", "expires_at", tokenSet.ExpiresAt.Format(time.RFC3339))
		}
	}

	if (cfg.AdAlerts.Enabled || options.SettingsUpdates != nil || options.MediaActionRedeem != nil || options.WrapUp != nil) && cfg.Twitch.AdsRefreshToken != "" {
		tokenSet, err := twitch.NewAuthManager(twitch.AuthConfig{
			ClientID:                     cfg.Twitch.AdsClientID,
			ClientSecret:                 cfg.Twitch.AdsClientSecret,
			RefreshToken:                 cfg.Twitch.AdsRefreshToken,
			StatePath:                    cfg.Twitch.AdsTokenStatePath,
			PreferConfiguredRefreshToken: preferConfiguredRefreshToken(envPath, cfg.Twitch.AdsTokenStatePath),
		}).Refresh(ctx)
		if err != nil {
			logger.Warn("failed to refresh twitch ads token; using configured ads access token if available", "error", err)
		} else {
			cfg.Twitch.AdsOAuthToken = "oauth:" + tokenSet.AccessToken
			cfg.Twitch.AdsRefreshToken = tokenSet.RefreshToken
			logger.Info("refreshed twitch broadcaster access token", "expires_at", tokenSet.ExpiresAt.Format(time.RFC3339))
		}
	}
	if cfg.AdAlerts.Enabled && cfg.Twitch.AdsOAuthToken == "" {
		cfg.Twitch.AdsOAuthToken = cfg.Twitch.OAuthToken
	}

	var streamProvider bot.StreamInfoProvider
	var helix *twitch.HelixClient
	var recentService *recentstreamers.Service
	var announcementService *announcements.Service
	var adService *adalerts.Service
	var broadcasterID string
	var moderatorID string
	var chatSendToken string
	var channelEmotes []twitch.Emote
	var adBreaks chan twitch.AdBreakEvent
	var redeems chan twitch.ChannelPointRedeemEvent
	var wrapEvents chan wrapup.Event
	if cfg.Twitch.ClientID != "" {
		helix = twitch.NewHelixClient(cfg.Twitch.ClientID, cfg.Twitch.OAuthToken)
		streamProvider = helix
		broadcasterID, moderatorID = resolveRecentStreamerIDs(ctx, helix, cfg.Twitch.Channel, cfg.Twitch.BotUsername, logger)
		chatSendToken = appAccessToken(ctx, cfg, logger)
		if cfg.Bot.EnableEmoteContext && broadcasterID != "" {
			emotes, err := helix.GetChannelEmotes(ctx, broadcasterID)
			if err != nil {
				logger.Warn("failed to load channel emote catalog; continuing with message emote metadata only", "error", err)
			} else {
				channelEmotes = convertChannelEmotes(emotes)
				logger.Info("loaded channel emote catalog", "count", len(channelEmotes))
			}
		}
	}

	if (cfg.AdAlerts.Enabled || options.SettingsUpdates != nil) && cfg.Twitch.AdsClientID != "" && (cfg.Twitch.AdsOAuthToken != "" || cfg.Twitch.OAuthToken != "") {
		adBreaks = make(chan twitch.AdBreakEvent, 16)
	}
	if options.MediaActionRedeem != nil {
		redeems = make(chan twitch.ChannelPointRedeemEvent, 32)
	}
	if options.WrapUp != nil {
		wrapEvents = make(chan wrapup.Event, 64)
		if helix != nil && !(cfg.Bot.RaffleEnabled && broadcasterID != "") {
			reconcileWrapUp(ctx, helix, options.WrapUp, cfg.Twitch.Channel, broadcasterID, logger)
		} else if helix == nil {
			logger.Warn("wrap-up stream tracking unavailable: missing Twitch client ID")
		}
	}

	type raffleActivation struct {
		service *raffle.Service
		epoch   uint64
	}
	var activeRaffle atomic.Pointer[raffleActivation]
	var raffleEpoch uint64
	var streamSignals chan twitch.StreamSignal
	var stopRaffle context.CancelFunc
	var raffleDone chan struct{}
	repo := options.RaffleRepository
	ownsRepo := false
	disableRaffle := func() {
		activeRaffle.Store(nil)
		if stopRaffle != nil {
			stopRaffle()
			<-raffleDone
			stopRaffle = nil
		}
	}
	defer func() {
		disableRaffle()
		if ownsRepo {
			repo.Close()
		}
	}()
	enableRaffle := func(ctx context.Context) (*raffle.Service, error) {
		if helix == nil || broadcasterID == "" {
			return nil, errors.New("raffle requires Twitch broadcaster identity")
		}
		if repo == nil {
			var err error
			repo, err = raffle.Open(cfg.Bot.RafflePath)
			if err != nil {
				return nil, errors.New("raffle storage unavailable")
			}
			ownsRepo = options.RaffleReady == nil
		}
		service, err := raffle.New(repo, broadcasterID, raffle.Options{Confirmation: cfg.Bot.RaffleConfirmation, Changed: options.RaffleChanged})
		if err != nil {
			return nil, errors.New("raffle storage unavailable")
		}
		var streamAuth adTokenRefresher
		if cfg.Twitch.RefreshToken != "" {
			streamAuth = twitch.NewAuthManager(twitch.AuthConfig{ClientID: cfg.Twitch.ClientID, ClientSecret: cfg.Twitch.ClientSecret, RefreshToken: cfg.Twitch.RefreshToken, StatePath: cfg.Twitch.TokenStatePath})
		}
		provider := verifiedStreamProvider{provider: helix, auth: streamAuth, observed: func(info twitch.StreamInfo) {
			if options.WrapUp != nil {
				applyWrapUpStatus(options.WrapUp, info, broadcasterID, logger)
			}
		}}
		if options.RaffleReady != nil {
			options.RaffleReady(service, repo)
		}
		if err := service.Reconcile(ctx, provider, cfg.Twitch.Channel, true); err != nil {
			logger.Warn("raffle verification unavailable; preserving saved state")
		}
		raffleEpoch++
		activeRaffle.Store(&raffleActivation{service: service, epoch: raffleEpoch})
		workerCtx, cancel := context.WithCancel(ctx)
		stopRaffle = cancel
		done := make(chan struct{})
		raffleDone = done
		go func() {
			defer close(done)
			service.Run(workerCtx, provider, cfg.Twitch.Channel, streamSignals, cfg.Bot.RafflePollInterval, func(err error) {
				service.ReportError(err)
				logger.Warn("raffle reconciliation failed; will retry")
			})
		}()
		return service, nil
	}
	if (cfg.Bot.RaffleEnabled || options.SettingsUpdates != nil) && helix != nil && broadcasterID != "" {
		streamSignals = make(chan twitch.StreamSignal, 16)
	}
	var raffleService *raffle.Service
	if cfg.Bot.RaffleEnabled {
		var raffleErr error
		raffleService, raffleErr = enableRaffle(ctx)
		if raffleErr != nil {
			logger.Warn("raffle unavailable", "error", raffleErr)
			if options.RaffleUnavailable != nil {
				options.RaffleUnavailable(raffleErr.Error())
			}
			if options.WrapUp != nil && helix != nil && broadcasterID != "" {
				reconcileWrapUp(ctx, helix, options.WrapUp, cfg.Twitch.Channel, broadcasterID, logger)
			}
		}
	}
	adsHelix := twitch.NewHelixClient(cfg.Twitch.AdsClientID, firstAdToken(cfg))
	rawChat := newTwitchChat(cfg, broadcasterID, moderatorID, chatSendToken, adBreaks, redeems, wrapEvents, logger)
	eventSub, _ := rawChat.(*twitch.EventSubChatClient)
	if eventSub != nil {
		if raffleService != nil || options.SettingsUpdates != nil {
			eventSub.SetStreamSignals(streamSignals)
		}
		if options.SettingsUpdates != nil {
			eventSub.ConfigureLiveAdSettings(cfg.AdAlerts.Enabled, adsHelix)
		}
	}
	if streamSignals != nil {
		if transport, ok := rawChat.(interface{ SetMessageObserver(func(*twitch.Message)) }); ok {
			transport.SetMessageObserver(func(msg *twitch.Message) {
				msg.RaffleFenced = true
				if activation := activeRaffle.Load(); activation != nil {
					msg.RaffleEpoch = activation.epoch
					fence, _ := activation.service.Capture()
					msg.RaffleSession = fence.SessionID
					msg.RaffleGeneration = fence.Generation
				}
			})
		}
	}
	chat := bot.WithChatLogging(rawChat, cfg.Bot.ChatLogPath, logger, cfg.Bot.Name)

	newRecent := func(settings config.RecentStreamersConfig) *recentstreamers.Service {
		if !settings.Enabled || helix == nil {
			return nil
		}
		return recentstreamers.New(recentstreamers.Config{
			Channel:              cfg.Twitch.Channel,
			Permission:           settings.Permission,
			SORoulettePermission: settings.SORoulettePermission,
			RouletteStreamers:    settings.RouletteStreamers,
			BroadcasterID:        broadcasterID,
			ModeratorID:          moderatorID,
			MinWatch:             settings.MinWatch,
			RecentWindow:         settings.RecentWindow,
			PageSize:             settings.PageSize,
			ShoutoutDelay:        settings.ShoutoutDelay,
			CacheTTL:             settings.CacheTTL,
			ChatterPollInterval:  settings.ChatterPollInterval,
		}, chat, helix, logger)
	}
	newAnnouncements := func(settings config.AnnouncementsConfig, items []announcements.Announcement) *announcements.Service {
		if !settings.Enabled {
			return nil
		}
		for _, item := range items {
			if item.Command == "!rafadd" || item.Command == "!rafpick" || item.Command == "!rafclear" {
				logger.Warn("announcement uses reserved raffle command; raffle takes precedence", "command", item.Command)
			}
		}
		return announcements.New(announcements.Config{
			Enabled:      settings.Enabled,
			Channel:      cfg.Twitch.Channel,
			PollInterval: settings.PollInterval,
			Items:        items,
		}, chat, streamProvider, logger)
	}
	recentService = newRecent(cfg.RecentStreamers)
	announcementItems, announcementErr := announcements.Load(cfg.Announcements.Path)
	if announcementErr != nil {
		logger.Warn("failed to load announcements; continuing without them", "path", cfg.Announcements.Path, "error", announcementErr)
	} else {
		announcementService = newAnnouncements(cfg.Announcements, announcementItems)
	}

	botSettings := bot.Config{
		BroadcasterID:         broadcasterID,
		Raffle:                raffleService,
		Name:                  cfg.Bot.Name,
		Channel:               cfg.Twitch.Channel,
		StreamerName:          cfg.Bot.StreamerName,
		StreamerPronouns:      cfg.Bot.StreamerPronouns,
		Personality:           cfg.Bot.Personality,
		WrapUp:                options.WrapUp,
		MaxContextMessages:    cfg.Bot.MaxContextMessages,
		StreamContextTTL:      cfg.Bot.StreamContextTTL,
		GlobalCooldown:        cfg.Bot.GlobalCooldown,
		UserCooldown:          cfg.Bot.UserCooldown,
		DailyBudgetUSD:        cfg.Bot.DailyBudgetUSD,
		MonthlyBudgetUSD:      cfg.Bot.MonthlyBudgetUSD,
		MaxRequestsPerHour:    cfg.Bot.MaxRequestsPerHour,
		InputPricePerMillion:  cfg.AI.InputPricePerMillion,
		OutputPricePerMillion: cfg.AI.OutputPricePerMillion,
		BudgetStatePath:       cfg.Bot.BudgetStatePath,
		ChatLogPath:           cfg.Bot.ChatLogPath,
		EmoteCachePath:        cfg.Bot.EmoteCachePath,
		EnableEmoteContext:    cfg.Bot.EnableEmoteContext,
		ChannelEmotes:         channelEmotes,
		SnapshotCrop: bot.SnapshotCrop{
			Enabled: cfg.Bot.SnapshotCrop.Enabled,
			X:       cfg.Bot.SnapshotCrop.X,
			Y:       cfg.Bot.SnapshotCrop.Y,
			Width:   cfg.Bot.SnapshotCrop.Width,
			Height:  cfg.Bot.SnapshotCrop.Height,
		},
		Knowledge: knowledgeBase,
	}
	setCommands(&botSettings, cfg.Bot)
	runner := bot.New(botSettings, chat, aiClient, streamProvider, recentService, announcementService, logger)
	runner.SetRaffle(raffleService, raffleEpoch)

	var adsAuth adTokenRefresher
	if cfg.Twitch.AdsRefreshToken != "" {
		adsAuth = twitch.NewAuthManager(twitch.AuthConfig{
			ClientID:                     cfg.Twitch.AdsClientID,
			ClientSecret:                 cfg.Twitch.AdsClientSecret,
			RefreshToken:                 cfg.Twitch.AdsRefreshToken,
			StatePath:                    cfg.Twitch.AdsTokenStatePath,
			PreferConfiguredRefreshToken: preferConfiguredRefreshToken(envPath, cfg.Twitch.AdsTokenStatePath),
		})
	}
	adProvider := &twitchAdScheduleProvider{helix: adsHelix, auth: adsAuth, tokenExpiresAt: adsTokenExpiresAt(cfg.Twitch.AdsTokenStatePath), logger: logger}
	newAdAlerts := func(settings config.AdAlertsConfig) *adalerts.Service {
		if !settings.Enabled || cfg.Twitch.AdsClientID == "" || firstAdToken(cfg) == "" || broadcasterID == "" {
			return nil
		}
		return adalerts.New(adalerts.Config{
			Channel:        cfg.Twitch.Channel,
			BroadcasterID:  broadcasterID,
			Enabled:        settings.Enabled,
			WarningLead:    settings.WarningLead,
			PollInterval:   settings.PollInterval,
			WarningMessage: settings.WarningMessage,
			StartMessage:   settings.StartMessage,
			EndMessage:     settings.EndMessage,
			Composer:       runner,
		}, chat, adProvider, logger)
	}
	adSettings := cfg.AdAlerts
	var stopAds context.CancelFunc
	var adsDone chan struct{}
	stopAdAlerts := func() {
		if stopAds != nil {
			stopAds()
			<-adsDone
			stopAds = nil
		}
	}
	defer stopAdAlerts()
	startAdAlerts := func(ctx context.Context, service *adalerts.Service) {
		adService = service
		if service == nil {
			return
		}
		workerCtx, cancel := context.WithCancel(ctx)
		stopAds = cancel
		done := make(chan struct{})
		adsDone = done
		pollDone := service.Start(workerCtx)
		go func() {
			defer close(done)
			defer func() { <-pollDone }()
			for {
				select {
				case <-workerCtx.Done():
					return
				case event := <-adBreaks:
					service.HandleAdBreakBegin(workerCtx, adalerts.AdBreakBegin{
						StartedAt: event.StartedAt,
						Duration:  event.Duration,
						Automatic: event.Automatic,
					})
				}
			}
		}()
	}
	startFeatures := func(ctx context.Context) {
		startAdAlerts(ctx, newAdAlerts(adSettings))
		if adSettings.Enabled && adService == nil {
			logger.Warn("ad alerts unavailable; check Twitch credentials and broadcaster identity")
		}
	}
	if redeems != nil {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case event := <-redeems:
					logger.Info("dispatching media action redeem", "reward", event.RewardTitle, "user", event.UserLogin)
					options.MediaActionRedeem(ctx, event)
				}
			}
		}()
	}
	if wrapEvents != nil {
		go func() {
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if helix != nil && activeRaffle.Load() == nil {
						reconcileWrapUp(ctx, helix, options.WrapUp, cfg.Twitch.Channel, broadcasterID, logger)
					}
				case event := <-wrapEvents:
					var err error
					switch event.Type {
					case "online":
						err = options.WrapUp.Online(fmt.Sprint(event.Metadata["streamId"]), event.UserID, event.At)
					case "offline":
						err = options.WrapUp.Offline(event.At)
					default:
						err = options.WrapUp.Record(event)
					}
					if err != nil {
						logger.Warn("failed to persist wrap-up event", "type", event.Type, "error", err)
					}
				}
			}
		}()
	}

	recentSettings := cfg.RecentStreamers
	announcementSettings := cfg.Announcements
	err = runner.RunWithSettings(ctx, options.SettingsUpdates, startFeatures, func(ctx context.Context) error {
		saved, err := config.LoadPartial(envPath)
		if err != nil {
			return err
		}
		var commands bot.Config
		setCommands(&commands, saved.Bot)
		runner.SetCommandSettings(commands)
		crop := saved.Bot.SnapshotCrop
		runner.SetSnapshotCrop(bot.SnapshotCrop{Enabled: crop.Enabled, X: crop.X, Y: crop.Y, Width: crop.Width, Height: crop.Height})

		var issues []error
		switch {
		case !saved.AdAlerts.Enabled:
			if adService != nil {
				stopAdAlerts()
				adService = nil
			}
			if eventSub != nil && adSettings.Enabled {
				if err := eventSub.SetAdBreaksEnabled(ctx, false); err != nil {
					issues = append(issues, fmt.Errorf("ad alerts stopped locally, but Twitch subscription removal failed: %w", err))
				} else {
					adSettings = saved.AdAlerts
				}
			}
		case saved.AdAlerts != adSettings || adService == nil:
			service := newAdAlerts(saved.AdAlerts)
			switch {
			case service == nil:
				issues = append(issues, errors.New("ad alerts require Twitch ads credentials and broadcaster identity from startup"))
			case eventSub == nil:
				issues = append(issues, errors.New("live ad alerts require a Twitch EventSub connection"))
			default:
				if err := adProvider.refreshIfNeeded(ctx, false); err != nil {
					issues = append(issues, err)
				} else if err := eventSub.SetAdBreaksEnabled(ctx, true); err != nil {
					issues = append(issues, fmt.Errorf("enable Twitch ad events: %w", err))
				} else {
					stopAdAlerts()
					// Discard events retained by the previous activation.
					for range len(adBreaks) {
						<-adBreaks
					}
					startAdAlerts(ctx, service)
					adSettings = saved.AdAlerts
				}
			}
		}
		if saved.Bot.RaffleEnabled && activeRaffle.Load() == nil {
			service, err := enableRaffle(ctx)
			if err != nil {
				issues = append(issues, err)
				if options.RaffleUnavailable != nil {
					options.RaffleUnavailable(err.Error())
				}
			} else {
				runner.SetRaffle(service, raffleEpoch)
			}
		} else if !saved.Bot.RaffleEnabled && activeRaffle.Load() != nil {
			disableRaffle()
			runner.SetRaffle(nil, 0)
			if options.RaffleUnavailable != nil {
				options.RaffleUnavailable("Raffle disabled")
			}
		}
		if saved.RecentStreamers.Enabled && helix == nil && !recentSettings.Enabled {
			issues = append(issues, errors.New("AutoSO requires a connected Twitch Helix client"))
		} else if !reflect.DeepEqual(recentSettings, saved.RecentStreamers) {
			runner.SetRecentStreamers(ctx, newRecent(saved.RecentStreamers))
			recentSettings = saved.RecentStreamers
		}

		var items []announcements.Announcement
		if saved.Announcements.Enabled {
			items, err = announcements.Load(announcementSettings.Path)
			if err != nil {
				issues = append(issues, fmt.Errorf("load live announcements: %w", err))
				return errors.Join(issues...)
			}
		}
		saved.Announcements.Path = announcementSettings.Path
		if saved.Announcements != announcementSettings || !reflect.DeepEqual(items, announcementItems) || announcementErr != nil {
			runner.SetAnnouncements(ctx, newAnnouncements(saved.Announcements, items))
			announcementSettings, announcementItems, announcementErr = saved.Announcements, items, nil
		}
		return errors.Join(issues...)
	})
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func setCommands(target *bot.Config, settings config.BotConfig) {
	target.EnableMentions = settings.EnableMentions
	target.EnableAsk = settings.EnableAsk
	target.EnableLurk = settings.EnableLurk
	target.EnableCommands = settings.EnableCommands
	target.EnableReset = settings.EnableReset
	target.MentionPermission = settings.MentionPermission
	target.AskPermission = settings.AskPermission
	target.LurkPermission = settings.LurkPermission
	target.GamePermission = settings.GamePermission
	target.CommandsPermission = settings.CommandsPermission
	target.ResetPermission = settings.ResetPermission
	target.WrapUpPermission = settings.WrapUpPermission
}

func appAccessToken(ctx context.Context, cfg config.Config, logger *slog.Logger) string {
	if cfg.Twitch.ClientID == "" || cfg.Twitch.ClientSecret == "" {
		if logger != nil {
			logger.Warn("Twitch app access token unavailable; chat messages will not use the Chat Bot Badge app-token path",
				"has_client_id", cfg.Twitch.ClientID != "",
				"has_client_secret", cfg.Twitch.ClientSecret != "",
			)
		}
		return ""
	}
	tokenSet, err := twitch.NewAuthManager(twitch.AuthConfig{
		ClientID:     cfg.Twitch.ClientID,
		ClientSecret: cfg.Twitch.ClientSecret,
		StatePath:    cfg.Twitch.AppTokenStatePath,
	}).AppAccessToken(ctx)
	if err != nil {
		if logger != nil {
			logger.Warn("failed to get Twitch app access token; chat messages will use the user token", "error", err)
		}
		return ""
	}
	if logger != nil {
		logger.Info("using Twitch app access token for chat message sending", "expires_at", tokenSet.ExpiresAt.Format(time.RFC3339))
	}
	return tokenSet.AccessToken
}

func newTwitchChat(cfg config.Config, broadcasterID, botUserID, sendToken string, adBreaks chan<- twitch.AdBreakEvent, redeems chan<- twitch.ChannelPointRedeemEvent, wrapEvents chan<- wrapup.Event, logger *slog.Logger) bot.Chat {
	if cfg.Twitch.ClientID != "" && cfg.Twitch.OAuthToken != "" && broadcasterID != "" && botUserID != "" {
		if logger != nil {
			logger.Info("configuring EventSub chat transport",
				"redeems_enabled", redeems != nil,
				"has_redeem_token", cfg.Twitch.AdsOAuthToken != "",
				"has_redeem_client_id", cfg.Twitch.AdsClientID != "",
			)
		}
		return twitch.NewEventSubChatClient(twitch.EventSubConfig{
			ClientID:            cfg.Twitch.ClientID,
			Token:               cfg.Twitch.OAuthToken,
			SendToken:           sendToken,
			AdClientID:          cfg.Twitch.AdsClientID,
			AdToken:             firstAdToken(cfg),
			RedeemClientID:      cfg.Twitch.AdsClientID,
			RedeemToken:         cfg.Twitch.AdsOAuthToken,
			BroadcasterToken:    cfg.Twitch.AdsOAuthToken,
			BroadcasterClientID: cfg.Twitch.AdsClientID,
			ModeratorID:         broadcasterID,
			Channel:             cfg.Twitch.Channel,
			BroadcasterID:       broadcasterID,
			UserID:              botUserID,
			AdBreaks:            adBreaks,
			Redeems:             redeems,
			WrapUpEvents:        wrapEvents,
		}, logger)
	}
	if logger != nil {
		logger.Warn("using legacy IRC chat transport; EventSub chat requires client ID, access token, broadcaster ID, and bot user ID",
			"has_client_id", cfg.Twitch.ClientID != "",
			"has_token", cfg.Twitch.OAuthToken != "",
			"has_broadcaster_id", broadcasterID != "",
			"has_bot_user_id", botUserID != "",
		)
	}
	return twitch.NewClient(twitch.Config{
		Username: cfg.Twitch.BotUsername,
		Token:    cfg.Twitch.OAuthToken,
		Channel:  cfg.Twitch.Channel,
	}, logger)
}

func firstAdToken(cfg config.Config) string {
	if cfg.Twitch.AdsOAuthToken != "" {
		return cfg.Twitch.AdsOAuthToken
	}
	return cfg.Twitch.OAuthToken
}

func convertChannelEmotes(items []twitch.ChannelEmote) []twitch.Emote {
	emotes := make([]twitch.Emote, 0, len(items))
	for _, item := range items {
		if item.ID == "" || item.Name == "" {
			continue
		}
		emotes = append(emotes, twitch.Emote{ID: item.ID, Name: item.Name, Count: 1})
	}
	return emotes
}

type adScheduleHelix interface {
	GetAdSchedule(ctx context.Context, broadcasterID string) (twitch.AdSchedule, error)
	SetAccessToken(accessToken string)
}

type adTokenRefresher interface {
	Refresh(ctx context.Context) (twitch.TokenSet, error)
}

type twitchAdScheduleProvider struct {
	mu             sync.Mutex
	helix          adScheduleHelix
	auth           adTokenRefresher
	tokenExpiresAt time.Time
	logger         *slog.Logger
}

func (p *twitchAdScheduleProvider) GetAdSchedule(ctx context.Context, broadcasterID string) (adalerts.Schedule, error) {
	if err := p.refreshIfNeeded(ctx, false); err != nil {
		return adalerts.Schedule{}, err
	}
	schedule, err := p.helix.GetAdSchedule(ctx, broadcasterID)
	if err != nil {
		if isUnauthorized(err) {
			if refreshErr := p.refreshIfNeeded(ctx, true); refreshErr != nil {
				return adalerts.Schedule{}, refreshErr
			}
			schedule, err = p.helix.GetAdSchedule(ctx, broadcasterID)
			if err == nil {
				return convertAdSchedule(schedule), nil
			}
		}
		return adalerts.Schedule{}, err
	}
	return convertAdSchedule(schedule), nil
}

func (p *twitchAdScheduleProvider) refreshIfNeeded(ctx context.Context, force bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.auth == nil {
		return nil
	}
	if !force && (p.tokenExpiresAt.IsZero() || time.Now().Before(p.tokenExpiresAt.Add(-2*time.Minute))) {
		return nil
	}
	tokenSet, err := p.auth.Refresh(ctx)
	if err != nil {
		return fmt.Errorf("refresh twitch ads token: %w", err)
	}
	p.helix.SetAccessToken(tokenSet.AccessToken)
	p.tokenExpiresAt = tokenSet.ExpiresAt
	if p.logger != nil {
		p.logger.Info("refreshed twitch ads access token for ad polling", "expires_at", tokenSet.ExpiresAt.Format(time.RFC3339))
	}
	return nil
}

func isUnauthorized(err error) bool {
	return strings.Contains(err.Error(), "401 Unauthorized")
}

func adsTokenExpiresAt(path string) time.Time {
	if strings.TrimSpace(path) == "" {
		return time.Time{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}
	}
	var tokenSet twitch.TokenSet
	if err := json.Unmarshal(data, &tokenSet); err != nil {
		return time.Time{}
	}
	return tokenSet.ExpiresAt
}

func preferConfiguredRefreshToken(configPath, statePath string) bool {
	if strings.TrimSpace(configPath) == "" || strings.TrimSpace(statePath) == "" {
		return false
	}
	configInfo, err := os.Stat(configPath)
	if err != nil {
		return false
	}
	stateInfo, err := os.Stat(statePath)
	if err != nil {
		return true
	}
	return configInfo.ModTime().After(stateInfo.ModTime())
}

func convertAdSchedule(schedule twitch.AdSchedule) adalerts.Schedule {
	return adalerts.Schedule{
		NextAdAt:        schedule.NextAdAt,
		LastAdAt:        schedule.LastAdAt,
		Duration:        schedule.Duration,
		PrerollFreeTime: schedule.PrerollFreeTime,
		SnoozeCount:     schedule.SnoozeCount,
		SnoozeRefreshAt: schedule.SnoozeRefreshAt,
	}
}

type userResolver interface {
	GetUsersByLogin(ctx context.Context, logins []string) ([]twitch.UserInfo, error)
}

func resolveRecentStreamerIDs(ctx context.Context, helix userResolver, channel, botUsername string, logger *slog.Logger) (string, string) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	users, err := helix.GetUsersByLogin(ctx, []string{channel, botUsername})
	if err != nil {
		logger.Warn("failed to resolve twitch user IDs for recent streamer tracking", "error", err)
		return "", ""
	}

	var broadcasterID, moderatorID string
	for _, user := range users {
		switch strings.ToLower(user.Login) {
		case strings.ToLower(channel):
			broadcasterID = user.ID
		case strings.ToLower(botUsername):
			moderatorID = user.ID
		}
	}
	if broadcasterID == "" || moderatorID == "" {
		logger.Warn("recent streamer chatter polling needs broadcaster and bot user IDs", "broadcaster_found", broadcasterID != "", "bot_found", moderatorID != "")
	}
	return broadcasterID, moderatorID
}

// Recheck Twitch even when an online/offline notification was missed. Failed
// lookups must never finalize a live session.
func reconcileWrapUp(ctx context.Context, provider bot.StreamInfoProvider, service *wrapup.Service, channel, broadcasterID string, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	info, err := provider.GetStreamInfo(ctx, channel)
	if err != nil {
		logger.Warn("could not reconcile wrap-up stream status; recording may be unavailable, will retry in one minute", "error", err)
		return
	}
	applyWrapUpStatus(service, info, broadcasterID, logger)
}

type verifiedStreamProvider struct {
	auth     adTokenRefresher
	provider bot.StreamInfoProvider
	observed func(twitch.StreamInfo)
}

func (p verifiedStreamProvider) GetStreamInfo(ctx context.Context, channel string) (twitch.StreamInfo, error) {
	info, err := p.provider.GetStreamInfo(ctx, channel)
	if err != nil && isUnauthorized(err) && p.auth != nil {
		tokens, refreshErr := p.auth.Refresh(ctx)
		if refreshErr != nil {
			return twitch.StreamInfo{}, refreshErr
		}
		if setter, ok := p.provider.(interface{ SetAccessToken(string) }); ok {
			setter.SetAccessToken(tokens.AccessToken)
			info, err = p.provider.GetStreamInfo(ctx, channel)
		}
	}
	if err == nil {
		p.observed(info)
	}
	return info, err
}
func applyWrapUpStatus(service *wrapup.Service, info twitch.StreamInfo, broadcasterID string, logger *slog.Logger) {
	var err error
	current, exists := service.Current()
	if info.Live {
		if exists && current.Status == "live" && current.StreamID == info.ID {
			return
		}
		err = service.Online(info.ID, broadcasterID, info.StartedAt)
	} else if exists && current.Status == "live" {
		err = service.Offline(time.Now().UTC())
	} else {
		return
	}
	if err != nil {
		logger.Warn("could not persist wrap-up stream status; check state file access", "error", err)
		return
	}
	logger.Info("wrap-up stream status reconciled", "live", info.Live, "stream_id", info.ID)
}
