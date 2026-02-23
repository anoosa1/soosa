package commands

import (
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
)

// ModerationCommands defines all moderation-related slash commands.
var ModerationCommands = []*discordgo.ApplicationCommand{
	{
		Name:        "kick",
		Description: "Kick a user from the server",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionUser,
				Name:        "user",
				Description: "The user to kick",
				Required:    true,
			},
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "reason",
				Description: "The reason for kicking the user",
				Required:    false,
			},
		},
	},
	{
		Name:        "ban",
		Description: "Ban a user from the server",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionUser,
				Name:        "user",
				Description: "The user to ban",
				Required:    true,
			},
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "reason",
				Description: "The reason for banning the user",
				Required:    false,
			},
		},
	},
	{
		Name:        "timeout",
		Description: "Timeout a user for a duration",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionUser,
				Name:        "user",
				Description: "The user to timeout",
				Required:    true,
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "duration",
				Description: "Duration in minutes",
				Required:    true,
			},
		},
	},
	{
		Name:        "purge",
		Description: "Delete a specified number of messages",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "amount",
				Description: "Number of messages to delete (1-100)",
				Required:    true,
				MinValue:    &minPurge,
				MaxValue:    100,
			},
		},
	},
}

var minPurge = 1.0

// HandleModerationCommand routes moderation commands to their handlers.
func HandleModerationCommand(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	switch data.Name {
	case "kick":
		handleKick(s, i, data)
	case "ban":
		handleBan(s, i, data)
	case "timeout":
		handleTimeout(s, i, data)
	case "purge":
		handlePurge(s, i, data)
	}
}

func handleKick(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	options := data.Options
	user := options[0].UserValue(s)
	reason := "No reason provided"
	if len(options) > 1 {
		reason = options[1].StringValue()
	}

	err := s.GuildMemberDeleteWithReason(i.GuildID, user.ID, reason)
	if err != nil {
		respond(s, i, fmt.Sprintf("❌ Failed to kick user: %v", err))
		return
	}

	respond(s, i, fmt.Sprintf("👢 Kicked **%s** — Reason: %s", user.Username, reason))
}

func handleBan(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	options := data.Options
	user := options[0].UserValue(s)
	reason := "No reason provided"
	if len(options) > 1 {
		reason = options[1].StringValue()
	}

	err := s.GuildBanCreateWithReason(i.GuildID, user.ID, reason, 0)
	if err != nil {
		respond(s, i, fmt.Sprintf("❌ Failed to ban user: %v", err))
		return
	}

	respond(s, i, fmt.Sprintf("🔨 Banned **%s** — Reason: %s", user.Username, reason))
}

func handleTimeout(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	options := data.Options
	user := options[0].UserValue(s)
	minutes := options[1].IntValue()

	until := time.Now().Add(time.Duration(minutes) * time.Minute)

	// Using GuildMemberTimeout helper if available, or GuildMemberEdit Assuming recent discordgo version which has GuildMemberTimeout

	err := s.GuildMemberTimeout(i.GuildID, user.ID, &until)
	if err != nil {
		respond(s, i, fmt.Sprintf("❌ Failed to timeout user: %v", err))
		return
	}

	respond(s, i, fmt.Sprintf("⏳ Timed out **%s** for %d minutes (until %s).", user.Username, minutes, until.Format("15:04")))
}

func handlePurge(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	amount := data.Options[0].IntValue()

	messages, err := s.ChannelMessages(i.ChannelID, int(amount), "", "", "")
	if err != nil {
		respond(s, i, "❌ Error fetching messages to purge.")
		return
	}

	var messageIDs []string
	for _, msg := range messages {
		messageIDs = append(messageIDs, msg.ID)
	}

	err = s.ChannelMessagesBulkDelete(i.ChannelID, messageIDs)
	if err != nil {
		respond(s, i, "❌ Error deleting messages.")
		return
	}

	respond(s, i, fmt.Sprintf("🗑️ Purged **%d** messages.", len(messages)))
}
