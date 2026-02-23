package events

import (
	"fmt"
	"log"
	"soosa/internal/discord"
	"time"

	"github.com/bwmarrin/discordgo"
)

type Logger struct {
	Session      *discordgo.Session
	LogChannelID string
}

func NewLogger(s *discordgo.Session, cfg *discord.Config) *Logger {
	return &Logger{
		Session:      s,
		LogChannelID: cfg.LogChannelID,
	}
}

func (l *Logger) OnGuildMemberAdd(s *discordgo.Session, m *discordgo.GuildMemberAdd) {
	embed := &discordgo.MessageEmbed{
		Title:       "User Joined",
		Description: fmt.Sprintf("%s has joined the server.", m.User.Username),
		Color:       0x00ff00, // Green
		Timestamp:   time.Now().Format(time.RFC3339),
		Thumbnail:   &discordgo.MessageEmbedThumbnail{URL: m.User.AvatarURL("")},
	}
	s.ChannelMessageSendEmbed(l.LogChannelID, embed)
	log.Printf("[EVENT] User Joined: %s (%s)", m.User.Username, m.User.ID)
}

func (l *Logger) OnGuildMemberRemove(s *discordgo.Session, m *discordgo.GuildMemberRemove) {
	embed := &discordgo.MessageEmbed{
		Title:       "User Left",
		Description: fmt.Sprintf("%s has left the server.", m.User.Username),
		Color:       0xff0000, // Red
		Timestamp:   time.Now().Format(time.RFC3339),
		Thumbnail:   &discordgo.MessageEmbedThumbnail{URL: m.User.AvatarURL("")},
	}
	s.ChannelMessageSendEmbed(l.LogChannelID, embed)
	log.Printf("[EVENT] User Left: %s (%s)", m.User.Username, m.User.ID)
}
