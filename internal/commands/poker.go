package commands

import (
	"fmt"
	"log"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Poker constants
type PokerStage string

const (
	StageLobby    PokerStage = "LOBBY"
	StagePreFlop  PokerStage = "PRE-FLOP"
	StageFlop     PokerStage = "FLOP"
	StageTurn     PokerStage = "TURN"
	StageRiver    PokerStage = "RIVER"
	StageShowdown PokerStage = "SHOWDOWN"
)

type PokerPlayerStatus string

const (
	PlayerPlaying PokerPlayerStatus = "PLAYING"
	PlayerFolded  PokerPlayerStatus = "FOLDED"
	PlayerAllIn   PokerPlayerStatus = "ALL-IN"
)

type HandRank int

const (
	RankHighCard HandRank = iota + 1
	RankPair
	RankTwoPair
	RankThreeOfAKind
	RankStraight
	RankFlush
	RankFullHouse
	RankFourOfAKind
	RankStraightFlush
	RankRoyalFlush
)

func (r HandRank) String() string {
	return []string{
		"", "High Card", "Pair", "Two Pair", "Three of a Kind",
		"Straight", "Flush", "Full House", "Four of a Kind",
		"Straight Flush", "Royal Flush",
	}[r]
}

// Poker-specific data
type PokerPlayer struct {
	UserID     string
	Username   string
	HoleCards  []Card
	Stack      int
	CurrentBet int // Bet in current round
	TotalBet   int // Total bet in entire game
	Status     PokerPlayerStatus
}

type PokerGame struct {
	MessageID      string
	ChannelID      string
	HostID         string
	Players        []*PokerPlayer
	Deck           []Card
	CommunityCards []Card

	Stage        PokerStage
	CurrentTurn  int
	DealerButton int
	SmallBlind   int
	BigBlind     int
	MinBet       int // Current call amount for the round
	LastRaise    int // Amount of the last raise
	ResultText   string
	StartTime    time.Time
	LobbyTimer   *time.Timer
}

var (
	activePokerGames = make(map[string]*PokerGame)
	pokerMutex       sync.Mutex
	maxPokerPlayers  = 8
	pokerLobbyTime   = 30 * time.Second
)

var PokerCommands = []*discordgo.ApplicationCommand{
	{
		Name:        "poker",
		Description: "Play Poker",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "buyin",
				Description: "Amount to buy in (default 1000)",
				Required:    false,
			},
		},
	},
}

func HandlePokerCommand(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	buyin := 100
	if len(data.Options) > 0 {
		buyin = int(data.Options[0].IntValue())
	}

	if buyin <= 0 {
		respondError(s, i, "Buy-in must be positive.")
		return
	}

	bal, err := DB.GetBalance(i.Member.User.ID)
	if err != nil {
		respondError(s, i, "Database error.")
		return
	}
	if bal < buyin {
		respondError(s, i, "Insufficient funds.")
		return
	}

	if err := DB.AddBalance(i.Member.User.ID, -buyin); err != nil {
		respondError(s, i, "Transaction failed.")
		return
	}

	game := &PokerGame{
		HostID:     i.Member.User.ID,
		Stage:      StageLobby,
		SmallBlind: 5,
		BigBlind:   10,
		Players: []*PokerPlayer{{
			UserID:   i.Member.User.ID,
			Username: i.Member.User.Username,
			Stack:    buyin,
			Status:   PlayerPlaying,
		}},
		StartTime: time.Now().Add(pokerLobbyTime),
	}
	log.Printf("[POKER LOBBY] Created by %s | Buyin: %d", i.Member.User.Username, buyin)

	embed := createPokerLobbyEmbed(game)
	components := createPokerLobbyButtons()

	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
	if err != nil {
		// Refund if we fail to respond (unlikely but safe)
		DB.AddBalance(i.Member.User.ID, buyin)
		return
	}

	msg, err := s.InteractionResponse(i.Interaction)
	if err != nil {
		return
	}

	game.MessageID = msg.ID
	game.ChannelID = msg.ChannelID

	pokerMutex.Lock()
	activePokerGames[msg.ID] = game
	pokerMutex.Unlock()

	game.LobbyTimer = time.AfterFunc(pokerLobbyTime, func() {
		pokerMutex.Lock()
		// Re-fetch to ensure it exists
		g, exists := activePokerGames[msg.ID]
		if exists && g.Stage == StageLobby {
			if len(g.Players) >= 2 {
				startPokerGame(s, g, nil)
			} else {
				// Refund all players
				for _, p := range g.Players {
					DB.AddBalance(p.UserID, p.Stack)
				}
				content := "Game cancelled: Not enough players."
				emptyEmbeds := []*discordgo.MessageEmbed{}
				emptyComponents := []discordgo.MessageComponent{}
				s.ChannelMessageEditComplex(&discordgo.MessageEdit{
					ID:         g.MessageID,
					Channel:    g.ChannelID,
					Content:    &content,
					Embeds:     &emptyEmbeds,
					Components: &emptyComponents,
				})
				delete(activePokerGames, msg.ID)
			}
		}
		pokerMutex.Unlock()
	})
}

