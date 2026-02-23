package commands

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

var BlackjackCommands = []*discordgo.ApplicationCommand{
	{
		Name:        "bj",
		Description: "Play Blackjack",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "bet",
				Description: "Amount to bet (default 0)",
				Required:    false,
			},
		},
	},
}

type GameState string

const (
	StateLobby    GameState = "LOBBY"
	StatePlaying  GameState = "PLAYING"
	StateFinished GameState = "FINISHED"
)

type PlayerStatus string

const (
	StatusWaiting PlayerStatus = "WAITING"
	StatusActive  PlayerStatus = "ACTIVE"
	StatusStood   PlayerStatus = "STOOD"
	StatusBusted  PlayerStatus = "BUSTED"
)

type BJPlayer struct {
	UserID   string
	Username string
	Hand     []Card
	Bet      int
	Status   PlayerStatus
}

type BlackjackGame struct {
	MessageID   string
	ChannelID   string
	HostID      string
	Players     []*BJPlayer
	CurrentTurn int
	Deck        []Card
	Dealer      []Card
	State       GameState
	BaseBet     int
	StartTime   time.Time
	LobbyTimer  *time.Timer
}

var (
	activeGames  = make(map[string]*BlackjackGame)
	gamesMutex   sync.Mutex
	maxPlayers   = 5
	lobbyTimeout = 30 * time.Second
)

func GetBlackjackCardValue(c Card) int {
	switch c.Value {
	case "A":
		return 11
	case "K", "Q", "J":
		return 10
	default:
		var score int
		fmt.Sscan(c.Value, &score)
		return score
	}
}

func CalculateBlackjackHand(hand []Card) int {
	score := 0
	aces := 0
	for _, c := range hand {
		score += GetBlackjackCardValue(c)
		if c.Value == "A" {
			aces++
		}
	}
	for score > 21 && aces > 0 {
		score -= 10
		aces--
	}
	return score
}

func formatHand(hand []Card) string {
	if len(hand) == 0 {
		return "None"
	}
	var s []string
	for _, c := range hand {
		s = append(s, fmt.Sprintf("`%s%s`", c.Value, c.Suit))
	}
	return strings.Join(s, " ")
}

func HandleBlackjackCommand(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	if data.Name == "bj" {
		startBlackjackLobby(s, i, data)
	}
}

func startBlackjackLobby(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	bet := 0
	if len(data.Options) > 0 {
		bet = int(data.Options[0].IntValue())
	}

	if bet < 0 {
		log.Printf("[BLACKJACK ERROR] User %s: Bet cannot be negative (%d)", i.Member.User.ID, bet)
		respondError(s, i, "Bet cannot be negative.")
		return
	}

	if bet > 0 {
		bal, err := DB.GetBalance(i.Member.User.ID)
		if err != nil {
			respondError(s, i, "Database error.")
			return
		}
		if bal < bet {
			respondError(s, i, "Insufficient funds.")
			return
		}
	}

	game := &BlackjackGame{
		HostID:    i.Member.User.ID,
		State:     StateLobby,
		BaseBet:   bet,
		Players:   []*BJPlayer{{UserID: i.Member.User.ID, Username: i.Member.User.Username, Bet: bet, Status: StatusWaiting}},
		StartTime: time.Now().Add(lobbyTimeout),
	}
	log.Printf("[BLACKJACK LOBBY] Created by %s | Bet: %d", i.Member.User.Username, bet)

	embed := createLobbyEmbed(game)
	components := createLobbyButtons()

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
	if err != nil {
		return
	}

	msg, err := s.InteractionResponse(i.Interaction)
	if err != nil {
		return
	}

	game.MessageID = msg.ID
	game.ChannelID = msg.ChannelID

	gamesMutex.Lock()
	activeGames[msg.ID] = game
	gamesMutex.Unlock()

	// Start auto-start timer Start auto-start timer

	game.LobbyTimer = time.AfterFunc(lobbyTimeout, func() {
		gamesMutex.Lock()
		// Re-check existence since it might have finished/cancelled
		if g, exists := activeGames[msg.ID]; exists && g.State == StateLobby {
			startGame(s, g)
		}
		gamesMutex.Unlock()
	})
}

