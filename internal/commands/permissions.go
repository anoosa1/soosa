package commands

import (
	"fmt"
	"log"

	"github.com/bwmarrin/discordgo"
)

var PermissionCommands = []*discordgo.ApplicationCommand{
	{
		Name:        "perm",
		Description: "Manage bot permissions",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "add",
				Description: "Add a permission to a user",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionUser,
						Name:        "user",
						Description: "The user to grant permission to",
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "node",
						Description: "The permission node (e.g. music.play)",
						Required:    true,
					},
				},
			},
			{
				Name:        "remove",
				Description: "Remove a permission from a user",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionUser,
						Name:        "user",
						Description: "The user to revoke permission from",
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "node",
						Description: "The permission node",
						Required:    true,
					},
				},
			},
			{
				Name:        "list",
				Description: "List permissions for a user",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionUser,
						Name:        "user",
						Description: "The user to list permissions for",
						Required:    true,
					},
				},
			},
		},
	},
}

func HandlePermissionCommand(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	// Subcommand handling
	if len(data.Options) == 0 {
		return
	}

	subcmd := data.Options[0]

	switch subcmd.Name {
	case "add":
		handlePermAdd(s, i, subcmd.Options)
	case "remove":
		handlePermRemove(s, i, subcmd.Options)
	case "list":
		handlePermList(s, i, subcmd.Options)
	}
}

func handlePermAdd(s *discordgo.Session, i *discordgo.InteractionCreate, options []*discordgo.ApplicationCommandInteractionDataOption) {
	user := options[0].UserValue(s)       // user
	inputNode := options[1].StringValue() // node

	if DB == nil {
		respondError(s, i, "Database not initialized.")
		return
	}

	nodesToAdd := resolveNodes(inputNode)
	if len(nodesToAdd) == 0 {
		respondError(s, i, fmt.Sprintf("Invalid permission node or category: `%s`", inputNode))
		return
	}

	count := 0
	for _, node := range nodesToAdd {
		err := DB.AddPermission(user.ID, node)
		if err != nil {
			log.Printf("Failed to add permission %s for user %s: %v", node, user.Username, err)
			continue
		}
		count++
	}

	if count == 0 {
		respondError(s, i, "Failed to add any permissions.")
		return
	}

	msg := fmt.Sprintf("✅ Granted **%d** permission(s) to **%s**.", count, user.Username)
	if count == 1 {
		msg = fmt.Sprintf("✅ Granted `%s` to **%s**.", nodesToAdd[0], user.Username)
	}
	respondSuccess(s, i, msg)
}

func handlePermRemove(s *discordgo.Session, i *discordgo.InteractionCreate, options []*discordgo.ApplicationCommandInteractionDataOption) {
	user := options[0].UserValue(s)
	inputNode := options[1].StringValue()

	if DB == nil {
		respondError(s, i, "Database not initialized.")
		return
	}

	nodesToRemove := resolveNodes(inputNode)
	if len(nodesToRemove) == 0 {
		respondError(s, i, fmt.Sprintf("Invalid permission node or category: `%s`", inputNode))
		return
	}

	count := 0
	for _, node := range nodesToRemove {
		err := DB.RemovePermission(user.ID, node)
		if err != nil {
			log.Printf("Failed to remove permission %s for user %s: %v", node, user.Username, err)
			continue
		}
		count++
	}

	msg := fmt.Sprintf("🗑️ Revoked **%d** permission(s) from **%s**.", count, user.Username)
	if count == 1 {
		msg = fmt.Sprintf("🗑️ Revoked `%s` from **%s**.", nodesToRemove[0], user.Username)
	}
	respondSuccess(s, i, msg)
}

func resolveNodes(input string) []string {
	// 1. Check if it's a valid single node
	if IsValidPermissionNode(input) {
		return []string{input}
	}

	// 2. Check if it's a category
	categoryNodes := GetPermissionsByCategory(input)
	if len(categoryNodes) > 0 {
		return categoryNodes
	}

	return nil
}

func handlePermList(s *discordgo.Session, i *discordgo.InteractionCreate, options []*discordgo.ApplicationCommandInteractionDataOption) {
	user := options[0].UserValue(s)

	if DB == nil {
		respondError(s, i, "Database not initialized.")
		return
	}

	nodes, err := DB.ListPermissions(user.ID)
	if err != nil {
		respondError(s, i, fmt.Sprintf("Failed to list permissions: %v", err))
		return
	}

	if len(nodes) == 0 {
		respondSuccess(s, i, fmt.Sprintf("**%s** has no explicit permissions.", user.Username))
		return
	}

	msg := fmt.Sprintf("📋 **Permissions for %s**:\n", user.Username)
	for _, n := range nodes {
		msg += fmt.Sprintf("- `%s`\n", n)
	}
	respondSuccess(s, i, msg)
}

func respondError(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "❌ " + msg,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

func respondSuccess(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: msg,
		},
	})
}
