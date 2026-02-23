package commands

import (
	"log"
	"strings"

	"soosa/internal/database"

	"github.com/bwmarrin/discordgo"
)

// CommandPermissionMap maps command names to their required permission node.
var CommandPermissionMap = map[string]string{
	// Moderation
	"kick":    "admin.kick",
	"ban":     "admin.ban",
	"timeout": "admin.timeout",
	"purge":   "admin.purge",

	// Music
	"play":       "music.play",
	"search":     "music.search",
	"queue":      "music.queue",
	"skip":       "music.skip",
	"stop":       "music.stop",
	"nowplaying": "music.nowplaying",
	"pause":      "music.pause",
	"resume":     "music.resume",
	"volume":     "music.volume",

	// Economy
	"money": "economy.money",

	// Games
	"bj":     "games.bj",
	"poker":  "games.poker",
	"wordle": "games.wordle",

	// Permissions
	"perm":        "admin.perm",
	"admin.money": "admin.money",
}

// DB instance for permission checks
var DB *database.DB
var OwnerID string

func AllCommands() []*discordgo.ApplicationCommand {
	all := make([]*discordgo.ApplicationCommand, 0, len(ModerationCommands)+len(MusicCommands)+len(EconomyCommands)+len(BlackjackCommands)+1)
	all = append(all, ModerationCommands...)
	all = append(all, MusicCommands...)
	all = append(all, EconomyCommands...)
	all = append(all, BlackjackCommands...)
	all = append(all, PokerCommands...)
	all = append(all, WordleCommands...)
	all = append(all, PermissionCommands...)
	return all
}

// RegisterCommands registers all slash commands with Discord.
func RegisterCommands(s *discordgo.Session, guildID string) {
	log.Println("Registering commands...")
	for _, cmd := range AllCommands() {
		_, err := s.ApplicationCommandCreate(s.State.User.ID, guildID, cmd)
		if err != nil {
			log.Printf("Cannot create command '%v': %v", cmd.Name, err)
		}
	}
	log.Println("Commands registered successfully!")
}

// HandleInteraction is the central dispatcher for all slash commands.
func HandleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type == discordgo.InteractionMessageComponent {
		id := i.MessageComponentData().CustomID
		if strings.HasPrefix(id, "music_") {
			HandleMusicComponent(s, i)
		} else if strings.HasPrefix(id, "game_bj_") {
			HandleGameComponent(s, i)
		} else if strings.HasPrefix(id, "game_poker_") {
			HandlePokerComponent(s, i)
		} else if strings.HasPrefix(id, "wordle_") {
			HandleWordleComponent(s, i)
		}
		return
	}

	if i.Type == discordgo.InteractionModalSubmit {
		id := i.ModalSubmitData().CustomID
		if strings.HasPrefix(id, "game_poker_") {
			HandlePokerModal(s, i)
		}
		return
	}

	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	data := i.ApplicationCommandData()

	// Permission Check
	if !hasPermission(i.Member.User.ID, data.Name) {
		log.Printf("[COMMAND DENIED] User: %s (%s) | Command: %s | Reason: Low Permissions", i.Member.User.Username, i.Member.User.ID, data.Name)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "🚫 You do not have permission to use this command.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	log.Printf("[COMMAND EXEC] User: %s (%s) | Guild: %s | Command: %s", i.Member.User.Username, i.Member.User.ID, i.GuildID, data.Name)

	switch data.Name {
	// Moderation
	case "kick", "ban", "timeout", "purge":
		HandleModerationCommand(s, i, data)
	// Music
	case "play", "search", "queue", "skip", "stop", "nowplaying", "pause", "resume", "volume":
		HandleMusicCommand(s, i, data)
	// Economy
	case "money":
		HandleEconomyCommand(s, i, data)
	// Games
	case "bj":
		HandleBlackjackCommand(s, i, data)
	case "poker":
		HandlePokerCommand(s, i, data)
	case "wordle":
		HandleWordleCommand(s, i, data)
	// Permissions
	case "perm":
		HandlePermissionCommand(s, i, data)
	}
}

func hasPermission(userID string, commandName string) bool {
	// 1. Check if user is the bot owner (server owner in this context, or configured owner) For simplicity, we'll check against the configured Guild Owner if available, but since we don't have the guild object easily here without an API call ensuring it's cached, we rely on the injected OwnerID.

	if userID == OwnerID {
		return true
	}

	// 2. Check if command requires permission
	node, exists := CommandPermissionMap[commandName]
	if !exists {
		// If no permission node is defined, decide default behavior. Requirement: "The only people who have perms by default should be people with the server ownser" So if it's not the owner, and no perm node, we block? Or if no perm node mapped, maybe it's public? The prompt says "every command should have its own permission". So we assume everything requires permission.

		return false
	}

	// 3. Check database
	if DB == nil {
		return false // Fail safe
	}

	has, err := DB.HasPermission(userID, node)
	if err != nil {
		log.Printf("Error checking permission for user %s node %s: %v", userID, node, err)
		return false
	}

	return has
}

// IsValidPermissionNode checks if a permission node exists in the map.
func IsValidPermissionNode(node string) bool {
	for _, n := range CommandPermissionMap {
		if n == node {
			return true
		}
	}
	return false
}

// GetPermissionsByCategory returns all permission nodes that start with the given category (e.g. "music"). It handles "music" -> "music.play", "music.skip", etc.

func GetPermissionsByCategory(category string) []string {
	var nodes []string
	prefix := category + "."
	for _, node := range CommandPermissionMap {
		if strings.HasPrefix(node, prefix) {
			nodes = append(nodes, node)
		}
	}
	return uniqueStrings(nodes)
}

func uniqueStrings(input []string) []string {
	u := make([]string, 0, len(input))
	m := make(map[string]bool)
	for _, val := range input {
		if !m[val] {
			m[val] = true
			u = append(u, val)
		}
	}
	return u
}