func createLobbyEmbed(g *BlackjackGame) *discordgo.MessageEmbed {
	var playersList []string
	for _, p := range g.Players {
		playersList = append(playersList, fmt.Sprintf("- %s", p.Username))
	}

	return &discordgo.MessageEmbed{
		Title:       "Blackjack Lobby",
		Description: fmt.Sprintf("Waiting for players... Starts <t:%d:R>\n\n**Players (%d/%d):**\n%s", g.StartTime.Unix(), len(g.Players), maxPlayers, strings.Join(playersList, "\n")),
		Color:       0x5865F2,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Bet", Value: fmt.Sprintf("$%d", g.BaseBet), Inline: true},
		},
	}
}

func createLobbyButtons() []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{Label: "Join", Style: discordgo.PrimaryButton, CustomID: "game_bj_join"},
				discordgo.Button{Label: "Start", Style: discordgo.SuccessButton, CustomID: "game_bj_start"},
			},
		},
	}
}

func createBjPlayAgainButtons(bet int) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{Label: "Play Again", Style: discordgo.SuccessButton, CustomID: fmt.Sprintf("game_bj_play_again:%d", bet)},
			},
		},
	}
}

func HandleGameComponent(s *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID

	if strings.HasPrefix(customID, "game_bj_play_again") {
		handleBjPlayAgain(s, i, customID)
		return
	}

	gamesMutex.Lock()
	game, exists := activeGames[i.Message.ID]
	gamesMutex.Unlock()

	if !exists {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: "Game not found.", Flags: discordgo.MessageFlagsEphemeral},
		})
		return
	}

	switch customID {
	case "game_bj_join":
		handleJoin(s, i, game)
	case "game_bj_start":
		handleStartButton(s, i, game)
	case "game_bj_hit":
		handleHit(s, i, game)
	case "game_bj_stand":
		handleStand(s, i, game)
	}
}

func handleBjPlayAgain(s *discordgo.Session, i *discordgo.InteractionCreate, customID string) {
	parts := strings.Split(customID, ":")
	buyin := 0
	if len(parts) > 1 {
		fmt.Sscan(parts[1], &buyin)
	}
	if buyin > 0 {
		bal, err := DB.GetBalance(i.Member.User.ID)
		if err != nil || bal < buyin {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{Content: "Insufficient funds!", Flags: discordgo.MessageFlagsEphemeral},
			})
			return
		}

		if err := DB.AddBalance(i.Member.User.ID, -buyin); err != nil {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{Content: "Transaction failed!", Flags: discordgo.MessageFlagsEphemeral},
			})
			return
		}
	}

	newGame := &BlackjackGame{
		HostID:    i.Member.User.ID,
		State:     StateLobby,
		BaseBet:   buyin,
		Players:   []*BJPlayer{{UserID: i.Member.User.ID, Username: i.Member.User.Username, Bet: buyin, Status: StatusWaiting}},
		StartTime: time.Now().Add(lobbyTimeout),
	}

	embed := createLobbyEmbed(newGame)
	components := createLobbyButtons()

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
	if err != nil {
		if buyin > 0 {
			DB.AddBalance(i.Member.User.ID, buyin)
		}
		return
	}

	msg, err := s.InteractionResponse(i.Interaction)
	if err != nil {
		return
	}

	newGame.MessageID = msg.ID
	newGame.ChannelID = msg.ChannelID

	gamesMutex.Lock()
	activeGames[msg.ID] = newGame
	gamesMutex.Unlock()

	newGame.LobbyTimer = time.AfterFunc(lobbyTimeout, func() {
		gamesMutex.Lock()
		if g, exists := activeGames[msg.ID]; exists && g.State == StateLobby {
			startGame(s, g)
		}
		gamesMutex.Unlock()
	})
}

