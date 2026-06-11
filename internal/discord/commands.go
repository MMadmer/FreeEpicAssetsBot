package discord

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"epicassetsnotifybot/internal/i18n"
	"epicassetsnotifybot/internal/model"

	"github.com/bwmarrin/discordgo"
)

func (r *Runtime) handleCommand(ctx context.Context, message *commandContext, command string, args []string) error {
	switch command {
	case "sub":
		return r.handleSubscribe(ctx, message)
	case "unsub":
		return r.handleUnsubscribe(message)
	case "enable", "on":
		return r.handleEnable(ctx, message)
	case "disable", "off":
		return r.handleDisable(message)
	case "set-channel", "setchannel":
		return r.handleSetChannel(ctx, message)
	case "set-thread", "setthread":
		return r.handleSetThread(ctx, message)
	case "clear-thread", "clearthread":
		return r.handleClearThread(ctx, message)
	case "set-role", "setrole":
		return r.handleSetRole(message)
	case "clear-role", "clearrole":
		return r.handleClearRole(message)
	case "images":
		mode := ""
		if len(args) > 0 {
			mode = args[0]
		}
		return r.handleImages(message, mode)
	case "settings", "config":
		return r.handleSettings(message)
	case "check":
		return r.handleCheck(message)
	case "time":
		return r.handleTime(message)
	case "lang", "locale", "l":
		locale := ""
		if len(args) > 0 {
			locale = args[0]
		}
		return r.handleLang(message, locale)
	default:
		localizer := r.localizerForMessage(message)
		return message.respond(r, localizer.T("errors.unknown_command", map[string]any{"command": command}))
	}
}

func (r *Runtime) handleSubscribe(ctx context.Context, message *commandContext) error {
	localizer := r.localizerForMessage(message)
	if !r.isDM(message) && !r.isAdmin(message) {
		return message.respond(r, localizer.T("errors.permission_denied", nil))
	}

	if r.isDM(message) {
		userID, _ := strconv.ParseInt(message.author.ID, 10, 64)

		r.mu.Lock()
		profile := r.ensureUserProfileLocked(userID)
		if profile.Subscribed {
			r.mu.Unlock()
			return message.respond(r, localizer.T("subscribe.dm.already", nil))
		}
		profile.Subscribed = true
		profile.ShownAssets = false
		r.markStateDirtyLocked()
		r.mu.Unlock()

		if err := message.respond(r, localizer.T("subscribe.dm.success", nil)); err != nil {
			return err
		}
		log.Printf("User %s subscribed to asset updates.", message.author.ID)
		r.requestFlush()
		r.sendCurrentAssetsToUser(ctx, userID)
		return nil
	}

	channel, err := r.commandTargetChannel(message)
	if err != nil {
		return err
	}
	channelID, threadID := currentTargetIDs(channel)
	guildID, _ := strconv.ParseInt(message.guildID, 10, 64)

	var cfgCopy model.GuildConfig
	r.mu.Lock()
	cfg := r.ensureGuildConfigLocked(guildID)
	if cfg.Enabled && equalInt64Ptr(cfg.ChannelID, channelID) && equalInt64Ptr(cfg.ThreadID, threadID) {
		cfgCopy = cloneGuildConfig(*cfg)
		r.mu.Unlock()
		return message.respond(r, localizer.T("subscribe.channel.already", nil))
	}
	r.setGuildTarget(cfg, channelID, threadID)
	cfg.Enabled = true
	cfgCopy = cloneGuildConfig(*cfg)
	r.markStateDirtyLocked()
	r.mu.Unlock()

	if err := message.respond(r, localizer.T("subscribe.channel.success", map[string]any{
		"channel_name": r.formatTargetLabel(cfgCopy, localizer),
	})); err != nil {
		return err
	}
	log.Printf("Guild %s subscribed via %s.", message.guildID, r.formatTargetLabel(cfgCopy, localizer))
	r.requestFlush()
	r.sendCurrentAssetsToGuild(ctx, guildID)
	return nil
}

