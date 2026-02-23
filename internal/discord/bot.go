package discord

import (
	"fmt"
	"log"

	"github.com/bwmarrin/discordgo"
	// "soosa/internal/config" // This line was removed as per instruction
)

type Bot struct {
	Session *discordgo.Session
	Config  *Config
}

func New(cfg *Config) (*Bot, error) {
	session, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		return nil, fmt.Errorf("error creating Discord session: %w", err)
	}

	return &Bot{
		Session: session,
		Config:  cfg,
	}, nil
}

func (b *Bot) Start() error {
	// Set intents
	b.Session.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsGuildMembers |
		discordgo.IntentsMessageContent |
		discordgo.IntentsGuildVoiceStates

	err := b.Session.Open()
	if err != nil {
		return fmt.Errorf("error opening connection: %w", err)
	}

	log.Println("Bot is now running. Press CTRL-C to exit.")
	return nil
}

func (b *Bot) Stop() error {
	return b.Session.Close()
}