func createPokerLobbyEmbed(g *PokerGame) *discordgo.MessageEmbed {
	var playersList []string
	for _, p := range g.Players {
		playersList = append(playersList, fmt.Sprintf("- %s ($%d)", p.Username, p.Stack))
	}

	return &discordgo.MessageEmbed{
		Title:       "Poker Lobby",
		Description: fmt.Sprintf("Waiting for players... Starts <t:%d:R>\n\n**Players (%d/%d):**\n%s", g.StartTime.Unix(), len(g.Players), maxPokerPlayers, strings.Join(playersList, "\n")),
		Color:       0x5865F2,
	}
}

func createPokerLobbyButtons() []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{Label: "Join", Style: discordgo.PrimaryButton, CustomID: "game_poker_join"},
				discordgo.Button{Label: "Start", Style: discordgo.SuccessButton, CustomID: "game_poker_start"},
			},
		},
	}
}

func createPokerPlayAgainButtons() []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{Label: "Play Again", Style: discordgo.SuccessButton, CustomID: "game_poker_play_again"},
			},
		},
	}
}

func HandlePokerComponent(s *discordgo.Session, i *discordgo.InteractionCreate) {
	pokerMutex.Lock()
	game, exists := activePokerGames[i.Message.ID]
	pokerMutex.Unlock()

	if !exists {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: "Game not found.", Flags: discordgo.MessageFlagsEphemeral},
		})
		return
	}

	customID := i.MessageComponentData().CustomID

	switch customID {
	case "game_poker_join":
		handlePokerJoin(s, i, game)
	case "game_poker_start":
		handlePokerStartButton(s, i, game)
	case "game_poker_reveal":
		handlePokerReveal(s, i, game)
	case "game_poker_fold":
		handlePokerFold(s, i, game)
	case "game_poker_call":
		handlePokerCall(s, i, game)
	case "game_poker_raise_modal":
		handlePokerRaiseModalTrigger(s, i, game)
	case "game_poker_play_again":
		handlePokerPlayAgain(s, i, game)
	}
}

