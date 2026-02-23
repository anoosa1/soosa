package commands

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	"soosa/internal/database"

	"github.com/bwmarrin/discordgo"
)

var (
	answers []string
	allowed []string
)

const (
	StatusGray   = 0
	StatusYellow = 1
	StatusGreen  = 2
)

// LoadWordleWords loads the answer and allowed word lists from the specified files.
func LoadWordleWords(answersPath, allowedPath string) error {
	var err error
	answers, err = readLines(answersPath)
	if err != nil {
		return fmt.Errorf("failed to load answers from %s: %w", answersPath, err)
	}

	allowed, err = readLines(allowedPath)
	if err != nil {
		return fmt.Errorf("failed to load allowed guesses from %s: %w", allowedPath, err)
	}

	// Append answers to allowed list to ensure all answers are valid guesses if not already present
	allowed = append(allowed, answers...)
	return nil
}

func readLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, strings.ToUpper(line))
		}
	}
	return lines, scanner.Err()
}

// EvaluateGuess compares the guess with the target word and returns an array of statuses. 2 = Green (Correct position), 1 = Yellow (Wrong position), 0 = Gray (Not in word)

func EvaluateGuess(guess, target string) []int {
	guess = strings.ToUpper(guess)
	target = strings.ToUpper(target)
	result := make([]int, 5)

	targetRunes := []rune(target)
	guessRunes := []rune(guess)

	targetFreq := make(map[rune]int)

	// First pass: Check for Greens and build frequency map for non-green target letters
	for i, char := range targetRunes {
		if guessRunes[i] == char {
			result[i] = StatusGreen
		} else {
			targetFreq[char]++
		}
	}

	// Second pass: Check for Yellows
	for i, char := range guessRunes {
		if result[i] == StatusGreen {
			continue
		}

		if count, exists := targetFreq[char]; exists && count > 0 {
			result[i] = StatusYellow
			targetFreq[char]--
		} else {
			result[i] = StatusGray
		}
	}

	return result
}

// GetDailyWord returns the daily word based on the current date (UTC). Using a SHA256 hash of the date to seed a random generator ensures deterministic but random selection from the answers list.

func GetDailyWord(t time.Time) string {
	if len(answers) == 0 {
		return "ERROR"
	}
	dateStr := t.UTC().Format("2006-01-02")
	hash := sha256.Sum256([]byte(dateStr))
	seed := int64(binary.BigEndian.Uint64(hash[:8]))

	rnd := rand.New(rand.NewSource(seed))
	index := rnd.Intn(len(answers))
	return answers[index]
}

// GetRandomWord returns a random word from the answers list.
func GetRandomWord() string {
	if len(answers) == 0 {
		return "ERROR"
	}
	return answers[rand.Intn(len(answers))]
}

// IsValidWord checks if the word is in the allowed list (or answers list).
func IsValidWord(word string) bool {
	if len(allowed) == 0 {
		return false
	}
	word = strings.ToUpper(word)
	for _, w := range allowed {
		if w == word {
			return true
		}
	}
	return false
}

// CalculateWordleReward returns the reward amount based on the number of attempts (1-indexed).
func CalculateWordleReward(attempts int) int {
	if attempts < 1 {
		return 0
	}
	if attempts > 6 {
		return 0 // Should be handled by caller, but safety check.
	}
	// 1st: 500, 2nd: 400, ..., 5th: 100, 6th: 50 (Custom rule)
	if attempts == 6 {
		return 50
	}
	return 500 - (attempts-1)*100
}

var WordleCommands = []*discordgo.ApplicationCommand{
	{
		Name:        "wordle",
		Description: "Wordle game commands",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "guess",
				Description: "Make a guess or start a game",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "word",
						Description: "Make a guess immediately",
						Required:    false,
					},
				},
			},
			{
				Name:        "giveup",
				Description: "Forfeit the current game",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
		},
	},
}

// HandleWordleCommand handles the /wordle slash command interactions
func HandleWordleCommand(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	if len(data.Options) == 0 {
		return
	}

	switch data.Options[0].Name {
	case "guess":
		var word string
		if len(data.Options[0].Options) > 0 {
			word = data.Options[0].Options[0].StringValue()
		}
		handleGuessCommand(s, i, word)
	case "giveup":
		handleGiveUpCommand(s, i)
	}
}