func (r *Runtime) handleUnsubscribe(message *commandContext) error {
	localizer := r.localizerForMessage(message)
	if !r.isDM(message) && !r.isAdmin(message) {
		return message.respond(r, localizer.T("errors.permission_denied", nil))
	}

	if r.isDM(message) {
		userID, _ := strconv.ParseInt(message.author.ID, 10, 64)

		r.mu.Lock()
		profile := r.userProfileLocked(userID)
		if profile == nil || !profile.Subscribed {
			r.mu.Unlock()
			return message.respond(r, localizer.T("unsubscribe.dm.not_subscribed", nil))
		}
		profile.Subscribed = false
		profile.ShownAssets = false
		r.markStateDirtyLocked()
		r.mu.Unlock()

		if err := message.respond(r, localizer.T("unsubscribe.success", nil)); err != nil {
			return err
		}
		log.Printf("User %s unsubscribed from asset updates.", message.author.ID)
		r.requestFlush()
		return nil
	}

	guildID, _ := strconv.ParseInt(message.guildID, 10, 64)

	r.mu.Lock()
	cfg := r.guildConfigLocked(guildID)
	if cfg == nil || !cfg.Enabled {
		r.mu.Unlock()
		return message.respond(r, localizer.T("unsubscribe.channel.not_subscribed", nil))
	}
	cfg.Enabled = false
	cfg.ShownAssets = false
	r.markStateDirtyLocked()
	r.mu.Unlock()

	if err := message.respond(r, localizer.T("unsubscribe.success", nil)); err != nil {
		return err
	}
	log.Printf("Guild %s disabled asset updates.", message.guildID)
	r.requestFlush()
	return nil
}

func (r *Runtime) handleEnable(ctx context.Context, message *commandContext) error {
	if r.isDM(message) {
		return r.handleSubscribe(ctx, message)
	}

	localizer := r.localizerForMessage(message)
	if !r.isAdmin(message) {
		return message.respond(r, localizer.T("errors.permission_denied", nil))
	}

	guildID, _ := strconv.ParseInt(message.guildID, 10, 64)

	r.mu.RLock()
	cfg := r.guildConfigLocked(guildID)
	if cfg == nil || cfg.ChannelID == nil {
		r.mu.RUnlock()
		return message.respond(r, strings.Join([]string{
			localizer.T("server.settings.not_configured", nil),
			localizer.T("server.settings.hint", nil),
		}, "\n"))
	}
	cfgCopy := cloneGuildConfig(*cfg)
	r.mu.RUnlock()

	if validationError := r.validateGuildConfigForEnable(cfgCopy, localizer); validationError != "" {
		return message.respond(r, validationError)
	}

	if cfgCopy.Enabled {
		return message.respond(r, localizer.T("server.enable.already", map[string]any{
			"target": r.formatTargetLabel(cfgCopy, localizer),
		}))
	}

	r.mu.Lock()
	cfg = r.guildConfigLocked(guildID)
	if cfg == nil || cfg.ChannelID == nil {
		r.mu.Unlock()
		return message.respond(r, strings.Join([]string{
			localizer.T("server.settings.not_configured", nil),
			localizer.T("server.settings.hint", nil),
		}, "\n"))
	}
	cfg.Enabled = true
	cfgCopy = cloneGuildConfig(*cfg)
	r.markStateDirtyLocked()
	r.mu.Unlock()

	if err := message.respond(r, localizer.T("server.enable.success", map[string]any{
		"target": r.formatTargetLabel(cfgCopy, localizer),
	})); err != nil {
		return err
	}
	r.requestFlush()
	r.sendCurrentAssetsToGuild(ctx, guildID)
	return nil
}

func (r *Runtime) validateGuildConfigForEnable(config model.GuildConfig, localizer *i18n.Localizer) string {
	targetChannelID := r.targetChannelID(config)
	if targetChannelID == "" {
		return strings.Join([]string{
			localizer.T("server.settings.not_configured", nil),
			localizer.T("server.settings.hint", nil),
		}, "\n")
	}

	channel, err := r.resolveChannelObject(targetChannelID)
	if err != nil || channel == nil {
		return strings.Join([]string{
			localizer.T("server.test.target_missing", nil),
			localizer.T("server.settings.hint", nil),
		}, "\n")
	}

	missingPermissions := r.missingTargetPermissions(targetChannelID, config.IncludeImages)
	if len(missingPermissions) > 0 {
		return localizer.T("server.test.missing_permissions", map[string]any{
			"permissions": strings.Join(missingPermissions, ", "),
		})
	}
	return ""
}

