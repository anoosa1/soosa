package player

import (
	"log"
	"sync"

	"soosa/internal/discord"
	"soosa/internal/subsonic"

	"github.com/bwmarrin/discordgo"
)

// Manager tracks per-guild Player instances.
type Manager struct {
	players   map[string]*Player
	subClient *subsonic.Client
	session   *discordgo.Session
	Config    *discord.Config
	mu        sync.Mutex
}

// NewManager creates a new player Manager.
func NewManager(session *discordgo.Session, subClient *subsonic.Client, cfg *discord.Config) *Manager {
	return &Manager{
		players:   make(map[string]*Player),
		subClient: subClient,
		session:   session,
		Config:    cfg,
	}
}

// GetPlayer returns the player for a guild, or nil if none exists.
func (m *Manager) GetPlayer(guildID string) *Player {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.players[guildID]
}

// GetOrCreatePlayer returns the existing player or creates a new one.
func (m *Manager) GetOrCreatePlayer(guildID string) *Player {
	m.mu.Lock()
	defer m.mu.Unlock()

	if p, ok := m.players[guildID]; ok {
		return p
	}

	p := NewPlayer(m.session, m.subClient, guildID, m.Config)
	m.players[guildID] = p
	log.Printf("[PLAYER] Created new player for Guild %s", guildID)
	return p
}

// Remove stops and removes the player for a guild.
func (m *Manager) Remove(guildID string) {
	m.mu.Lock()
	p, ok := m.players[guildID]
	if ok {
		delete(m.players, guildID)
	}
	m.mu.Unlock()

	if ok && p != nil {
		p.Stop()
	}
}

// GetSubsonicClient returns the Subsonic client.
func (m *Manager) GetSubsonicClient() *subsonic.Client {
	return m.subClient
}

// Shutdown stops all players and releases resources.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, p := range m.players {
		go p.Stop() // Stop in parallel to speed up shutdown
	}
	// Wait? For now just fire and forget, main will exit soon. Ideally we'd wait, but p.Stop() blocks on p.done which is good.

}