func handleGuessCommand(s *discordgo.Session, i *discordgo.InteractionCreate, guessArg string) {
	userID := i.Member.User.ID

	// Check if user has an active game
	state, err := DB.GetWordleState(userID)
	if err != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: "Error retrieving game state.", Flags: discordgo.MessageFlagsEphemeral},
		})
		return
	}

	// Active game found
	if state != nil && !state.Completed {
		// Resume game
		if guessArg != "" {
			handleGuess(s, i, state, guessArg)
		} else {
			respondWithGame(s, i.Interaction, state)
		}
		return
	}

	// No active game, check if Daily played today
	today := time.Now().UTC().Format("2006-01-02")
	if state != nil && state.Completed {
		if state.GameType == "DAILY" && state.LastPlayed == today {
			// Daily finished today. Offer Random.
			respondWithStatsAndRandomOffer(s, i.Interaction, userID)
			return
		}
	}

	// Start new Daily Game
	word := GetDailyWord(time.Now().UTC())
	newState := &database.WordleState{
		UserID:     userID,
		GameType:   "DAILY",
		Word:       word,
		Guesses:    []string{},
		LastPlayed: today,
		Completed:  false,
	}
	log.Printf("[WORDLE START] User: %s | Mode: DAILY | Word: %s", userID, word)

	// Send Public Message
	publicEmbed := buildPublicEmbed(newState)
	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Show Guesses",
					Style:    discordgo.SecondaryButton,
					CustomID: "wordle_show_guesses",
				},
			},
		},
	}

	msg, err := s.ChannelMessageSendComplex(i.ChannelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{publicEmbed},
		Components: components,
	})
	if err != nil {
		// Fallback if permission error, though unlikely if command worked
		fmt.Println("Error sending public wordle message:", err)
	} else {
		newState.MessageID = msg.ID
		newState.ChannelID = msg.ChannelID
	}

	if err := DB.SaveWordleState(newState); err != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: "Failed to start game.", Flags: discordgo.MessageFlagsEphemeral},
		})
		return
	}

	// Check if a guess was provided
	if guessArg != "" {
		handleGuess(s, i, newState, guessArg)
	} else {
		respondWithGame(s, i.Interaction, newState)
	}
}

func handleGiveUpCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Defer to allow unified handling in finishWordleGame
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})

	userID := i.Member.User.ID
	state, _ := DB.GetWordleState(userID)

	if state == nil || state.Completed {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: "You don't have an active game to give up.", Flags: discordgo.MessageFlagsEphemeral},
		})
		return
	}

	state.Completed = true
	DB.SaveWordleState(state)
	finishWordleGame(s, i, state, false)
}

func respondWithGame(s *discordgo.Session, interaction *discordgo.Interaction, state *database.WordleState) {
	// If message ID is missing (legacy game), try to send a new public one
	if state.MessageID == "" {
		publicEmbed := buildPublicEmbed(state)
		components := []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label:    "Show Guesses",
						Style:    discordgo.SecondaryButton,
						CustomID: "wordle_show_guesses",
					},
				},
			},
		}
		msg, err := s.ChannelMessageSendComplex(interaction.ChannelID, &discordgo.MessageSend{
			Embeds:     []*discordgo.MessageEmbed{publicEmbed},
			Components: components,
		})
		if err == nil {
			state.MessageID = msg.ID
			state.ChannelID = msg.ChannelID
			DB.SaveWordleState(state)
		}
	}

	s.InteractionRespond(interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{buildPrivateEmbed(state)},
			Flags:  discordgo.MessageFlagsEphemeral,
		},
	})
}

func respondWithStatsAndRandomOffer(s *discordgo.Session, interaction *discordgo.Interaction, userID string) {
	stats, _ := DB.GetWordleStats(userID)
	embed := &discordgo.MessageEmbed{
		Title: "Wordle - Daily Completed",
		Color: 0x00FF00,
		Description: fmt.Sprintf("You have already completed the Daily Wordle for today.\n\n**Stats:**\nPlayed: %d | Won: %d | Streak: %d | Max Streak: %d",
			stats.GamesPlayed, stats.GamesWon, stats.CurrentStreak, stats.MaxStreak),
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Play Random Wordle",
					Style:    discordgo.SecondaryButton,
					CustomID: "wordle_start_random",
				},
			},
		},
	}

	s.InteractionRespond(interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func HandleWordleComponent(s *discordgo.Session, i *discordgo.InteractionCreate) {
	id := i.MessageComponentData().CustomID
	userID := i.Member.User.ID

	if id == "wordle_start_random" {
		startRandomGame(s, i, userID)
		return
	}

	if id == "wordle_show_guesses" {
		// Only the game owner can see their guesses We need to fetch the state. But wait, we don't know who the game owner is from the button ID alone easily unless we encode it, OR we just fetch the *clicker's* active game. The requirement is "send a message that only the user can see with the words and the guess". Assuming "the user" refers to the game owner. If I click "Show Guesses" on SOMEONE ELSE'S game, I probably shouldn't see THEIR guesses? Or does "the user" mean the person attempting to view? Given the context of cheating/spoilers, likely only the owner should see. However, looking up the "clicker's" game is the safest bet. If I click it, I see MY game. But the button is attached to A SPECIFIC message. If I click the button on User A's game, but I am User B, what should happen? Ideally: "This is User A's game." or generic failure. But simpler: just show User B's current game if they have one? No, that's confusing. The prompt implies the button is context-sensitive to the game being displayed. But `WordleState` is keyed by UserID. We CAN find the game by MessageID! But we don't have a `GetWordleStateByMessageID`. Let's rely on the fact that usually only the player interacts. But to be robust: let's stick to looking up the CLICKER'S game for now, OR better, we can assume the user clicking is likely the owner. Actually, if we just use `GetWordleState(userID)`, it returns the clicker's game. If I click the button on someone else's game, and I have my OWN game, I'll see MY game. That's... acceptable? But if I don't have a game, I see nothing. Ideally we'd match the game to the message. Let's add a quick check: does the clicker have a game?

		state, err := DB.GetWordleState(userID)
		if err != nil || state == nil || state.Completed {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{Content: "You don't have an active game.", Flags: discordgo.MessageFlagsEphemeral},
			})
			return
		}

		// Optional: Verify this game belongs to this message? if state.MessageID != i.Message.ID { ... } But `i.Message` might be partial. Let's just show their game.

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{buildPrivateEmbed(state)},
				Flags:  discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}
}