func (r *Runtime) handleDisable(message *commandContext) error {
	if r.isDM(message) {
		return r.handleUnsubscribe(message)
	}

	localizer := r.localizerForMessage(message)
	if !r.isAdmin(message) {
		return message.respond(r, localizer.T("errors.permission_denied", nil))
	}

	guildID, _ := strconv.ParseInt(message.guildID, 10, 64)

	r.mu.Lock()
	cfg := r.guildConfigLocked(guildID)
	if cfg == nil || !cfg.Enabled {
		r.mu.Unlock()
		return message.respond(r, localizer.T("server.disable.already", nil))
	}
	cfg.Enabled = false
	cfg.ShownAssets = false
	r.markStateDirtyLocked()
	r.mu.Unlock()

	if err := message.respond(r, localizer.T("server.disable.success", nil)); err != nil {
		return err
	}
	r.requestFlush()
	return nil
}

func (r *Runtime) handleSetChannel(ctx context.Context, message *commandContext) error {
	localizer := r.localizerForMessage(message)
	if r.isDM(message) {
		return message.respond(r, localizer.T("errors.guild_only", nil))
	}
	if !r.isAdmin(message) {
		return message.respond(r, localizer.T("errors.permission_denied", nil))
	}

	channel, err := r.commandTargetChannel(message)
	if err != nil {
		return err
	}
	channelID, _ := currentTargetIDs(channel)
	guildID, _ := strconv.ParseInt(message.guildID, 10, 64)

	var cfgCopy model.GuildConfig
	r.mu.Lock()
	cfg := r.ensureGuildConfigLocked(guildID)
	if !r.setGuildTarget(cfg, channelID, nil) {
		cfgCopy = cloneGuildConfig(*cfg)
		r.mu.Unlock()
		return message.respond(r, localizer.T("server.channel.already", map[string]any{
			"target": r.formatTargetLabel(cfgCopy, localizer),
		}))
	}
	cfgCopy = cloneGuildConfig(*cfg)
	r.markStateDirtyLocked()
	r.mu.Unlock()

	if err := message.respond(r, localizer.T("server.channel.updated", map[string]any{
		"target": r.formatTargetLabel(cfgCopy, localizer),
	})); err != nil {
		return err
	}
	r.requestFlush()
	r.sendCurrentAssetsToGuild(ctx, guildID)
	return nil
}

func (r *Runtime) handleSetThread(ctx context.Context, message *commandContext) error {
	localizer := r.localizerForMessage(message)
	if r.isDM(message) {
		return message.respond(r, localizer.T("errors.guild_only", nil))
	}
	if !r.isAdmin(message) {
		return message.respond(r, localizer.T("errors.permission_denied", nil))
	}

	channel, err := r.commandTargetChannel(message)
	if err != nil {
		return err
	}
	if channel == nil || !isThreadChannel(channel.Type) {
		return message.respond(r, localizer.T("server.thread.not_in_thread", nil))
	}

	channelID, threadID := currentTargetIDs(channel)
	guildID, _ := strconv.ParseInt(message.guildID, 10, 64)

	var cfgCopy model.GuildConfig
	r.mu.Lock()
	cfg := r.ensureGuildConfigLocked(guildID)
	if !r.setGuildTarget(cfg, channelID, threadID) {
		cfgCopy = cloneGuildConfig(*cfg)
		r.mu.Unlock()
		return message.respond(r, localizer.T("server.thread.already", map[string]any{
			"target": r.formatTargetLabel(cfgCopy, localizer),
		}))
	}
	cfgCopy = cloneGuildConfig(*cfg)
	r.markStateDirtyLocked()
	r.mu.Unlock()

	if err := message.respond(r, localizer.T("server.thread.updated", map[string]any{
		"target": r.formatTargetLabel(cfgCopy, localizer),
	})); err != nil {
		return err
	}
	r.requestFlush()
	r.sendCurrentAssetsToGuild(ctx, guildID)
	return nil
}