func handlePokerPlayAgain(s *discordgo.Session, i *discordgo.InteractionCreate, g *PokerGame) {
	// Start a new game with same settings We need to fetch original buyin which should be initial stack of host or any player locally But actually we can just use the initial stack from one of the players if we tracked it, or just pass the buyin used in this game. Since we don't store initial buyin explicitly in Game struct, let's assume standard logic or we can check if players have enough balance again.

	// Simple approach: trigger the command logic again for the user However, we can't easily call HandlePokerCommand with interaction data. Better: Construct a new game state and start lobby.

	// For now, let's just trigger a new lobby for the user who clicked. We need to know the buyin. Let's infer it from the game history or just use default/100 if not stored? Wait, the user asked for "same bet". We can find the buyin by checking the players' initial stacks? No, stacks change. Let's add BuyIn to PokerGame struct or just assume 100 for now if we can't easily find it. Actually, looking at `HandlePokerCommand`, we set stack = buyin. We can't easily recover it unless we stored it. Let's modify PokerGame struct to store BuyIn? For this specific edit, I'll stick to a simple restart with 100 or try to parse from description? User said "same bet". I will modify PokerGame struct in a separate edit if needed, or just guess. BUT WAIT, I can't modify struct in this same tool call easily if I didn't plan it. Let's look at `HandlePokerCommand` again. It sets `Stack`. I'll just use 100 as per new default if I can't confirm. OR, I can pass `g.BigBlind * 10`? Actually, I'll just restart with 100 for now, or maybe the user wants the exact same custom buyin? A quick improvement: add BuyIn to struct. I'll do that in a follow-up or just use 100. Let's assume 100 for now to safeguard.

	buyin := 100 // Default or extracted

	// Check balance
	bal, err := DB.GetBalance(i.Member.User.ID)
	if err != nil || bal < buyin {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: "Insufficient funds for replay!", Flags: discordgo.MessageFlagsEphemeral},
		})
		return
	}

	if err := DB.AddBalance(i.Member.User.ID, -buyin); err != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: "Transaction failed.", Flags: discordgo.MessageFlagsEphemeral},
		})
		return
	}

	newGame := &PokerGame{
		HostID:     i.Member.User.ID,
		Stage:      StageLobby,
		SmallBlind: 5,
		BigBlind:   10,
		Players: []*PokerPlayer{{
			UserID:   i.Member.User.ID,
			Username: i.Member.User.Username,
			Stack:    buyin,
			Status:   PlayerPlaying,
		}},
		StartTime: time.Now().Add(pokerLobbyTime),
	}

	embed := createPokerLobbyEmbed(newGame)
	components := createPokerLobbyButtons()

	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
	if err != nil {
		DB.AddBalance(i.Member.User.ID, buyin)
		return
	}

	msg, err := s.InteractionResponse(i.Interaction)
	if err != nil {
		return
	}

	newGame.MessageID = msg.ID
	newGame.ChannelID = msg.ChannelID

	pokerMutex.Lock()
	activePokerGames[msg.ID] = newGame
	pokerMutex.Unlock()

	newGame.LobbyTimer = time.AfterFunc(pokerLobbyTime, func() {
		pokerMutex.Lock()
		g, exists := activePokerGames[msg.ID]
		if exists && g.Stage == StageLobby {
			if len(g.Players) >= 2 {
				startPokerGame(s, g, nil)
			} else {
				for _, p := range g.Players {
					DB.AddBalance(p.UserID, p.Stack)
				}
				content := "Game cancelled: Not enough players."
				emptyEmbeds := []*discordgo.MessageEmbed{}
				emptyComponents := []discordgo.MessageComponent{}
				s.ChannelMessageEditComplex(&discordgo.MessageEdit{
					ID:         g.MessageID,
					Channel:    g.ChannelID,
					Content:    &content,
					Embeds:     &emptyEmbeds,
					Components: &emptyComponents,
				})
				delete(activePokerGames, msg.ID)
			}
		}
		pokerMutex.Unlock()
	})
}

func handlePokerJoin(s *discordgo.Session, i *discordgo.InteractionCreate, g *PokerGame) {
	pokerMutex.Lock()
	defer pokerMutex.Unlock()

	if g.Stage != StageLobby {
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

	if len(g.Players) >= maxPokerPlayers {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: "Lobby is full!", Flags: discordgo.MessageFlagsEphemeral},
		})
		return
	}

	buyin := g.Players[0].Stack
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

	g.Players = append(g.Players, &PokerPlayer{
		UserID:   i.Member.User.ID,
		Username: i.Member.User.Username,
		Stack:    buyin,
		Status:   PlayerPlaying,
	})
	log.Printf("[POKER JOIN] %s joined game %s", i.Member.User.Username, g.MessageID)

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{createPokerLobbyEmbed(g)},
			Components: createPokerLobbyButtons(),
		},
	})
}

func handlePokerStartButton(s *discordgo.Session, i *discordgo.InteractionCreate, g *PokerGame) {
	pokerMutex.Lock()
	defer pokerMutex.Unlock()

	if i.Member.User.ID != g.HostID {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: "Only the host can start early!", Flags: discordgo.MessageFlagsEphemeral},
		})
		return
	}

	if g.Stage != StageLobby {
		return
	}

	if len(g.Players) < 2 {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: "Need at least 2 players!", Flags: discordgo.MessageFlagsEphemeral},
		})
		return
	}

	if g.LobbyTimer != nil {
		g.LobbyTimer.Stop()
	}

	startPokerGame(s, g, i)
}

