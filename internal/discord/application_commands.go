package discord

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"
)

const (
	optionChannel = "channel"
	optionLocale  = "locale"
	optionMode    = "mode"
	optionRole    = "role"
	optionThread  = "thread"
)

func (r *Runtime) registerApplicationCommands() error {
	commands := r.applicationCommands()
	created, err := r.session.ApplicationCommandBulkOverwrite(r.session.State.User.ID, "", commands)
	if err != nil {
		return fmt.Errorf("register application commands: %w", err)
	}
	log.Printf("Registered %d application command(s).", len(created))
	return nil
}

func (r *Runtime) applicationCommands() []*discordgo.ApplicationCommand {
	guildAndDM := []discordgo.InteractionContextType{
		discordgo.InteractionContextGuild,
		discordgo.InteractionContextBotDM,
	}
	guildOnly := []discordgo.InteractionContextType{
		discordgo.InteractionContextGuild,
	}
	textTargets := []discordgo.ChannelType{
		discordgo.ChannelTypeGuildText,
		discordgo.ChannelTypeGuildNews,
		discordgo.ChannelTypeGuildPublicThread,
		discordgo.ChannelTypeGuildPrivateThread,
		discordgo.ChannelTypeGuildNewsThread,
	}
	textChannels := []discordgo.ChannelType{
		discordgo.ChannelTypeGuildText,
		discordgo.ChannelTypeGuildNews,
	}
	threadChannels := []discordgo.ChannelType{
		discordgo.ChannelTypeGuildPublicThread,
		discordgo.ChannelTypeGuildPrivateThread,
		discordgo.ChannelTypeGuildNewsThread,
	}

	return []*discordgo.ApplicationCommand{
		command("sub", "Subscribe to asset updates.", guildAndDM, []*discordgo.ApplicationCommandOption{
			channelOption(optionChannel, "Channel or thread to receive notifications.", textTargets),
		}),
		command("unsub", "Unsubscribe from asset updates.", guildAndDM, nil),
		command("enable", "Enable server asset updates.", guildOnly, nil),
		command("disable", "Disable server asset updates.", guildOnly, nil),
		command("set-channel", "Use a channel for notifications.", guildOnly, []*discordgo.ApplicationCommandOption{
			channelOption(optionChannel, "Notification channel.", textChannels),
		}),
		command("set-thread", "Use a thread for notifications.", guildOnly, []*discordgo.ApplicationCommandOption{
			channelOption(optionThread, "Notification thread.", threadChannels),
		}),
		command("clear-thread", "Clear the configured notification thread.", guildOnly, nil),
		command("set-role", "Mention a role in server notifications.", guildOnly, []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionRole,
				Name:        optionRole,
				Description: "Role to mention.",
				Required:    true,
			},
		}),
		command("clear-role", "Clear the notification mention role.", guildOnly, nil),
		command("images", "Show or change image attachment mode.", guildOnly, []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        optionMode,
				Description: "Image attachment mode.",
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "on", Value: "on"},
					{Name: "off", Value: "off"},
				},
			},
		}),
		command("settings", "Show current notification settings.", guildAndDM, nil),
		command("check", "Check notification delivery.", guildOnly, []*discordgo.ApplicationCommandOption{
			channelOption(optionChannel, "Temporary channel or thread to check.", textTargets),
		}),
		command("time", "Show time left until the next check.", guildAndDM, nil),
		command("lang", "Show or change notification language.", guildAndDM, []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        optionLocale,
				Description: "Locale code.",
				Required:    false,
				Choices:     r.localeCommandChoices(),
			},
		}),
	}
}

func channelOption(
	name string,
	description string,
	channelTypes []discordgo.ChannelType,
) *discordgo.ApplicationCommandOption {
	return &discordgo.ApplicationCommandOption{
		Type:         discordgo.ApplicationCommandOptionChannel,
		Name:         name,
		Description:  description,
		Required:     false,
		ChannelTypes: channelTypes,
	}
}

func command(
	name string,
	description string,
	contexts []discordgo.InteractionContextType,
	options []*discordgo.ApplicationCommandOption,
) *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        name,
		Description: description,
		Contexts:    &contexts,
		Options:     options,
	}
}

func (r *Runtime) localeCommandChoices() []*discordgo.ApplicationCommandOptionChoice {
	locales := r.localeChoices()
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(locales))
	for _, locale := range locales {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  locale,
			Value: locale,
		})
	}
	return choices
}

func (r *Runtime) onInteractionCreate(_ *discordgo.Session, interaction *discordgo.InteractionCreate) {
	if interaction == nil || interaction.Type != discordgo.InteractionApplicationCommand {
		return
	}

	data := interaction.ApplicationCommandData()
	command, args, mentionRoles, targetID := parseCommandInteraction(data)
	if command == "" {
		return
	}

	message := newInteractionCommandContext(
		interaction,
		commandContent(command, args),
		mentionRoles,
		targetID,
	)
	if err := message.deferResponse(r); err != nil {
		log.Printf("defer command %q failed: %v", command, err)
		return
	}

	go func() {
		if err := r.handleCommand(context.Background(), message, command, args); err != nil && !isContextDone(err) {
			log.Printf("command %q failed: %v", command, err)
			if !message.hasFinalResponse() {
				_ = message.respond(r, fmt.Sprintf("Command failed: %v", err))
			}
		}
	}()
}

func parseCommandInteraction(data discordgo.ApplicationCommandInteractionData) (string, []string, []string, string) {
	args := make([]string, 0, len(data.Options))
	mentionRoles := make([]string, 0, 1)
	targetID := ""

	if option := data.GetOption(optionMode); option != nil {
		args = append(args, option.StringValue())
	}
	if option := data.GetOption(optionLocale); option != nil {
		args = append(args, option.StringValue())
	}
	if option := data.GetOption(optionRole); option != nil {
		roleID := fmt.Sprint(option.Value)
		args = append(args, roleID)
		mentionRoles = append(mentionRoles, roleID)
	}
	if option := data.GetOption(optionChannel); option != nil {
		targetID = fmt.Sprint(option.Value)
	}
	if option := data.GetOption(optionThread); option != nil {
		targetID = fmt.Sprint(option.Value)
	}
	return data.Name, args, mentionRoles, targetID
}

func commandContent(command string, args []string) string {
	if len(args) == 0 {
		return command
	}
	return fmt.Sprintf("%s %s", command, strings.Join(args, " "))
}