func (r *Runtime) handleClearThread(ctx context.Context, message *commandContext) error {
	localizer := r.localizerForMessage(message)
	if r.isDM(message) {
		return message.respond(r, localizer.T("errors.guild_only", nil))
	}
	if !r.isAdmin(message) {
		return message.respond(r, localizer.T("errors.permission_denied", nil))
	}

	guildID, _ := strconv.ParseInt(message.guildID, 10, 64)

	var cfgCopy model.GuildConfig
	r.mu.Lock()
	cfg := r.guildConfigLocked(guildID)
	if cfg == nil || cfg.ThreadID == nil {
		r.mu.Unlock()
		return message.respond(r, localizer.T("server.thread.already_cleared", nil))
	}
	r.setGuildTarget(cfg, cfg.ChannelID, nil)
	cfgCopy = cloneGuildConfig(*cfg)
	r.markStateDirtyLocked()
	r.mu.Unlock()

	if err := message.respond(r, localizer.T("server.thread.cleared", map[string]any{
		"target": r.formatTargetLabel(cfgCopy, localizer),
	})); err != nil {
		return err
	}
	r.requestFlush()
	r.sendCurrentAssetsToGuild(ctx, guildID)
	return nil
}

func (r *Runtime) handleSetRole(message *commandContext) error {
	localizer := r.localizerForMessage(message)
	if r.isDM(message) {
		return message.respond(r, localizer.T("errors.guild_only", nil))
	}
	if !r.isAdmin(message) {
		return message.respond(r, localizer.T("errors.permission_denied", nil))
	}

	roleID, ok := parseRoleID(message)
	if !ok {
		return message.respond(r, localizer.T("errors.invalid_arguments", nil))
	}

	guildID, _ := strconv.ParseInt(message.guildID, 10, 64)

	r.mu.Lock()
	cfg := r.ensureGuildConfigLocked(guildID)
	if cfg.MentionRoleID != nil && *cfg.MentionRoleID == roleID {
		r.mu.Unlock()
		return message.respond(r, localizer.T("server.role.already", map[string]any{
			"role": fmt.Sprintf("<@&%d>", roleID),
		}))
	}
	cfg.MentionRoleID = &roleID
	r.markStateDirtyLocked()
	r.mu.Unlock()

	if err := message.respond(r, localizer.T("server.role.updated", map[string]any{
		"role": fmt.Sprintf("<@&%d>", roleID),
	})); err != nil {
		return err
	}
	r.requestFlush()
	return nil
}

func (r *Runtime) handleClearRole(message *commandContext) error {
	localizer := r.localizerForMessage(message)
	if r.isDM(message) {
		return message.respond(r, localizer.T("errors.guild_only", nil))
	}
	if !r.isAdmin(message) {
		return message.respond(r, localizer.T("errors.permission_denied", nil))
	}

	guildID, _ := strconv.ParseInt(message.guildID, 10, 64)

	r.mu.Lock()
	cfg := r.guildConfigLocked(guildID)
	if cfg == nil || cfg.MentionRoleID == nil {
		r.mu.Unlock()
		return message.respond(r, localizer.T("server.role.already_cleared", nil))
	}
	cfg.MentionRoleID = nil
	r.markStateDirtyLocked()
	r.mu.Unlock()

	if err := message.respond(r, localizer.T("server.role.cleared", nil)); err != nil {
		return err
	}
	r.requestFlush()
	return nil
}