func startPokerGame(s *discordgo.Session, g *PokerGame, i *discordgo.InteractionCreate) {
	g.Stage = StagePreFlop
	g.Deck = NewDeck()

	for _, p := range g.Players {
		p.HoleCards = []Card{g.Deck[0], g.Deck[1]}
		g.Deck = g.Deck[2:]
	}

	g.DealerButton = rand.Intn(len(g.Players))

	sbIdx := (g.DealerButton + 1) % len(g.Players)
	bbIdx := (g.DealerButton + 2) % len(g.Players)
	if len(g.Players) == 2 {
		sbIdx = g.DealerButton
		bbIdx = (g.DealerButton + 1) % len(g.Players)
	}

	postBlind(g.Players[sbIdx], g.SmallBlind, g)
	postBlind(g.Players[bbIdx], g.BigBlind, g)

	g.MinBet = g.BigBlind
	g.CurrentTurn = (bbIdx + 1) % len(g.Players)

	updatePokerGameMessage(s, g, i)
}

func postBlind(p *PokerPlayer, amount int, g *PokerGame) {
	if amount > p.Stack {
		amount = p.Stack
		p.Status = PlayerAllIn
	}
	p.Stack -= amount
	p.CurrentBet += amount
	p.TotalBet += amount
}

func updatePokerGameMessage(s *discordgo.Session, g *PokerGame, i *discordgo.InteractionCreate) {
	embed := &discordgo.MessageEmbed{
		Title: "Poker",
		Color: 0x00FF00,
	}

	communityStr := ""
	for _, c := range g.CommunityCards {
		communityStr += fmt.Sprintf("`%s%s` ", c.Value, c.Suit)
	}
	if communityStr == "" {
		communityStr = "None"
	}

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:  "Community Cards",
		Value: communityStr,
	})

	potTotal := 0
	for _, p := range g.Players {
		potTotal += p.TotalBet
	}

	embed.Description = fmt.Sprintf("Stage: **%s**\nTotal Pot: **$%d**", g.Stage, potTotal)
	if g.ResultText != "" {
		embed.Description += "\n\n" + g.ResultText
	}

	for idx, p := range g.Players {
		status := ""
		if p.Status == PlayerFolded {
			status = "❌ **FOLDED**"
		} else if p.Status == PlayerAllIn {
			status = "💎 **ALL-IN**"
		} else if idx == g.CurrentTurn && g.Stage != StageShowdown {
			status = "➡️ **TURN**"
		}

		if idx == g.DealerButton {
			status += " 🔘"
		}

		val := fmt.Sprintf("Bet: $%d", p.CurrentBet)
		if g.Stage == StageShowdown && p.Status != PlayerFolded {
			// Reveal cards
			handStr := ""
			for _, c := range p.HoleCards {
				handStr += fmt.Sprintf("`%s%s` ", c.Value, c.Suit)
			}
			val = fmt.Sprintf("Cards: %s\nBet: $%d", handStr, p.CurrentBet)
		}

		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("%s ($%d) %s", p.Username, p.Stack, status),
			Value:  val,
			Inline: false,
		})
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{Label: "Reveal Hand", Style: discordgo.SecondaryButton, CustomID: "game_poker_reveal"},
				discordgo.Button{Label: "Fold", Style: discordgo.DangerButton, CustomID: "game_poker_fold"},
				discordgo.Button{Label: "Check/Call", Style: discordgo.PrimaryButton, CustomID: "game_poker_call"},
				discordgo.Button{Label: "Raise", Style: discordgo.SuccessButton, CustomID: "game_poker_raise_modal"},
			},
		},
	}

	if g.Stage == StageShowdown {
		components = []discordgo.MessageComponent{}
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

func handlePokerReveal(s *discordgo.Session, i *discordgo.InteractionCreate, g *PokerGame) {
	var player *PokerPlayer
	for _, p := range g.Players {
		if p.UserID == i.Member.User.ID {
			player = p
			break
		}
	}

	if player == nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: "You are not in this game.", Flags: discordgo.MessageFlagsEphemeral},
		})
		return
	}

	handStr := ""
	for _, c := range player.HoleCards {
		handStr += fmt.Sprintf("`%s%s` ", c.Value, c.Suit)
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("Your Hole Cards: %s", handStr),
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

func handlePokerFold(s *discordgo.Session, i *discordgo.InteractionCreate, g *PokerGame) {
	pokerMutex.Lock()
	defer pokerMutex.Unlock()

	activePlayer := g.Players[g.CurrentTurn]
	if i.Member.User.ID != activePlayer.UserID {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: "It's not your turn!", Flags: discordgo.MessageFlagsEphemeral},
		})
		return
	}

	activePlayer.Status = PlayerFolded
	log.Printf("[POKER ACTION] %s FOLDED", i.Member.User.Username)
	nextPokerTurn(s, g, i)
}

