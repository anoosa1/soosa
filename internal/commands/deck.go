package commands

import (
	"math/rand"
	"time"
)

// Card represents a playing card.
type Card struct {
	Suit  string
	Value string
}

// NewDeck creates a standard 52-card deck.
func NewDeck() []Card {
	suits := []string{"♠", "♥", "♦", "♣"}
	values := []string{"2", "3", "4", "5", "6", "7", "8", "9", "10", "J", "Q", "K", "A"}
	deck := []Card{}

	for _, s := range suits {
		for _, v := range values {
			deck = append(deck, Card{Suit: s, Value: v})
		}
	}
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(deck), func(i, j int) { deck[i], deck[j] = deck[j], deck[i] })
	return deck
}