func handleJoin(s *discordgo.Session, i *discordgo.InteractionCreate, g *BlackjackGame) {
	// Check balance first without lock to avoid blocking all games
	if g.BaseBet > 0 {
		bal, err := DB.GetBalance(i.Member.User.ID)
		if err != nil || bal < g.BaseBet {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{Content: "Insufficient funds!", Flags: discordgo.MessageFlagsEphemeral},
			})
			return
		}
	}

	gamesMutex.Lock()
	defer gamesMutex.Unlock()

	// Re-check state after lock
	if g.State != StateLobby {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: "Game already started!", Flags: discordgo.MessageFlagsEphemeral},
		})
		return
	}

	for _, p := range g.Players {
		if p.UserID == i.Member.User.ID {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{Content: "You already joined!", Flags: discordgo.MessageFlagsEphemeral},
			})
			return
		}
	}

	// Check max players
	if len(g.Players) >= maxPlayers {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: "Lobby is full!", Flags: discordgo.MessageFlagsEphemeral},
		})
		return
	}

	// Double check we haven't already joined (race condition cover)
	for _, p := range g.Players {
		if p.UserID == i.Member.User.ID {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{Content: "You already joined!", Flags: discordgo.MessageFlagsEphemeral},
			})
			return
		}
	}

	g.Players = append(g.Players, &BJPlayer{
		UserID:   i.Member.User.ID,
		Username: i.Member.User.Username,
		Bet:      g.BaseBet,
		Status:   StatusWaiting,
	})
	log.Printf("[BLACKJACK JOIN] %s joined game %s", i.Member.User.Username, g.MessageID)

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{createLobbyEmbed(g)},
			Components: createLobbyButtons(),
		},
	})
}

func handleStartButton(s *discordgo.Session, i *discordgo.InteractionCreate, g *BlackjackGame) {
	gamesMutex.Lock()
	defer gamesMutex.Unlock()

	if i.Member.User.ID != g.HostID {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: "Only the host can start early!", Flags: discordgo.MessageFlagsEphemeral},
		})
		return
	}

	if g.State != StateLobby {
		return
	}

	// Acknowledge interaction to prevent failure state
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})

	if g.LobbyTimer != nil {
		g.LobbyTimer.Stop()
	}

	startGame(s, g)
}

func startGame(s *discordgo.Session, g *BlackjackGame) {
	g.State = StatePlaying
	g.Deck = NewDeck()
	if len(g.Deck) < 10 { // Safety check, though NewDeck returns 52
		log.Println("Error: Deck generation failed")
		return
	}
	g.Dealer = []Card{g.Deck[0], g.Deck[1]}
	g.Deck = g.Deck[2:]

	// Deduct bets and deal initial cards
	for _, p := range g.Players {
		if g.BaseBet > 0 {
			DB.AddBalance(p.UserID, -g.BaseBet)
		}
		p.Hand = []Card{g.Deck[0], g.Deck[1]}
		g.Deck = g.Deck[2:]
		p.Status = StatusWaiting
	}

	g.CurrentTurn = 0
	g.Players[0].Status = StatusActive

	updateGameMessage(s, g, nil)
	log.Printf("[BLACKJACK START] Game %s started with %d players", g.MessageID, len(g.Players))
}

func updateGameMessage(s *discordgo.Session, g *BlackjackGame, i *discordgo.InteractionCreate) {
	embed := &discordgo.MessageEmbed{
		Title: "Blackjack",
		Color: 0x00FF00,
	}

	dealerHandStr := fmt.Sprintf("`%s%s` `??`", g.Dealer[0].Value, g.Dealer[0].Suit)
	if g.State == StateFinished {
		dealerHandStr = fmt.Sprintf("(%d) %s", CalculateBlackjackHand(g.Dealer), formatHand(g.Dealer))
	}

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:  "Dealer's Hand",
		Value: dealerHandStr,
	})

	for _, p := range g.Players {
		status := ""
		switch p.Status {
		case StatusActive:
			status = "⬅️ **TURN**"
		case StatusBusted:
			status = "💥 **BUST**"
		case StatusStood:
			status = "⏹️ **STOOD**"
		}

		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("%s's Hand (%d) %s", p.Username, CalculateBlackjackHand(p.Hand), status),
			Value:  formatHand(p.Hand),
			Inline: false,
		})
	}

	var components []discordgo.MessageComponent
	if g.State == StatePlaying {
		components = []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{Label: "Hit", Style: discordgo.PrimaryButton, CustomID: "game_bj_hit"},
					discordgo.Button{Label: "Stand", Style: discordgo.SecondaryButton, CustomID: "game_bj_stand"},
				},
			},
		}
	}

	if i != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Embeds:     []*discordgo.MessageEmbed{embed},
				Components: components,
			},
		})
	} else {
		embeds := []*discordgo.MessageEmbed{embed}
		s.ChannelMessageEditComplex(&discordgo.MessageEdit{
			ID:         g.MessageID,
			Channel:    g.ChannelID,
			Embeds:     &embeds,
			Components: &components,
		})
	}
}

