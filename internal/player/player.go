package player

import (
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"soosa/internal/discord"
	"soosa/internal/subsonic"

	"github.com/bwmarrin/discordgo"
)

// Player manages audio playback for a single guild.
type Player struct {
	GuildID    string
	VoiceConn  *discordgo.VoiceConnection
	Queue      []*subsonic.Song
	NowPlaying *subsonic.Song
	IsPlaying  bool
	IsPaused   bool
	Volume     int // 0-256, default 256

	History       []*subsonic.Song
	LastMsgID     string
	LastChannelID string

	stopChan         chan struct{}
	skipChan         chan struct{}
	volumeChangeChan chan struct{} // Signal to update PCM volume (does not restart stream)
	done             chan struct{}

	encoding *EncodeSession
	stream   *StreamingSession

	session   *discordgo.Session
	subClient *subsonic.Client
	Config    *discord.Config
	mu        sync.Mutex
}

// NewPlayer creates a new Player for a guild.
func NewPlayer(session *discordgo.Session, subClient *subsonic.Client, guildID string, cfg *discord.Config) *Player {
	return &Player{
		GuildID:          guildID,
		Volume:           cfg.DefaultVolume,
		stopChan:         make(chan struct{}),
		skipChan:         make(chan struct{}),
		volumeChangeChan: make(chan struct{}, 1), // Buffered to avoid blocking
		done:             make(chan struct{}),
		session:          session,
		subClient:        subClient,
		Config:           cfg,
	}
}

// SetLastMessage updates the last message ID and channel ID.
func (p *Player) SetLastMessage(msgID, channelID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.LastMsgID = msgID
	p.LastChannelID = channelID
}

// Enqueue adds a song to the queue.
func (p *Player) Enqueue(song *subsonic.Song) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Queue = append(p.Queue, song)
	log.Printf("[PLAYER] Enqueued: %s - %s", song.Artist, song.Title)
}

// GetQueue returns a copy of the current queue.
func (p *Player) GetQueue() []*subsonic.Song {
	p.mu.Lock()
	defer p.mu.Unlock()
	q := make([]*subsonic.Song, len(p.Queue))
	copy(q, p.Queue)
	return q
}

// dequeue removes and returns the next song, or nil if empty.
func (p *Player) dequeue() *subsonic.Song {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.Queue) == 0 {
		return nil
	}
	song := p.Queue[0]
	p.Queue = p.Queue[1:]
	return song
}

// Start joins the voice channel and begins the playback loop.
func (p *Player) Start(channelID string) error {
	p.mu.Lock()
	if p.IsPlaying {
		p.mu.Unlock()
		return nil
	}
	p.IsPlaying = true
	p.mu.Unlock()

	vc, err := p.session.ChannelVoiceJoin(p.GuildID, channelID, false, true)
	if err != nil {
		p.mu.Lock()
		p.IsPlaying = false
		p.mu.Unlock()
		return fmt.Errorf("failed to join voice channel: %w", err)
	}
	p.VoiceConn = vc

	// Give Discord a moment to establish the voice connection
	time.Sleep(250 * time.Millisecond)

	go p.playLoop()
	return nil
}

// Stop stops playback, clears the queue, and leaves the voice channel.
func (p *Player) Stop() {
	p.mu.Lock()
	p.Queue = nil
	p.mu.Unlock()

	// Signal stop
	select {
	case <-p.stopChan:
		// already closed
	default:
		close(p.stopChan)
	}

	// Wait for playback loop to finish
	<-p.done

	if p.VoiceConn != nil {
		p.VoiceConn.Disconnect()
		p.VoiceConn = nil
	}

	log.Printf("[PLAYER] Stopped playback")

	p.mu.Lock()
	p.IsPlaying = false
	p.NowPlaying = nil

	if p.LastMsgID != "" && p.LastChannelID != "" {
		p.session.ChannelMessageEdit(p.LastChannelID, p.LastMsgID, "⏹️ **Stopped.**")
		p.LastMsgID = ""
		p.LastChannelID = ""
	}
	p.mu.Unlock()
}

// Skip skips the current song by closing the skip channel.
func (p *Player) Skip() {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Close the current skip channel to signal skip, then make a new one
	select {
	case <-p.skipChan:
		// already closed
	default:
		close(p.skipChan)
	}
	log.Printf("[PLAYER] Skipped song")
}

