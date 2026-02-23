package commands

import (
	"fmt"
	"strings"

	"soosa/internal/player"

	"github.com/bwmarrin/discordgo"
)

// PlayerManager is set during initialization in main.go.
var PlayerManager *player.Manager

// MusicCommands defines all music-related slash commands.
var MusicCommands = []*discordgo.ApplicationCommand{
	{
		Name:        "play",
		Description: "Search and play a song from the music server",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "query",
				Description: "Song name or search query",
				Required:    true,
			},
		},
	},
	{
		Name:        "search",
		Description: "Search for songs on the music server",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "query",
				Description: "Search query",
				Required:    true,
			},
		},
	},
	{
		Name:        "queue",
		Description: "Show the current music queue",
	},
	{
		Name:        "skip",
		Description: "Skip the current song",
	},
	{
		Name:        "stop",
		Description: "Stop playback, clear queue, and leave voice",
	},
	{
		Name:        "nowplaying",
		Description: "Show the currently playing song",
	},
	{
		Name:        "pause",
		Description: "Pause the current song",
	},
	{
		Name:        "resume",
		Description: "Resume playback",
	},
	{
		Name:        "volume",
		Description: "Show or set playback volume",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "level",
				Description: "Volume level (0-100)",
				Required:    false,
				MinValue:    &minVolume,
				MaxValue:    100,
			},
		},
	},
}

var minVolume = 0.0

// HandleMusicCommand routes music commands to their handlers.
func HandleMusicCommand(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	switch data.Name {
	case "play":
		handlePlay(s, i, data)
	case "search":
		handleSearch(s, i, data)
	case "queue":
		handleQueue(s, i)
	case "skip":
		handleSkip(s, i)
	case "stop":
		handleStop(s, i)
	case "nowplaying":
		handleNowPlaying(s, i)
	case "pause":
		handlePause(s, i)
	case "resume":
		handleResume(s, i)
	case "volume":
		handleVolume(s, i, data)
	}
}

// HandleMusicComponent routes button interactions.
func HandleMusicComponent(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	switch data.CustomID {
	case "music_prev":
		handlePrev(s, i)
	case "music_pause":
		handlePauseResumeToggle(s, i)
	case "music_stop":
		handleStop(s, i) // Use existing stop handler
	case "music_next":
		handleSkip(s, i) // Next is just skip
	}
}

// --- Helpers ---

func respond(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
		},
	})
}

func respondEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
		},
	})
}

// findUserVoiceChannel finds the voice channel the command invoker is in.
func findUserVoiceChannel(s *discordgo.Session, guildID, userID string) string {
	guild, err := s.State.Guild(guildID)
	if err != nil {
		return ""
	}
	for _, vs := range guild.VoiceStates {
		if vs.UserID == userID {
			return vs.ChannelID
		}
	}
	return ""
}

// --- Command Handlers ---

