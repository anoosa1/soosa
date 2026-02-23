package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"soosa/internal/commands"
	"soosa/internal/database"
	"soosa/internal/discord"
	"soosa/internal/events"
	"soosa/internal/player"
	"soosa/internal/subsonic"

	"github.com/bwmarrin/discordgo"
)

func main() {
	// 1. Load Configuration
	cfg, err := discord.Load()
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	// 2. Initialize Bot
	bot, err := discord.New(cfg)
	if err != nil {
		log.Fatalf("Error initializing bot: %v", err)
	}

	// 3. Initialize Subsonic Client
	var subClient *subsonic.Client
	if cfg.SubsonicURL != "" && cfg.SubsonicUser != "" && cfg.SubsonicPassword != "" {
		subClient = subsonic.NewClient(cfg.SubsonicURL, cfg.SubsonicUser, cfg.SubsonicPassword)
		if err := subClient.Ping(); err != nil {
			log.Printf("Warning: Subsonic ping failed: %v (music features may not work)", err)
		} else {
			log.Println("Subsonic server connected successfully!")
		}
	} else {
		log.Println("Warning: Subsonic config not set. Music features disabled.")
	}

	// 4. Initialize Database
	db, err := database.New(cfg.Database)
	if err != nil {
		log.Fatalf("Error initializing database: %v", err)
	}
	defer db.Close()

	// 5. Initialize Wordle
	if cfg.WordleAnswersPath != "" && cfg.WordleAllowedPath != "" {
		if err := commands.LoadWordleWords(cfg.WordleAnswersPath, cfg.WordleAllowedPath); err != nil {
			log.Printf("Warning: Failed to load Wordle words: %v", err)
		} else {
			log.Println("Wordle words loaded successfully.")
		}
	} else {
		log.Println("Warning: Wordle paths not configured. Wordle features disabled.")
	}

	// Inject DB and OwnerID into commands package
	commands.DB = db
	// TODO: Make this configurable. For now, we might need to fetch it from the bot application info or config. Check if OwnerID is in config, otherwise fetch from Application info

	app, err := bot.Session.Application("@me")
	if err != nil {
		log.Printf("Warning: Could not fetch application info: %v", err)
	} else {
		if app.Owner != nil {
			commands.OwnerID = app.Owner.ID
			log.Printf("Bot Owner ID set to: %s", commands.OwnerID)
		} else if app.Team != nil {
			// If team, maybe allow all team members? For now let's just log it.
			log.Printf("Bot is owned by a team. OwnerID logic might need adjustment.")
		}
	}

	// 5. Initialize Player Manager
	if subClient != nil {
		mgr := player.NewManager(bot.Session, subClient, cfg)
		commands.PlayerManager = mgr
	}

	// 5. Register Event Handlers

	// Interaction Handler (Slash Commands)
	bot.Session.AddHandler(commands.HandleInteraction)

	// Logging Handlers
	if cfg.LogChannelID != "" {
		logger := events.NewLogger(bot.Session, cfg)
		bot.Session.AddHandler(logger.OnGuildMemberAdd)
		bot.Session.AddHandler(logger.OnGuildMemberRemove)
	}

	// Ready Handler
	bot.Session.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Printf("Logged in as: %v#%v", s.State.User.Username, s.State.User.Discriminator)
	})

	// 6. Start Bot
	err = bot.Start()
	if err != nil {
		log.Fatalf("Error starting bot: %v", err)
	}
	defer bot.Stop()

	// 7. Register Commands
	commands.RegisterCommands(bot.Session, cfg.GuildID)
	log.Println("Internal commands registered.")

	// 8. Wait for Shutdown Signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	log.Println("Bot is running. Press Ctrl+C to exit.")
	<-stop

	log.Println("Gracefully shutting down...")
	if commands.PlayerManager != nil {
		commands.PlayerManager.Shutdown()
	}
}