func (r *Runtime) handleImages(message *commandContext, mode string) error {
	localizer := r.localizerForMessage(message)
	if r.isDM(message) {
		return message.respond(r, localizer.T("errors.guild_only", nil))
	}
	if !r.isAdmin(message) {
		return message.respond(r, localizer.T("errors.permission_denied", nil))
	}

	guildID, _ := strconv.ParseInt(message.guildID, 10, 64)

	r.mu.Lock()
	cfg := r.ensureGuildConfigLocked(guildID)
	if strings.TrimSpace(mode) == "" {
		value := cfg.IncludeImages
		r.mu.Unlock()
		return message.respond(r, localizer.T("server.images.current", map[string]any{
			"value": r.formatBoolLabel(value, localizer),
		}))
	}

	modeMap := map[string]bool{
		"on": true, "true": true, "yes": true, "1": true,
		"off": false, "false": false, "no": false, "0": false,
	}
	includeImages, ok := modeMap[strings.ToLower(strings.TrimSpace(mode))]
	if !ok {
		r.mu.Unlock()
		return message.respond(r, localizer.T("server.images.invalid", nil))
	}
	if cfg.IncludeImages == includeImages {
		current := cfg.IncludeImages
		r.mu.Unlock()
		return message.respond(r, localizer.T("server.images.already", map[string]any{
			"value": r.formatBoolLabel(current, localizer),
		}))
	}

	cfg.IncludeImages = includeImages
	r.markStateDirtyLocked()
	r.mu.Unlock()

	if err := message.respond(r, localizer.T("server.images.updated", map[string]any{
		"value": r.formatBoolLabel(includeImages, localizer),
	})); err != nil {
		return err
	}
	r.requestFlush()
	return nil
}

func (r *Runtime) handleSettings(message *commandContext) error {
	localizer := r.localizerForMessage(message)
	if r.isDM(message) {
		userID, _ := strconv.ParseInt(message.author.ID, 10, 64)

		r.mu.RLock()
		profile := r.userProfiles[userID]
		if profile == nil {
			r.mu.RUnlock()
			return message.respond(r, localizer.T("server.settings.dm_not_configured", nil))
		}
		profileCopy := *profile
		r.mu.RUnlock()

		status := r.formatBoolLabel(profileCopy.Subscribed, localizer)
		return message.respond(r, strings.Join([]string{
			localizer.T("server.settings.dm_header", nil),
			localizer.T("server.settings.dm_status", map[string]any{"value": status}),
			localizer.T("server.settings.locale", map[string]any{
				"locale_name": r.localizer.LocaleName(profileCopy.Locale),
				"locale_code": profileCopy.Locale,
			}),
		}, "\n"))
	}

	guildID, _ := strconv.ParseInt(message.guildID, 10, 64)
	r.mu.RLock()
	cfg := r.guildConfigs[guildID]
	var cfgCopy *model.GuildConfig
	if cfg != nil {
		cloned := cloneGuildConfig(*cfg)
		cfgCopy = &cloned
	}
	r.mu.RUnlock()

	return message.respond(r, r.settingsMessage(cfgCopy, localizer))
}

func (r *Runtime) handleCheck(message *commandContext) error {
	localizer := r.localizerForMessage(message)
	if r.isDM(message) {
		return message.respond(r, localizer.T("errors.guild_only", nil))
	}
	if !r.isAdmin(message) {
		return message.respond(r, localizer.T("errors.permission_denied", nil))
	}

	guildID, _ := strconv.ParseInt(message.guildID, 10, 64)
	r.mu.RLock()
	cfg := r.guildConfigs[guildID]
	cfgCopy := model.GuildConfig{
		GuildID:       guildID,
		Locale:        r.baseLocale,
		IncludeImages: true,
	}
	if cfg != nil {
		cfgCopy = cloneGuildConfig(*cfg)
	}
	r.mu.RUnlock()

	if cfg == nil && message.targetID == "" {
		return message.respond(r, localizer.T("server.test.not_configured", nil))
	}

	if message.targetID != "" {
		channel, err := r.commandTargetChannel(message)
		if err != nil {
			return err
		}
		channelID, threadID := currentTargetIDs(channel)
		r.setGuildTarget(&cfgCopy, channelID, threadID)
	}

	targetChannelID := r.targetChannelID(cfgCopy)
	if targetChannelID == "" {
		return message.respond(r, localizer.T("server.test.target_missing", nil))
	}

	if validationError := r.validateGuildConfigForEnable(cfgCopy, localizer); validationError != "" {
		return message.respond(r, validationError)
	}

	guildName := message.guildID
	if r.session.State != nil {
		if guild, err := r.session.State.Guild(message.guildID); err == nil && guild != nil {
			guildName = guild.Name
		}
	}
	testBody := localizer.T("server.test.body", map[string]any{"guild_name": guildName})
	if err := r.sendAssetMessage(targetChannelID, r.composeDeliveryContent(cfgCopy, testBody), nil); err != nil {
		return message.respond(r, localizer.T("server.test.failed", map[string]any{"error": err.Error()}))
	}

	confirmation := localizer.T("server.test.sent", map[string]any{
		"target": r.formatTargetLabel(cfgCopy, localizer),
	})
	return message.respond(r, confirmation)
}

