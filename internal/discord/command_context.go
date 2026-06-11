package discord

import (
	"time"

	"github.com/bwmarrin/discordgo"
)

type commandContext struct {
	channelID    string
	targetID     string
	guildID      string
	content      string
	mentionRoles []string
	author       *discordgo.User
	member       *discordgo.Member
	interaction  *discordgo.Interaction
	responded    bool
	deferred     bool
}

func newInteractionCommandContext(
	interaction *discordgo.InteractionCreate,
	content string,
	mentionRoles []string,
	targetID string,
) *commandContext {
	author := interaction.User
	if interaction.Member != nil {
		author = interaction.Member.User
	}

	return &commandContext{
		channelID:    interaction.ChannelID,
		targetID:     targetID,
		guildID:      interaction.GuildID,
		content:      content,
		mentionRoles: mentionRoles,
		author:       author,
		member:       interaction.Member,
		interaction:  interaction.Interaction,
	}
}

func (c *commandContext) respond(runtime *Runtime, content string) error {
	if c.interaction == nil {
		_, err := runtime.session.ChannelMessageSend(c.channelID, content)
		return err
	}

	if c.deferred {
		_, err := runtime.session.InteractionResponseEdit(c.interaction, &discordgo.WebhookEdit{
			Content: &content,
		})
		if err == nil {
			c.deferred = false
		}
		return err
	}

	if !c.responded {
		err := runtime.session.InteractionRespond(c.interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: content,
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		if err == nil {
			c.responded = true
		}
		return err
	}

	_, err := runtime.session.FollowupMessageCreate(c.interaction, false, &discordgo.WebhookParams{
		Content: content,
		Flags:   discordgo.MessageFlagsEphemeral,
	})
	return err
}

func (c *commandContext) deferResponse(runtime *Runtime) error {
	if c.interaction == nil || c.responded {
		return nil
	}

	err := runtime.session.InteractionRespond(c.interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})
	if err == nil {
		c.responded = true
		c.deferred = true
	}
	return err
}

func (c *commandContext) hasFinalResponse() bool {
	return c.responded && !c.deferred
}

func (c *commandContext) respondTemporary(runtime *Runtime, content string) error {
	if c.interaction == nil {
		return runtime.sendTemporaryMessage(c.channelID, content)
	}

	if err := c.respond(runtime, content); err != nil {
		return err
	}

	go func() {
		timer := time.NewTimer(runtime.deleteAfter)
		defer timer.Stop()
		<-timer.C
		_ = runtime.session.InteractionResponseDelete(c.interaction)
	}()
	return nil
}