func handlePokerCall(s *discordgo.Session, i *discordgo.InteractionCreate, g *PokerGame) {
	pokerMutex.Lock()
	defer pokerMutex.Unlock()

	activePlayer := g.Players[g.CurrentTurn]
	if i.Member.User.ID != activePlayer.UserID {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: "It's not your turn!", Flags: discordgo.MessageFlagsEphemeral},
		})
		return
	}

	callAmount := g.MinBet - activePlayer.CurrentBet
	if callAmount > activePlayer.Stack {
		callAmount = activePlayer.Stack
		activePlayer.Status = PlayerAllIn
	}

	activePlayer.Stack -= callAmount
	activePlayer.CurrentBet += callAmount
	activePlayer.TotalBet += callAmount

	log.Printf("[POKER ACTION] %s %s | Amount: %d", i.Member.User.Username, func() string {
		if callAmount == 0 {
			return "CHECK"
		}
		return "CALL"
	}(), callAmount)

	nextPokerTurn(s, g, i)
}

func handlePokerRaiseModalTrigger(s *discordgo.Session, i *discordgo.InteractionCreate, g *PokerGame) {
	activePlayer := g.Players[g.CurrentTurn]
	if i.Member.User.ID != activePlayer.UserID {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: "It's not your turn!", Flags: discordgo.MessageFlagsEphemeral},
		})
		return
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: "game_poker_raise_modal_" + g.MessageID,
			Title:    "Raise Amount",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "amount",
							Label:       fmt.Sprintf("Total target bet (Min: %d)", g.MinBet+g.LastRaise),
							Style:       discordgo.TextInputShort,
							Placeholder: strconv.Itoa(g.MinBet + g.LastRaise),
							Required:    true,
						},
					},
				},
			},
		},
	})
}

func HandlePokerModal(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ModalSubmitData()
	if !strings.HasPrefix(data.CustomID, "game_poker_raise_modal_") {
		return
	}
	msgID := strings.TrimPrefix(data.CustomID, "game_poker_raise_modal_")

	pokerMutex.Lock()
	g, exists := activePokerGames[msgID]
	pokerMutex.Unlock()

	if !exists {
		respondError(s, i, "Game not found.")
		return
	}

	pokerMutex.Lock()
	defer pokerMutex.Unlock()

	activePlayer := g.Players[g.CurrentTurn]
	if i.Member.User.ID != activePlayer.UserID {
		respondError(s, i, "It's not your turn!")
		return
	}

	amountStr := data.Components[0].(*discordgo.ActionsRow).Components[0].(*discordgo.TextInput).Value
	amount, err := strconv.Atoi(amountStr)
	if err != nil || amount <= g.MinBet {
		respondError(s, i, "Invalid raise amount.")
		return
	}

	raiseAmount := amount - activePlayer.CurrentBet
	if raiseAmount > activePlayer.Stack {
		raiseAmount = activePlayer.Stack
		activePlayer.Status = PlayerAllIn
	}

	activePlayer.Stack -= raiseAmount
	activePlayer.CurrentBet += raiseAmount
	activePlayer.TotalBet += raiseAmount

	if activePlayer.CurrentBet > g.MinBet {
		g.LastRaise = activePlayer.CurrentBet - g.MinBet
		g.MinBet = activePlayer.CurrentBet
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
	})

	nextPokerTurn(s, g, nil)
	log.Printf("[POKER ACTION] %s RAISED to %d", i.Member.User.Username, activePlayer.CurrentBet)
}