func (r *Runtime) handleTime(message *commandContext) error {
	localizer := r.localizerForMessage(message)
	deleteHint := localizer.T("time.delete_hint", map[string]any{
		"delete_after": int(r.deleteAfter.Seconds()),
	})

	r.mu.RLock()
	nextCheck := r.nextCheck
	r.mu.RUnlock()

	if !nextCheck.IsZero() {
		remaining := time.Until(nextCheck)
		if remaining < 0 {
			remaining = 0
		}
		totalSeconds := int(remaining.Seconds())
		hours := totalSeconds / 3600
		minutes := (totalSeconds % 3600) / 60
		seconds := totalSeconds % 60
		return message.respondTemporary(r, strings.Join([]string{
			localizer.T("time.remaining", map[string]any{
				"hours":   hours,
				"minutes": minutes,
				"seconds": seconds,
			}),
			deleteHint,
		}, "\n"))
	}

	return message.respondTemporary(r, strings.Join([]string{
		localizer.T("time.no_schedule", nil),
		deleteHint,
	}, "\n"))
}

func (r *Runtime) handleLang(message *commandContext, locale string) error {
	usage := r.localeUsage()
	if r.isDM(message) {
		userID, _ := strconv.ParseInt(message.author.ID, 10, 64)
		localizer := r.localizer.ForLocale(r.userLocale(userID))
		currentLocale := r.userLocale(userID)

		if strings.TrimSpace(locale) == "" {
			return message.respond(r, strings.Join([]string{
				localizer.T("locale.dm.current", map[string]any{
					"locale_name": r.localizer.LocaleName(currentLocale),
					"locale_code": currentLocale,
				}),
				localizer.T("locale.available", map[string]any{"locales": r.availableLocalesLabel()}),
				localizer.T("locale.usage", map[string]any{"command": usage}),
			}, "\n"))
		}

		resolved := r.localizer.NormalizeLocale(locale)
		if resolved == "" {
			return message.respond(r, strings.Join([]string{
				localizer.T("locale.invalid", map[string]any{"input_value": locale}),
				localizer.T("locale.available", map[string]any{"locales": r.availableLocalesLabel()}),
				localizer.T("locale.usage", map[string]any{"command": usage}),
			}, "\n"))
		}
		if currentLocale == resolved {
			return message.respond(r, localizer.T("locale.dm.already", map[string]any{
				"locale_name": r.localizer.LocaleName(resolved),
				"locale_code": resolved,
			}))
		}

		r.mu.Lock()
		profile := r.ensureUserProfileLocked(userID)
		profile.Locale = resolved
		r.markStateDirtyLocked()
		r.mu.Unlock()
		r.requestFlush()

		newLocalizer := r.localizer.ForLocale(resolved)
		return message.respond(r, newLocalizer.T("locale.dm.changed", map[string]any{
			"locale_name": r.localizer.LocaleName(resolved),
			"locale_code": resolved,
		}))
	}

	localizer := r.localizerForMessage(message)
	guildID, _ := strconv.ParseInt(message.guildID, 10, 64)
	currentLocale := r.guildLocale(guildID)
	if strings.TrimSpace(locale) == "" {
		return message.respond(r, strings.Join([]string{
			localizer.T("locale.server.current", map[string]any{
				"locale_name": r.localizer.LocaleName(currentLocale),
				"locale_code": currentLocale,
			}),
			localizer.T("locale.available", map[string]any{"locales": r.availableLocalesLabel()}),
			localizer.T("locale.usage", map[string]any{"command": usage}),
		}, "\n"))
	}
	if !r.isAdmin(message) {
		return message.respond(r, localizer.T("errors.permission_denied", nil))
	}

	resolved := r.localizer.NormalizeLocale(locale)
	if resolved == "" {
		return message.respond(r, strings.Join([]string{
			localizer.T("locale.invalid", map[string]any{"input_value": locale}),
			localizer.T("locale.available", map[string]any{"locales": r.availableLocalesLabel()}),
			localizer.T("locale.usage", map[string]any{"command": usage}),
		}, "\n"))
	}
	if currentLocale == resolved {
		return message.respond(r, localizer.T("locale.server.already", map[string]any{
			"locale_name": r.localizer.LocaleName(resolved),
			"locale_code": resolved,
		}))
	}

	r.mu.Lock()
	cfg := r.ensureGuildConfigLocked(guildID)
	cfg.Locale = resolved
	r.markStateDirtyLocked()
	r.mu.Unlock()
	r.requestFlush()

	newLocalizer := r.localizer.ForLocale(resolved)
	return message.respond(r, newLocalizer.T("locale.server.changed", map[string]any{
		"locale_name": r.localizer.LocaleName(resolved),
		"locale_code": resolved,
	}))
}

