package database

import (
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"

	"log"
)

type DB struct {
	conn *sql.DB
}

// New initializes the database connection and creates the schema.
func New(dsn string) (*DB, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	d := &DB{conn: db}
	if err := d.migrate(); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	log.Printf("Database connected: %s", dsn)
	return d, nil
}

func (d *DB) migrate() error {
	// Permissions table
	_, err := d.conn.Exec(`
	CREATE TABLE IF NOT EXISTS permissions (
		user_id TEXT NOT NULL,
		node TEXT NOT NULL,
		PRIMARY KEY (user_id, node)
	);`)
	if err != nil {
		return err
	}

	// Economy table
	_, err = d.conn.Exec(`
	CREATE TABLE IF NOT EXISTS economy (
		user_id TEXT PRIMARY KEY,
		balance INTEGER DEFAULT 0
	);`)
	if err != nil {
		return err
	}

	// Wordle Stats table
	_, err = d.conn.Exec(`
	CREATE TABLE IF NOT EXISTS wordle_stats (
		user_id TEXT PRIMARY KEY,
		games_played INTEGER DEFAULT 0,
		games_won INTEGER DEFAULT 0,
		current_streak INTEGER DEFAULT 0,
		max_streak INTEGER DEFAULT 0,
		distribution TEXT DEFAULT '{}'
	);`)
	if err != nil {
		return err
	}

	// Wordle State table
	_, err = d.conn.Exec(`
	CREATE TABLE IF NOT EXISTS wordle_state (
		user_id TEXT PRIMARY KEY,
		game_type TEXT NOT NULL,
		word TEXT NOT NULL,
		guesses TEXT NOT NULL,
		last_played TEXT NOT NULL,
		completed BOOLEAN NOT NULL DEFAULT 0,
		message_id TEXT DEFAULT '',
		channel_id TEXT DEFAULT ''
	);`)
	if err != nil {
		return err
	}

	// Add new columns to existing table if they don't exist
	_, _ = d.conn.Exec(`ALTER TABLE wordle_state ADD COLUMN message_id TEXT DEFAULT '';`)
	_, _ = d.conn.Exec(`ALTER TABLE wordle_state ADD COLUMN channel_id TEXT DEFAULT '';`)

	return nil
}

func (d *DB) Close() error {
	log.Println("Database connection closing.")
	return d.conn.Close()
}

// AddPermission grants a permission node to a user.
func (d *DB) AddPermission(userID, node string) error {
	_, err := d.conn.Exec("INSERT OR IGNORE INTO permissions (user_id, node) VALUES (?, ?)", userID, node)
	return err
}

// RemovePermission revokes a permission node from a user.
func (d *DB) RemovePermission(userID, node string) error {
	_, err := d.conn.Exec("DELETE FROM permissions WHERE user_id = ? AND node = ?", userID, node)
	return err
}