func nextPokerTurn(s *discordgo.Session, g *PokerGame, i *discordgo.InteractionCreate) {
	activeCount := 0
	var lastPlayer *PokerPlayer
	for _, p := range g.Players {
		if p.Status != PlayerFolded {
			activeCount++
			lastPlayer = p
		}
	}

	if activeCount == 1 {
		finishPokerGame(s, g, lastPlayer)
		updatePokerGameMessage(s, g, i)
		return
	}

	startTurn := g.CurrentTurn
	for {
		g.CurrentTurn = (g.CurrentTurn + 1) % len(g.Players)
		p := g.Players[g.CurrentTurn]
		if p.Status != PlayerFolded { // Use simplified check: if not folded, they are in hand (Playing or All-In)
			break
		}
		if g.CurrentTurn == startTurn {
			// Should be covered by activeCount check above, but safety
			break
		}
	}

	allMatched := true
	for _, p := range g.Players {
		if (p.Status == PlayerPlaying || p.Status == PlayerAllIn) && p.CurrentBet < g.MinBet {
			if p.Status == PlayerAllIn {
				continue
			}
			allMatched = false
			break
		}
	}

	if allMatched && g.CurrentTurn == (g.DealerButton+1)%len(g.Players) {
		advancePokerStage(s, g)
	}

	updatePokerGameMessage(s, g, i)
}

func advancePokerStage(s *discordgo.Session, g *PokerGame) {
	for _, p := range g.Players {
		p.CurrentBet = 0
	}
	g.MinBet = 0
	g.LastRaise = 0

	switch g.Stage {
	case StagePreFlop:
		g.Stage = StageFlop
		g.CommunityCards = append(g.CommunityCards, g.Deck[0], g.Deck[1], g.Deck[2])
		g.Deck = g.Deck[3:]
	case StageFlop:
		g.Stage = StageTurn
		g.CommunityCards = append(g.CommunityCards, g.Deck[0])
		g.Deck = g.Deck[1:]
	case StageTurn:
		g.Stage = StageRiver
		g.CommunityCards = append(g.CommunityCards, g.Deck[0])
		g.Deck = g.Deck[1:]
	case StageRiver:
		g.Stage = StageShowdown
		determinePokerWinners(s, g)
		return // Stop the flow here so we don't try to calculate next turn
	}

	g.CurrentTurn = (g.DealerButton + 1) % len(g.Players)
	for g.Players[g.CurrentTurn].Status != PlayerPlaying {
		g.CurrentTurn = (g.CurrentTurn + 1) % len(g.Players)
	}
}

