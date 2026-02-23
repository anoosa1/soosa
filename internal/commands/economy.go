package commands

import (
	"fmt"
	"log"

	"github.com/bwmarrin/discordgo"
)

var EconomyCommands = []*discordgo.ApplicationCommand{
	{
		Name:        "money",
		Description: "Economy commands",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "balance",
				Description: "Check balance",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionUser,
						Name:        "user",
						Description: "User to check balance of",
						Required:    false,
					},
				},
			},
			{
				Name:        "send",
				Description: "Send money to another user",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionUser,
						Name:        "user",
						Description: "User to send money to",
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "amount",
						Description: "Amount to send",
						Required:    true,
					},
				},
			},
			{
				Name:        "add",
				Description: "Add money to a user (Admin only)",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionUser,
						Name:        "user",
						Description: "User to add money to",
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "amount",
						Description: "Amount to add",
						Required:    true,
					},
				},
			},
		},
	},
}

func HandleEconomyCommand(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	if len(data.Options) == 0 {
		// Default to balance
		handleBalance(s, i, nil, data)
		return
	}

	subcmd := data.Options[0]
	switch subcmd.Name {
	case "balance":
		handleBalance(s, i, subcmd.Options, data)
	case "send":
		handleSend(s, i, subcmd.Options)
	case "add":
		handleAddMoney(s, i, subcmd.Options)
	}
}

func handleBalance(s *discordgo.Session, i *discordgo.InteractionCreate, options []*discordgo.ApplicationCommandInteractionDataOption, data discordgo.ApplicationCommandInteractionData) {
	var user *discordgo.User
	if len(options) > 0 {
		user = options[0].UserValue(s)
	} else {
		user = i.Member.User
	}

	bal, err := DB.GetBalance(user.ID)
	if err != nil {
		respondError(s, i, "Failed to fetch balance.")
		return
	}

	respondSuccess(s, i, fmt.Sprintf("💰 **%s** has **$%d**", user.Username, bal))
}

func handleSend(s *discordgo.Session, i *discordgo.InteractionCreate, options []*discordgo.ApplicationCommandInteractionDataOption) {
	toUser := options[0].UserValue(s)
	amount := int(options[1].IntValue())

	if amount <= 0 {
		respondError(s, i, "Amount must be positive.")
		return
	}

	if toUser.ID == i.Member.User.ID {
		respondError(s, i, "You cannot send money to yourself.")
		return
	}

	if toUser.Bot {
		respondError(s, i, "You cannot send money to bots.")
		return
	}

	err := DB.Transfer(i.Member.User.ID, toUser.ID, amount)
	if err != nil {
		if err.Error() == "insufficient funds" {
			respondError(s, i, "Insufficient funds.")
		} else {
			log.Printf("Transfer error: %v", err)
			respondError(s, i, "Transaction failed.")
		}
		return
	}

	respondSuccess(s, i, fmt.Sprintf("💸 Sent **$%d** to **%s**.", amount, toUser.Username))
}

func handleAddMoney(s *discordgo.Session, i *discordgo.InteractionCreate, options []*discordgo.ApplicationCommandInteractionDataOption) {
	// Check admin permission logic (or rely on registry routing, but let's double check basic admin) Actually registry handles general "money" perm, but "add" might need "admin.money". Implementation plan said: checks `admin.money`

	if !hasPermission(i.Member.User.ID, "admin.money") {
		respondError(s, i, "You do not have permission to use this command.")
		return
	}

	targetUser := options[0].UserValue(s)
	amount := int(options[1].IntValue())

	err := DB.AddBalance(targetUser.ID, amount)
	if err != nil {
		log.Printf("AddBalance error: %v", err)
		respondError(s, i, "Failed to add money.")
		return
	}

	respondSuccess(s, i, fmt.Sprintf("✅ Added **$%d** to **%s**.", amount, targetUser.Username))
}