// HasPermission checks if a user has a specific permission node.
func (d *DB) HasPermission(userID, node string) (bool, error) {
	var exists int
	err := d.conn.QueryRow("SELECT 1 FROM permissions WHERE user_id = ? AND node = ?", userID, node).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ListPermissions returns all permission nodes for a user.
func (d *DB) ListPermissions(userID string) ([]string, error) {
	rows, err := d.conn.Query("SELECT node FROM permissions WHERE user_id = ?", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []string
	for rows.Next() {
		var node string
		if err := rows.Scan(&node); err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

// Economy Methods

// GetBalance returns the balance of a user.
func (d *DB) GetBalance(userID string) (int, error) {
	var balance int
	err := d.conn.QueryRow("SELECT balance FROM economy WHERE user_id = ?", userID).Scan(&balance)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return balance, nil
}

// SetBalance sets the balance of a user.
func (d *DB) SetBalance(userID string, amount int) error {
	_, err := d.conn.Exec("INSERT INTO economy (user_id, balance) VALUES (?, ?) ON CONFLICT(user_id) DO UPDATE SET balance = ?", userID, amount, amount)
	return err
}

// AddBalance adds (or subtracts) an amount from a user's balance.
func (d *DB) AddBalance(userID string, amount int) error {
	_, err := d.conn.Exec("INSERT INTO economy (user_id, balance) VALUES (?, ?) ON CONFLICT(user_id) DO UPDATE SET balance = balance + ?", userID, amount, amount)
	return err
}

// Transfer moves money from one user to another.
func (d *DB) Transfer(fromID, toID string, amount int) error {
	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Atomic check and update
	res, err := tx.Exec("UPDATE economy SET balance = balance - ? WHERE user_id = ? AND balance >= ?", amount, fromID, amount)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		// Either user doesn't exist or insufficient funds We can disambiguate if needed, but "insufficient funds" covers both for transfer purposes usually

		return fmt.Errorf("insufficient funds")
	}

	// Update receiver
	_, err = tx.Exec("INSERT INTO economy (user_id, balance) VALUES (?, ?) ON CONFLICT(user_id) DO UPDATE SET balance = balance + ?", toID, amount, amount)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// Wordle Methods

type WordleStats struct {
	UserID        string
	GamesPlayed   int
	GamesWon      int
	CurrentStreak int
	MaxStreak     int
	Distribution  map[int]int
}

type WordleState struct {
	UserID     string
	GameType   string
	Word       string
	Guesses    []string
	LastPlayed string
	Completed  bool
	MessageID  string
	ChannelID  string
}

func (d *DB) GetWordleStats(userID string) (*WordleStats, error) {
	stats := &WordleStats{UserID: userID, Distribution: make(map[int]int)}
	var distStr string
	err := d.conn.QueryRow("SELECT games_played, games_won, current_streak, max_streak, distribution FROM wordle_stats WHERE user_id = ?", userID).Scan(
		&stats.GamesPlayed, &stats.GamesWon, &stats.CurrentStreak, &stats.MaxStreak, &distStr,
	)
	if err == sql.ErrNoRows {
		return stats, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(distStr), &stats.Distribution); err != nil {
		return nil, fmt.Errorf("failed to unmarshal distribution: %w", err)
	}
	if stats.Distribution == nil {
		stats.Distribution = make(map[int]int)
	}
	return stats, nil
}

func (d *DB) UpdateWordleStats(stats *WordleStats) error {
	distBytes, err := json.Marshal(stats.Distribution)
	if err != nil {
		return fmt.Errorf("failed to marshal distribution: %w", err)
	}
	_, err = d.conn.Exec(`
		INSERT INTO wordle_stats (user_id, games_played, games_won, current_streak, max_streak, distribution)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
		games_played = excluded.games_played,
		games_won = excluded.games_won,
		current_streak = excluded.current_streak,
		max_streak = excluded.max_streak,
		distribution = excluded.distribution
	`, stats.UserID, stats.GamesPlayed, stats.GamesWon, stats.CurrentStreak, stats.MaxStreak, string(distBytes))
	return err
}

func (d *DB) GetWordleState(userID string) (*WordleState, error) {
	state := &WordleState{UserID: userID}
	var guessesStr string
	err := d.conn.QueryRow("SELECT game_type, word, guesses, last_played, completed, message_id, channel_id FROM wordle_state WHERE user_id = ?", userID).Scan(
		&state.GameType, &state.Word, &guessesStr, &state.LastPlayed, &state.Completed, &state.MessageID, &state.ChannelID,
	)
	if err == sql.ErrNoRows {
		return nil, nil // No active state
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(guessesStr), &state.Guesses); err != nil {
		return nil, fmt.Errorf("failed to unmarshal guesses: %w", err)
	}
	return state, nil
}

func (d *DB) SaveWordleState(state *WordleState) error {
	guessesBytes, err := json.Marshal(state.Guesses)
	if err != nil {
		return fmt.Errorf("failed to marshal guesses: %w", err)
	}
	_, err = d.conn.Exec(`
		INSERT INTO wordle_state (user_id, game_type, word, guesses, last_played, completed, message_id, channel_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
		game_type = excluded.game_type,
		word = excluded.word,
		guesses = excluded.guesses,
		last_played = excluded.last_played,
		completed = excluded.completed,
		message_id = excluded.message_id,
		channel_id = excluded.channel_id
	`, state.UserID, state.GameType, state.Word, string(guessesBytes), state.LastPlayed, state.Completed, state.MessageID, state.ChannelID)
	return err
}