// PlayPrevious skips to the previous song in history.
func (p *Player) PlayPrevious() {
	p.mu.Lock()
	if len(p.History) == 0 {
		p.mu.Unlock()
		return
	}

	// Pop last song from history
	prevSong := p.History[len(p.History)-1]
	p.History = p.History[:len(p.History)-1]

	// Push current NowPlaying to front of Queue (if exists) so we don't lose it
	if p.NowPlaying != nil {
		p.Queue = append([]*subsonic.Song{p.NowPlaying}, p.Queue...)
	}
	// Push prevSong to the very front
	p.Queue = append([]*subsonic.Song{prevSong}, p.Queue...)
	p.mu.Unlock()

	// Skip current song to immediately play the one we just pushed
	p.Skip()
}

// Pause pauses playback.
func (p *Player) Pause() {
	p.mu.Lock()
	p.IsPaused = true
	p.mu.Unlock()

	p.mu.Lock()
	s := p.stream
	p.mu.Unlock()
	if s != nil {
		s.SetPaused(true)
	}
	log.Printf("[PLAYER] Paused playback")
}

// Resume resumes playback.
func (p *Player) Resume() {
	p.mu.Lock()
	p.IsPaused = false
	p.mu.Unlock()

	p.mu.Lock()
	s := p.stream
	p.mu.Unlock()
	if s != nil {
		s.SetPaused(false)
	}
	log.Printf("[PLAYER] Resumed playback")
}

// SetVolume sets volume (0-256).
func (p *Player) SetVolume(vol int) {
	if vol < 0 {
		vol = 0
	}
	if vol > 256 {
		vol = 256
	}
	p.mu.Lock()
	p.Volume = vol
	p.mu.Unlock()

	// Signal volume change if possible
	select {
	case p.volumeChangeChan <- struct{}{}:
	default:
		// Channel already full (restart pending), no need to block
	}
}

// playLoop continuously dequeues and plays songs until stopped or queue is empty.
func (p *Player) playLoop() {
	defer close(p.done)

	for {
		// Check for stop signal
		select {
		case <-p.stopChan:
			return
		default:
		}

		song := p.dequeue()
		if song == nil {
			// Queue empty — disconnect and stop
			p.mu.Lock()
			p.IsPlaying = false
			p.NowPlaying = nil
			p.mu.Unlock()

			if p.VoiceConn != nil {
				p.VoiceConn.Disconnect()
				p.VoiceConn = nil
			}

			// Update last message to say queue ended
			p.mu.Lock()
			if p.LastMsgID != "" && p.LastChannelID != "" {
				// We need to use the session to edit. Note: session is safe to use here.

				p.session.ChannelMessageEdit(p.LastChannelID, p.LastMsgID, "⏹️ **Queue ended.**")
				p.LastMsgID = ""
				p.LastChannelID = ""
			}
			p.mu.Unlock()

			return
		}

		p.mu.Lock()
		p.NowPlaying = song
		p.IsPlaying = true
		p.mu.Unlock()

		log.Printf("Now playing: %s - %s", song.Artist, song.Title)
		p.streamSong(song)

		// Song finished (either naturally or skipped)
		p.mu.Lock()
		// Add to history
		p.History = append(p.History, song)
		if len(p.History) > 50 {
			p.History = p.History[1:]
		}
		p.mu.Unlock()

		// After a song finishes, recreate skip channel for the next song
		p.mu.Lock()
		// Ensure skip channel is open (reset) for next song
		p.skipChan = make(chan struct{})
		// Drain volume channel to clear unexpected signals between songs
	loop:
		for {
			select {
			case <-p.volumeChangeChan:
			default:
				break loop
			}
		}
		p.mu.Unlock()
	}
}