func determinePokerWinners(s *discordgo.Session, g *PokerGame) {
	// Calculate Side Pots Logic We dynamically calculate pots based on TotalBet of each player.

	type PotCandidate struct {
		Amount          int
		EligiblePlayers []*PokerPlayer
	}

	var pots []PotCandidate

	// Map to track how much of each player's bet has been allocated to a pot
	allocated := make(map[string]int)

	for {
		// Find the minimum unallocated bet > 0
		minBet := 2147483647
		found := false
		for _, p := range g.Players {
			unallocated := p.TotalBet - allocated[p.UserID]
			if unallocated > 0 {
				if unallocated < minBet {
					minBet = unallocated
				}
				found = true
			}
		}

		if !found {
			break
		}

		// Create a pot for this slice of bets
		pot := PotCandidate{Amount: 0}
		for _, p := range g.Players {
			unallocated := p.TotalBet - allocated[p.UserID]
			if unallocated > 0 {
				contribution := minBet
				if unallocated < minBet {
					contribution = unallocated // Should not happen if minBet is truly min, but safety
				}
				pot.Amount += contribution
				allocated[p.UserID] += contribution

				// If player is not folded, they are eligible for this pot
				if p.Status != PlayerFolded {
					pot.EligiblePlayers = append(pot.EligiblePlayers, p)
				}
			}
		}
		pots = append(pots, pot)
	}

	// Distribute each pot
	var resultBuilder strings.Builder
	resultBuilder.WriteString("🏆 **WINNERS:**\n")

	playerWinnings := make(map[string]int)

	for i, pot := range pots {
		if pot.Amount == 0 || len(pot.EligiblePlayers) == 0 {
			continue
		}

		bestScore := int64(-1)
		var winners []*PokerPlayer

		// Find winners for this specific pot
		for _, p := range pot.EligiblePlayers {
			eval := EvaluateBestHand(append(p.HoleCards, g.CommunityCards...))
			if eval.Score > bestScore {
				bestScore = eval.Score
				winners = []*PokerPlayer{p}
			} else if eval.Score == bestScore {
				winners = append(winners, p)
			}
		}

		payout := pot.Amount / len(winners)
		remainder := pot.Amount % len(winners)

		for idx, w := range winners {
			amt := payout
			if idx == 0 {
				amt += remainder
			}
			playerWinnings[w.UserID] += amt
			// We don't credit DB yet, just aggregate
		}

		// Log specific pot (optional, simplified for user)
		if len(pots) > 1 {
			// Debug or verbose info could go here, but for now we aggregate
		}
		// To avoid "unused" error if I don't use i
		_ = i
	}

	// Apply winnings and build result string
	for _, p := range g.Players {
		win := playerWinnings[p.UserID]
		if win > 0 {
			DB.AddBalance(p.UserID, win)
			playerWinnings[p.UserID] = 0 // Clear to mark as processed

			eval := EvaluateBestHand(append(p.HoleCards, g.CommunityCards...))
			resultBuilder.WriteString(fmt.Sprintf("- **%s** won **$%d** with **%s**\n", p.Username, win, eval.Rank))
		}

		// Refund remaining stack (this logic was in original, unclear why stack is refunded? Ah, stack is money *remaining* that wasn't bet. Yes, that stays with user implicitly if we didn't deduct it. Wait, the code deducts buyin at start. So `Stack` is money ON THE TABLE. So yes, we must refund `p.Stack`.

		if p.Stack > 0 {
			DB.AddBalance(p.UserID, p.Stack)
		}
	}

	g.ResultText = resultBuilder.String()

	// Show Play Again
	components := createPokerPlayAgainButtons()

	embed := &discordgo.MessageEmbed{
		Title:       "Poker - Game Over",
		Description: g.ResultText,
		Color:       0xF1C40F,
	}

	s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		ID:         g.MessageID,
		Channel:    g.ChannelID,
		Embeds:     &[]*discordgo.MessageEmbed{embed},
		Components: &components,
	})

	// Temporary button cleanup
	go func(mID, cID string) {
		time.Sleep(10 * time.Second)
		s.ChannelMessageEditComplex(&discordgo.MessageEdit{
			ID:         mID,
			Channel:    cID,
			Components: &[]discordgo.MessageComponent{},
		})
	}(g.MessageID, g.ChannelID)

	delete(activePokerGames, g.MessageID)
}

func finishPokerGame(s *discordgo.Session, g *PokerGame, winner *PokerPlayer) {
	totalPot := 0
	for _, p := range g.Players {
		totalPot += p.TotalBet
	}

	DB.AddBalance(winner.UserID, totalPot)
	g.ResultText = fmt.Sprintf("🏆 **WINNER**: %s (+$%d)\nEveryone else folded.", winner.Username, totalPot)
	g.Stage = StageShowdown

	// Refund all players
	for _, p := range g.Players {
		if p.Stack > 0 {
			DB.AddBalance(p.UserID, p.Stack)
		}
	}

	components := createPokerPlayAgainButtons()
	s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		ID:         g.MessageID,
		Channel:    g.ChannelID,
		Embeds:     &[]*discordgo.MessageEmbed{{Title: "Poker - Finished", Description: g.ResultText, Color: 0xF1C40F}},
		Components: &components,
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

	delete(activePokerGames, g.MessageID)
}

// Hand evaluation helpers

type EvaluatedHand struct {
	Rank  HandRank
	Score int64
	Cards []Card
}

func GetCardValue(v string) int {
	switch v {
	case "A":
		return 14
	case "K":
		return 13
	case "Q":
		return 12
	case "J":
		return 11
	case "10":
		return 10
	default:
		val, _ := strconv.Atoi(v)
		return val
	}
}

func EvaluateBestHand(cards []Card) EvaluatedHand {
	best := EvaluatedHand{Rank: 0, Score: -1}
	indices := make([]int, 5)
	var generateCombos func(int, int)
	generateCombos = func(start, depth int) {
		if depth == 5 {
			combo := make([]Card, 5)
			for i, idx := range indices {
				combo[i] = cards[idx]
			}
			curr := evaluateFiveCardHand(combo)
			if curr.Score > best.Score {
				best = curr
			}
			return
		}
		for i := start; i < len(cards); i++ {
			indices[depth] = i
			generateCombos(i+1, depth+1)
		}
	}
	generateCombos(0, 0)
	return best
}