func handleGuess(s *discordgo.Session, i *discordgo.InteractionCreate, state *database.WordleState, guess string) {
	// Defer response immediately to prevent timeout and allow for silent completion
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})

	guess = strings.ToUpper(guess)
	if !IsValidWord(guess) {
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: &[]string{"Not a valid word!"}[0],
		})
		return
	}

	// Update State
	state.Guesses = append(state.Guesses, guess)

	log.Printf("[WORDLE GUESS] User: %s | Guess: %s | Target: %s", i.Member.User.ID, guess, state.Word)

	// Check Win
	won := guess == state.Word
	lost := len(state.Guesses) >= 6 && !won

	if won || lost {
		state.Completed = true
	}

	DB.SaveWordleState(state)

	// Update Public Message
	if state.MessageID != "" && state.ChannelID != "" {
		publicEmbed := buildPublicEmbed(state)

		components := []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label:    "Show Guesses",
						Style:    discordgo.SecondaryButton,
						CustomID: "wordle_show_guesses",
					},
				},
			},
		}

		// Use Complex Edit to include components
		_, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
			Channel:    state.ChannelID,
			ID:         state.MessageID,
			Embed:      publicEmbed,
			Components: &components,
		})
		if err != nil {
			fmt.Println("Error editing public wordle message:", err)
		}
	}

	if won || lost {
		finishWordleGame(s, i, state, won)
	} else {
		// Silent success - delete the deferred response
		s.InteractionResponseDelete(i.Interaction)
	}
}

func startRandomGame(s *discordgo.Session, i *discordgo.InteractionCreate, userID string) {
	word := GetRandomWord()
	newState := &database.WordleState{
		UserID:     userID,
		GameType:   "RANDOM",
		Word:       word,
		Guesses:    []string{},
		LastPlayed: time.Now().UTC().Format("2006-01-02"), // Not critical for random but good for keeping track
		Completed:  false,
	}
	log.Printf("[WORDLE START] User: %s | Mode: RANDOM | Word: %s", userID, word)

	// Ack the button click silently
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})

	// Send Public Message
	publicEmbed := buildPublicEmbed(newState)
	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Show Guesses",
					Style:    discordgo.SecondaryButton,
					CustomID: "wordle_show_guesses",
				},
			},
		},
	}

	msg, err := s.ChannelMessageSendComplex(i.ChannelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{publicEmbed},
		Components: components,
	})

	if err != nil {
		fmt.Println("Error sending public wordle message:", err)
	} else {
		newState.MessageID = msg.ID
		newState.ChannelID = msg.ChannelID
	}

	if err := DB.SaveWordleState(newState); err != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: "Failed to start game.", Flags: discordgo.MessageFlagsEphemeral},
		})
		return
	}
}