// streamSong streams a single song to the voice connection using dca and a custom PCM pipeline.
func (p *Player) streamSong(song *subsonic.Song) {
	// Request transcoding to WAV for direct PCM streaming (includes header for sample rate)
	streamURL := p.subClient.StreamURL(song.ID) + "&format=wav"

	defer p.VoiceConn.Speaking(false) // Ensure speaking is disabled on exit

	// 1. Fetch audio stream securely
	resp, err := p.subClient.StreamHTTP.Get(streamURL)
	if err != nil {
		log.Printf("[PLAYER] ERROR: Failed to fetch stream: %v", err)
		return
	}
	defer resp.Body.Close()

	// 2. Parse WAV Header to get sample rate and advance reader
	wavHeader, err := ReadWavHeader(resp.Body)
	if err != nil {
		log.Printf("[PLAYER] ERROR: Failed to parse WAV header: %v", err)
		return
	}
	log.Printf("[PLAYER] Stream Info: %d Hz, %d channels, %d bits/sample",
		wavHeader.SampleRate, wavHeader.NumChannels, wavHeader.BitsPerSample)

	// 3. Setup PCM Scaler directly on the response body
	pcmScaler := NewPCMVolume(resp.Body)

	// Frame size: 960 samples * 2 channels * 2 bytes = 3840 bytes (at 48kHz)
	const frameSize = 3840

	// Create a pipe to connect Scaler output TO Encoder input
	pipeReader, pipeWriter := io.Pipe()
	go func() {
		log.Printf("[PLAYER] PCM Pump started")
		defer pipeWriter.Close()
		buf := make([]byte, frameSize)
		totalBytes := 0
		for {
			n, err := pcmScaler.Read(buf)
			if n > 0 {
				totalBytes += n
				if _, wErr := pipeWriter.Write(buf[:n]); wErr != nil {
					// Pipe closed or error
					log.Printf("[PLAYER] Pipe write error: %v", wErr)
					break
				}
			}
			if err != nil {
				log.Printf("[PLAYER] PCM Scaler read error (EOF?): %v. Total bytes pumped: %d", err, totalBytes)
				break
			}
		}
		log.Printf("[PLAYER] PCM Pump finished")
	}()

	// 4. Start Encoding Process (FFmpeg 2) with RawInput to tell dca to add -f s16le args
	encodeOpts := *StdEncodeOptions
	encodeOpts.RawOutput = true  // We want raw Opus for Discord
	encodeOpts.RawInput = true   // We are feeding raw PCM
	encodeOpts.PCMOutput = false // Ensure we are NOT outputting PCM
	encodeOpts.Bitrate = p.Config.FFmpegBitrate
	encodeOpts.Application = "audio"
	encodeOpts.CompressionLevel = p.Config.CompressionLevel
	encodeOpts.FrameRate = int(wavHeader.SampleRate) // Tell ffmpeg the INPUT sample rate
	encodeOpts.Channels = 2
	encodeOpts.FrameDuration = 20
	encodeOpts.BufferedFrames = 0 // Extremely low buffer for instant volume changes

	encodingSession, err := EncodeMem(pipeReader, &encodeOpts)
	if err != nil {
		log.Printf("[PLAYER] ERROR: dca.EncodeMem (encode) failed: %v", err)
		return
	}

	p.mu.Lock()
	p.encoding = encodingSession
	p.mu.Unlock()

	// Clean up session
	cleanupSession := func() {
		encodingSession.Cleanup()
		p.mu.Lock()
		if p.encoding == encodingSession {
			p.encoding = nil
			p.stream = nil
		}
		p.mu.Unlock()
	}

	p.VoiceConn.Speaking(true)

	// Update volume initially
	p.mu.Lock()
	currentVol := float64(p.Volume) / 256.0
	p.mu.Unlock()
	pcmScaler.SetVolume(currentVol)

	// 5. Send Opus to Discord
	doneChan := make(chan error)
	stream := NewStream(encodingSession, p.VoiceConn, doneChan)

	p.mu.Lock()
	p.stream = stream
	p.mu.Unlock()

	shouldReturn := false

	// Control Loop
	for {
		select {
		case <-p.stopChan:
			log.Printf("[PLAYER] Stop signal received")
			shouldReturn = true
		case <-p.skipChan:
			log.Printf("[PLAYER] Skip signal received")
			shouldReturn = true
		case <-p.volumeChangeChan:
			// Just update scaler volume!
			p.mu.Lock()
			newVol := float64(p.Volume) / 256.0
			p.mu.Unlock()
			pcmScaler.SetVolume(newVol)
			log.Printf("[PLAYER] Volume updated to %.2f (instant)", newVol)
			// Clean channel continue loop

		case err := <-doneChan:
			if err != nil && err != io.EOF {
				log.Printf("[PLAYER] Stream ended with error: %v", err)
			} else {
				log.Printf("[PLAYER] Stream finished normally")
			}
			shouldReturn = true
		}

		if shouldReturn {
			break
		}
	}

	cleanupSession()
	p.VoiceConn.Speaking(false)

	if shouldReturn {
		return
	}
	// If NOT returning (natural finish), we loop back -> StartTime increased -> New Encoding created Wait, start time won't increase automatically unless we track it And usually natural finish means song done.

}

// FormatDuration formats seconds into MM:SS.
func FormatDuration(seconds int) string {
	m := seconds / 60
	s := seconds % 60
	return fmt.Sprintf("%d:%02d", m, s)
}