func handleHit(s *discordgo.Session, i *discordgo.InteractionCreate, g *BlackjackGame) {
	gamesMutex.Lock()
	defer gamesMutex.Unlock()

	if g.State != StatePlaying {
		return
	}

	activePlayer := g.Players[g.CurrentTurn]
	if i.Member.User.ID != activePlayer.UserID {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: "It's not your turn!", Flags: discordgo.MessageFlagsEphemeral},
		})
		return
	}

	activePlayer.Hand = append(activePlayer.Hand, g.Deck[0])
	g.Deck = g.Deck[1:]

	if CalculateBlackjackHand(activePlayer.Hand) > 21 {
		activePlayer.Status = StatusBusted
		nextTurn(s, g, i)
	} else {
		updateGameMessage(s, g, i)
	}
}

func handleStand(s *discordgo.Session, i *discordgo.InteractionCreate, g *BlackjackGame) {
	gamesMutex.Lock()
	defer gamesMutex.Unlock()

	if g.State != StatePlaying {
		return
	}

	activePlayer := g.Players[g.CurrentTurn]
	if i.Member.User.ID != activePlayer.UserID {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: "It's not your turn!", Flags: discordgo.MessageFlagsEphemeral},
		})
		return
	}

	activePlayer.Status = StatusStood
	log.Printf("[BLACKJACK ACTION] %s STOOD | Hand Value: %d", i.Member.User.Username, CalculateBlackjackHand(activePlayer.Hand))
	nextTurn(s, g, i)
}

func nextTurn(s *discordgo.Session, g *BlackjackGame, i *discordgo.InteractionCreate) {
	g.CurrentTurn++
	if g.CurrentTurn >= len(g.Players) {
		finishGame(s, g, i)
	} else {
		g.Players[g.CurrentTurn].Status = StatusActive
		updateGameMessage(s, g, i)
	}
}

func finishGame(s *discordgo.Session, g *BlackjackGame, i *discordgo.InteractionCreate) {
	g.State = StateFinished

	// Dealer play
	dScore := CalculateBlackjackHand(g.Dealer)
	for dScore < 17 {
		g.Dealer = append(g.Dealer, g.Deck[0])
		g.Deck = g.Deck[1:]
		dScore = CalculateBlackjackHand(g.Dealer)
	}

	// Calculate results and payouts
	resultsSummary := ""
	for _, p := range g.Players {
		pScore := CalculateBlackjackHand(p.Hand)
		win := false
		push := false

		if p.Status == StatusBusted {
			resultsSummary += fmt.Sprintf("\n- %s: Busted ❌", p.Username)
		} else if dScore > 21 {
			resultsSummary += fmt.Sprintf("\n- %s: Won (Dealer Bust) 🎉", p.Username)
			win = true
		} else if pScore > dScore {
			resultsSummary += fmt.Sprintf("\n- %s: Won 🎉", p.Username)
			win = true
		} else if pScore < dScore {
			resultsSummary += fmt.Sprintf("\n- %s: Lost ❌", p.Username)
		} else {
			resultsSummary += fmt.Sprintf("\n- %s: Push 🤝", p.Username)
			push = true
		}

		if g.BaseBet > 0 {
			if win {
				DB.AddBalance(p.UserID, g.BaseBet*2)
			} else if push {
				DB.AddBalance(p.UserID, g.BaseBet)
			}
		}

	}
	log.Printf("[BLACKJACK FINISH] Game %s | Dealer: %d | Results: %s", g.MessageID, dScore, resultsSummary)

	embed := &discordgo.MessageEmbed{
		Title:       "Blackjack - Results",
		Description: "**Dealer Finished!**" + resultsSummary,
		Color:       0x00FF00,
	}

	dealerHandStr := fmt.Sprintf("(%d) %s", dScore, formatHand(g.Dealer))
	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:  "Dealer's Hand",
		Value: dealerHandStr,
	})

	for _, p := range g.Players {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("%s's Hand (%d)", p.Username, CalculateBlackjackHand(p.Hand)),
			Value:  formatHand(p.Hand),
			Inline: true,
		})
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: createBjPlayAgainButtons(g.BaseBet),
		},
	})

	// Cleanup button
	go func(mID, cID string) {
		time.Sleep(10 * time.Second)
		s.ChannelMessageEditComplex(&discordgo.MessageEdit{
			ID:         mID,
			Channel:    cID,
			Components: &[]discordgo.MessageComponent{},
		})
	}(g.MessageID, g.ChannelID)

	delete(activeGames, g.MessageID)
}