func (r *Runtime) isDM(message *commandContext) bool {
	return message.guildID == ""
}

func (r *Runtime) commandTargetChannel(message *commandContext) (*discordgo.Channel, error) {
	channelID := message.channelID
	if message.targetID != "" {
		channelID = message.targetID
	}
	return r.resolveChannelObject(channelID)
}

func (r *Runtime) isAdmin(message *commandContext) bool {
	if message == nil || message.author == nil || message.guildID == "" {
		return false
	}

	if message.member != nil && message.member.Permissions&discordgo.PermissionAdministrator != 0 {
		return true
	}

	guild, err := r.resolveGuild(message.guildID)
	if err == nil && guild != nil && guild.OwnerID == message.author.ID {
		return true
	}

	member := message.member
	if member == nil {
		member, _ = r.session.GuildMember(message.guildID, message.author.ID)
	}
	if guild == nil || member == nil {
		return false
	}

	return memberHasAdministrator(guild, member)
}

func (r *Runtime) resolveGuild(guildID string) (*discordgo.Guild, error) {
	if guildID == "" {
		return nil, nil
	}
	if r.session.State != nil {
		if guild, err := r.session.State.Guild(guildID); err == nil && guild != nil {
			return guild, nil
		}
	}
	return r.session.Guild(guildID)
}

func memberHasAdministrator(guild *discordgo.Guild, member *discordgo.Member) bool {
	if guild == nil || member == nil {
		return false
	}

	if member.Permissions&discordgo.PermissionAdministrator != 0 {
		return true
	}

	roleIDs := map[string]struct{}{
		guild.ID: {},
	}
	for _, roleID := range member.Roles {
		roleIDs[roleID] = struct{}{}
	}

	for _, role := range guild.Roles {
		if _, ok := roleIDs[role.ID]; !ok {
			continue
		}
		if role.Permissions&discordgo.PermissionAdministrator != 0 {
			return true
		}
	}
	return false
}

func parseRoleID(message *commandContext) (int64, bool) {
	if message == nil {
		return 0, false
	}
	if len(message.mentionRoles) > 0 {
		roleID, err := strconv.ParseInt(message.mentionRoles[0], 10, 64)
		return roleID, err == nil
	}

	fields := strings.Fields(strings.TrimSpace(message.content))
	for _, field := range fields {
		field = strings.TrimPrefix(field, "<@&")
		field = strings.TrimSuffix(field, ">")
		roleID, err := strconv.ParseInt(field, 10, 64)
		if err == nil {
			return roleID, true
		}
	}
	return 0, false
}