func handlePlay(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	query := data.Options[0].StringValue()

	// Find user's voice channel
	channelID := findUserVoiceChannel(s, i.GuildID, i.Member.User.ID)
	if channelID == "" {
		respond(s, i, "❌ You must be in a voice channel to play music.")
		return
	}

	// Acknowledge immediately since search + join may take a moment
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})

	// Search for the song
	subClient := PlayerManager.GetSubsonicClient()
	results, err := subClient.Search(query, 1)
	if err != nil {
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: strPtr(fmt.Sprintf("❌ Search failed: %v", err)),
		})
		return
	}

	if len(results.Songs) == 0 {
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: strPtr(fmt.Sprintf("❌ No results found for: **%s**", query)),
		})
		return
	}

	song := &results.Songs[0]
	p := PlayerManager.GetOrCreatePlayer(i.GuildID)

	// If already playing, just enqueue
	if p.IsPlaying {
		p.Enqueue(song)
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: strPtr(fmt.Sprintf("🎵 Added to queue: **%s** — %s (%s)",
				song.Title, song.Artist, player.FormatDuration(song.Duration))),
		})
		return
	}

	// Otherwise enqueue and start playing
	p.Enqueue(song)
	err = p.Start(channelID)
	if err != nil {
		PlayerManager.Remove(i.GuildID)
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: strPtr(fmt.Sprintf("❌ Failed to join voice: %v", err)),
		})
		return
	}

	embed := &discordgo.MessageEmbed{
		Title:       "🎶 Now Playing",
		Description: fmt.Sprintf("**%s**\n%s", song.Title, song.Artist),
		Color:       0x1DB954,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Album", Value: song.Album, Inline: true},
			{Name: "Duration", Value: player.FormatDuration(song.Duration), Inline: true},
		},
	}
	if song.CoverArt != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{
			URL: subClient.GetCoverArtURL(song.CoverArt, 300),
		}
	}

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name: "Requested by", Value: i.Member.User.Mention(), Inline: true,
	})

	// Add buttons
	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Prev",
					Emoji:    &discordgo.ComponentEmoji{Name: "⏮️"},
					Style:    discordgo.SecondaryButton,
					CustomID: "music_prev",
				},
				discordgo.Button{
					Label:    "Pause/Plau",
					Emoji:    &discordgo.ComponentEmoji{Name: "⏯️"},
					Style:    discordgo.SecondaryButton,
					CustomID: "music_pause",
				},
				discordgo.Button{
					Label:    "Stop",
					Emoji:    &discordgo.ComponentEmoji{Name: "⏹️"},
					Style:    discordgo.DangerButton,
					CustomID: "music_stop",
				},
				discordgo.Button{
					Label:    "Next",
					Emoji:    &discordgo.ComponentEmoji{Name: "⏭️"},
					Style:    discordgo.SecondaryButton,
					CustomID: "music_next",
				},
			},
		},
	}

	msg, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds:     &[]*discordgo.MessageEmbed{embed},
		Components: &components,
	})

	if err == nil && msg != nil {
		p.SetLastMessage(msg.ID, msg.ChannelID)
	}
}

func handleSearch(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	query := data.Options[0].StringValue()

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})

	subClient := PlayerManager.GetSubsonicClient()
	results, err := subClient.Search(query, 5)
	if err != nil {
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: strPtr(fmt.Sprintf("❌ Search failed: %v", err)),
		})
		return
	}

	if len(results.Songs) == 0 {
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: strPtr(fmt.Sprintf("❌ No results found for: **%s**", query)),
		})
		return
	}

	var lines []string
	for idx, song := range results.Songs {
		lines = append(lines, fmt.Sprintf("`%d.` **%s** — %s (%s)",
			idx+1, song.Title, song.Artist, player.FormatDuration(song.Duration)))
	}

	embed := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("🔎 Search Results for \"%s\"", query),
		Description: strings.Join(lines, "\n"),
		Color:       0x5865F2,
		Footer:      &discordgo.MessageEmbedFooter{Text: "Use /play <song name> to play a result"},
	}

	s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{embed},
	})
}

func handleQueue(s *discordgo.Session, i *discordgo.InteractionCreate) {
	p := PlayerManager.GetPlayer(i.GuildID)
	if p == nil || !p.IsPlaying {
		respond(s, i, "📭 Nothing is playing right now.")
		return
	}

	queue := p.GetQueue()

	var desc strings.Builder
	if p.NowPlaying != nil {
		desc.WriteString(fmt.Sprintf("**Now Playing:**\n🎵 **%s** — %s (%s)\n\n",
			p.NowPlaying.Title, p.NowPlaying.Artist, player.FormatDuration(p.NowPlaying.Duration)))
	}

	if len(queue) == 0 {
		desc.WriteString("Queue is empty.")
	} else {
		desc.WriteString("**Up Next:**\n")
		for idx, song := range queue {
			if idx >= 10 {
				desc.WriteString(fmt.Sprintf("\n...and %d more", len(queue)-10))
				break
			}
			desc.WriteString(fmt.Sprintf("`%d.` **%s** — %s (%s)\n",
				idx+1, song.Title, song.Artist, player.FormatDuration(song.Duration)))
		}
	}

	embed := &discordgo.MessageEmbed{
		Title:       "📋 Queue",
		Description: desc.String(),
		Color:       0x5865F2,
	}
	respondEmbed(s, i, embed)
}