func finishWordleGame(s *discordgo.Session, i *discordgo.InteractionCreate, state *database.WordleState, won bool) {
	reward := 0
	if won {
		reward = CalculateWordleReward(len(state.Guesses))
		DB.AddBalance(state.UserID, reward)
	}
	log.Printf("[WORDLE FINISH] User: %s | Won: %v | Guesses: %d | Reward: %d", state.UserID, won, len(state.Guesses), reward)

	// Update Stats
	stats, _ := DB.GetWordleStats(state.UserID)
	if stats == nil {
		stats = &database.WordleStats{UserID: state.UserID, Distribution: make(map[int]int)}
	}
	if stats.Distribution == nil {
		stats.Distribution = make(map[int]int)
	}
	stats.GamesPlayed++
	if won {
		stats.GamesWon++
		stats.CurrentStreak++
		if stats.CurrentStreak > stats.MaxStreak {
			stats.MaxStreak = stats.CurrentStreak
		}
		stats.Distribution[len(state.Guesses)]++
	} else {
		stats.CurrentStreak = 0
	}
	DB.UpdateWordleStats(stats)

	// Update Public Message
	if state.MessageID != "" && state.ChannelID != "" {
		publicEmbed := buildPublicEmbed(state) // Squares only
		publicEmbed.Title = "Wordle - Finished"
		if won {
			publicEmbed.Color = 0x00FF00
			publicEmbed.Description += fmt.Sprintf("\n\nUser <@%s> Won! \nMoney Earned: **$%d**", state.UserID, reward)
		} else {
			publicEmbed.Color = 0xFF0000
			publicEmbed.Description += fmt.Sprintf("\n\nUser <@%s> Lost.", state.UserID)
		}

		// Remove buttons by passing empty components
		s.ChannelMessageEditComplex(&discordgo.MessageEdit{
			Channel:    state.ChannelID,
			ID:         state.MessageID,
			Embed:      publicEmbed,
			Components: &[]discordgo.MessageComponent{},
		})
	}

	// Ephemeral Response to User
	privateEmbed := buildPrivateEmbed(state)
	privateEmbed.Title = "Wordle - Finished"

	var statusMsg string
	if won {
		privateEmbed.Color = 0x00FF00
		statusMsg = fmt.Sprintf("You Won! The word was **%s**.\nReward: **$%d**\n\n", state.Word, reward)
	} else {
		privateEmbed.Color = 0xFF0000
		statusMsg = fmt.Sprintf("Game Over. The word was **%s**.\n\n", state.Word)
	}
	// Prepend status message to the existing grid description
	privateEmbed.Description = statusMsg + privateEmbed.Description

	// Since both handleGuess and handleGiveUpCommand deferred, we can use Edit
	if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{privateEmbed},
	}); err != nil {
		fmt.Println("Error editing Wordle finish response:", err)
		// Fallback to sending if edit fails (e.g. if defer didn't happen - shouldn't happen)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{privateEmbed},
				Flags:  discordgo.MessageFlagsEphemeral,
			},
		})
	}
}

func buildPrivateEmbed(state *database.WordleState) *discordgo.MessageEmbed {
	var sb strings.Builder
	for _, guess := range state.Guesses {
		eval := EvaluateGuess(guess, state.Word)
		for _, status := range eval {
			switch status {
			case StatusGreen:
				sb.WriteString("🟩 ")
			case StatusYellow:
				sb.WriteString("🟨 ")
			default:
				sb.WriteString("⬛ ")
			}
		}
		sb.WriteString("  " + guess + "\n")
	}

	// Fill remaining rows
	for i := len(state.Guesses); i < 6; i++ {
		sb.WriteString("⬜ ⬜ ⬜ ⬜ ⬜\n")
	}

	title := "Wordle - Daily"
	if state.GameType == "RANDOM" {
		title = "Wordle - Random"
	}

	return &discordgo.MessageEmbed{
		Title:       title,
		Description: sb.String(),
		Color:       0x3498db,
		Footer:      &discordgo.MessageEmbedFooter{Text: fmt.Sprintf("Attempt %d/6", len(state.Guesses))},
	}
}

// buildPublicEmbed creates an embed with only squares, no letters.
func buildPublicEmbed(state *database.WordleState) *discordgo.MessageEmbed {
	var sb strings.Builder
	for _, guess := range state.Guesses {
		eval := EvaluateGuess(guess, state.Word)
		for _, status := range eval {
			switch status {
			case StatusGreen:
				sb.WriteString("🟩 ")
			case StatusYellow:
				sb.WriteString("🟨 ")
			default:
				sb.WriteString("⬛ ")
			}
		}
		sb.WriteString("\n") // No letters
	}

	// Fill remaining rows
	for i := len(state.Guesses); i < 6; i++ {
		sb.WriteString("⬜ ⬜ ⬜ ⬜ ⬜\n")
	}

	title := "Wordle - Daily"
	if state.GameType == "RANDOM" {
		title = "Wordle - Random"
	}

	description := fmt.Sprintf("<@%s>\n\n%s", state.UserID, sb.String())

	return &discordgo.MessageEmbed{
		Title:       title,
		Description: description,
		Color:       0x3498db,
		Footer:      &discordgo.MessageEmbedFooter{Text: fmt.Sprintf("Attempt %d/6", len(state.Guesses))},
	}
}