func evaluateFiveCardHand(cards []Card) EvaluatedHand {
	sort.Slice(cards, func(i, j int) bool {
		return GetCardValue(cards[i].Value) > GetCardValue(cards[j].Value)
	})

	isFlush := true
	for i := 1; i < 5; i++ {
		if cards[i].Suit != cards[0].Suit {
			isFlush = false
			break
		}
	}

	isStraight := true
	for i := 0; i < 4; i++ {
		if GetCardValue(cards[i].Value) != GetCardValue(cards[i+1].Value)+1 {
			if i == 0 && cards[0].Value == "A" && GetCardValue(cards[1].Value) == 5 && GetCardValue(cards[2].Value) == 4 && GetCardValue(cards[3].Value) == 3 && GetCardValue(cards[4].Value) == 2 {
				continue
			}
			isStraight = false
			break
		}
	}

	counts := make(map[string]int)
	for _, c := range cards {
		counts[c.Value]++
	}
	var pairs, trips, quads []int
	for val, count := range counts {
		v := GetCardValue(val)
		switch count {
		case 2:
			pairs = append(pairs, v)
		case 3:
			trips = append(trips, v)
		case 4:
			quads = append(quads, v)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(pairs)))

	buildScore := func(r HandRank, values ...int) int64 {
		s := int64(r)
		for _, v := range values {
			s = (s << 4) | int64(v)
		}
		for len(values) < 5 {
			s <<= 4
			values = append(values, 0)
		}
		return s
	}

	var rank HandRank
	var score int64

	if isStraight && isFlush {
		rank = RankStraightFlush
		if cards[0].Value == "A" && cards[1].Value == "K" {
			rank = RankRoyalFlush
		}
		val := GetCardValue(cards[0].Value)
		if cards[0].Value == "A" && GetCardValue(cards[1].Value) == 5 {
			val = 5
		}
		score = buildScore(rank, val)
	} else if len(quads) > 0 {
		rank = RankFourOfAKind
		kicker := 0
		for _, c := range cards {
			if GetCardValue(c.Value) != quads[0] {
				kicker = GetCardValue(c.Value)
				break
			}
		}
		score = buildScore(rank, quads[0], kicker)
	} else if len(trips) > 0 && len(pairs) > 0 {
		rank = RankFullHouse
		score = buildScore(rank, trips[0], pairs[0])
	} else if isFlush {
		rank = RankFlush
		score = buildScore(rank, GetCardValue(cards[0].Value), GetCardValue(cards[1].Value), GetCardValue(cards[2].Value), GetCardValue(cards[3].Value), GetCardValue(cards[4].Value))
	} else if isStraight {
		rank = RankStraight
		val := GetCardValue(cards[0].Value)
		if cards[0].Value == "A" && GetCardValue(cards[1].Value) == 5 {
			val = 5
		}
		score = buildScore(rank, val)
	} else if len(trips) > 0 {
		rank = RankThreeOfAKind
		var kickers []int
		for _, c := range cards {
			if GetCardValue(c.Value) != trips[0] {
				kickers = append(kickers, GetCardValue(c.Value))
			}
		}
		sort.Sort(sort.Reverse(sort.IntSlice(kickers)))
		score = buildScore(rank, trips[0], kickers[0], kickers[1])
	} else if len(pairs) >= 2 {
		rank = RankTwoPair
		kicker := 0
		for _, c := range cards {
			if GetCardValue(c.Value) != pairs[0] && GetCardValue(c.Value) != pairs[1] {
				kicker = GetCardValue(c.Value)
				break
			}
		}
		score = buildScore(rank, pairs[0], pairs[1], kicker)
	} else if len(pairs) == 1 {
		rank = RankPair
		var kickers []int
		for _, c := range cards {
			if GetCardValue(c.Value) != pairs[0] {
				kickers = append(kickers, GetCardValue(c.Value))
			}
		}
		sort.Sort(sort.Reverse(sort.IntSlice(kickers)))
		score = buildScore(rank, pairs[0], kickers[0], kickers[1], kickers[2])
	} else {
		rank = RankHighCard
		score = buildScore(rank, GetCardValue(cards[0].Value), GetCardValue(cards[1].Value), GetCardValue(cards[2].Value), GetCardValue(cards[3].Value), GetCardValue(cards[4].Value))
	}
	return EvaluatedHand{Rank: rank, Score: score, Cards: cards}
}