func handleSkip(s *discordgo.Session, i *discordgo.InteractionCreate) {
	p := PlayerManager.GetPlayer(i.GuildID)
	if p == nil || !p.IsPlaying {
		respond(s, i, "❌ Nothing is playing right now.")
		return
	}

	skipped := p.NowPlaying
	p.Skip()

	if skipped != nil {
		respond(s, i, fmt.Sprintf("⏭️ Skipped **%s** — %s", skipped.Title, skipped.Artist))
	} else {
		respond(s, i, "⏭️ Skipped.")
	}
}

func handlePrev(s *discordgo.Session, i *discordgo.InteractionCreate) {
	p := PlayerManager.GetPlayer(i.GuildID)
	if p == nil || !p.IsPlaying {
		respond(s, i, "❌ Nothing is playing right now.")
		return
	}

	p.PlayPrevious()

	// Respond to the interaction to prevent "Interaction failed"
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "⏮️ Playing previous song...",
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

func handleStop(s *discordgo.Session, i *discordgo.InteractionCreate) {
	p := PlayerManager.GetPlayer(i.GuildID)
	if p == nil {
		respond(s, i, "❌ Nothing is playing right now.")
		return
	}

	PlayerManager.Remove(i.GuildID)

	// If triggered by button, update message to remove buttons? Or just respond.

	if i.Type == discordgo.InteractionMessageComponent {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    "⏹️ Stopped playback.",
				Components: []discordgo.MessageComponent{}, // clear buttons
			},
		})
	} else {
		respond(s, i, "⏹️ Stopped playback and left the voice channel.")
	}
}

func handleNowPlaying(s *discordgo.Session, i *discordgo.InteractionCreate) {
	p := PlayerManager.GetPlayer(i.GuildID)
	if p == nil || !p.IsPlaying || p.NowPlaying == nil {
		respond(s, i, "📭 Nothing is playing right now.")
		return
	}

	song := p.NowPlaying
	subClient := PlayerManager.GetSubsonicClient()

	embed := &discordgo.MessageEmbed{
		Title:       "🎶 Now Playing",
		Description: fmt.Sprintf("**%s**\n%s", song.Title, song.Artist),
		Color:       0x1DB954,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Album", Value: song.Album, Inline: true},
			{Name: "Duration", Value: player.FormatDuration(song.Duration), Inline: true},
		},
	}
	if song.CoverArt != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{
			URL: subClient.GetCoverArtURL(song.CoverArt, 300),
		}
	}
	if song.Year > 0 {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name: "Year", Value: fmt.Sprintf("%d", song.Year), Inline: true,
		})
	}

	respondEmbed(s, i, embed)
}

func handlePause(s *discordgo.Session, i *discordgo.InteractionCreate) {
	p := PlayerManager.GetPlayer(i.GuildID)
	if p == nil || !p.IsPlaying {
		respond(s, i, "❌ Nothing is playing right now.")
		return
	}
	p.Pause()
	respond(s, i, "⏸️ Paused.")
}

func handleResume(s *discordgo.Session, i *discordgo.InteractionCreate) {
	p := PlayerManager.GetPlayer(i.GuildID)
	if p == nil {
		respond(s, i, "❌ Nothing is playing right now.")
		return
	}
	p.Resume()
	respond(s, i, "▶️ Resumed.")
}

func handlePauseResumeToggle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	p := PlayerManager.GetPlayer(i.GuildID)
	if p == nil { // Not playing
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ Nothing is playing.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	if p.IsPaused {
		p.Resume()
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "▶️ Resumed.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
	} else {
		p.Pause()
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "⏸️ Paused.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
	}
}

func handleVolume(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	p := PlayerManager.GetOrCreatePlayer(i.GuildID)

	if len(data.Options) == 0 {
		// Show current volume
		currentVol := int(float64(p.Volume) / 256.0 * 100.0)
		respond(s, i, fmt.Sprintf("🔊 Current volume is **%d%%**.", currentVol))
		return
	}

	level := int(data.Options[0].IntValue())

	// Map 0-100 to 0-256 (dca volume range)
	dcaVolume := int(float64(level) / 100.0 * 256.0)
	p.SetVolume(dcaVolume)

	respond(s, i, fmt.Sprintf("🔊 Volume set to **%d%%**.", level))
}

func strPtr(s string) *string {
	return &s
}
