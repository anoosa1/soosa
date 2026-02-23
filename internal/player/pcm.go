package player

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"sync"
)

// PCMVolume wraps an io.Reader (providing standard int16 PCM audio) and scales the amplitude by a volume factor.

type PCMVolume struct {
	r        io.Reader
	volume   float64 // Linear volume: 0.0 to 1.0 (or higher for boost)
	mu       sync.Mutex
	leftover []byte // Buffer for partial sample (1 byte)
}

// NewPCMVolume creates a new PCMVolume reader. bufferSize should be aligned with the frame size (e.g., 960 stereo samples * 2 bytes = 3840 bytes).

func NewPCMVolume(r io.Reader) *PCMVolume {
	return &PCMVolume{
		r:      r,
		volume: 1.0,
	}
}

// SetVolume sets the volume factor (0.0 - 1.0).
func (v *PCMVolume) SetVolume(vol float64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.volume = math.Max(0, vol)
}

// Read implements io.Reader. It reads pcm data, scales it, and writes it to p.
func (v *PCMVolume) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	// 1. If we have a leftover byte from previous read, use it first
	startIdx := 0
	if len(v.leftover) > 0 {
		p[0] = v.leftover[0]
		v.leftover = nil
		startIdx = 1
		n = 1
		// If buffer was length 1, we are done
		if len(p) == 1 {
			return 1, nil
		}
	}

	// 2. Read from underlying source into the rest of p
	readN, err := v.r.Read(p[startIdx:])
	n += readN

	// 3. Handle leftover for NEXT read if we have an odd number of bytes total We need pairs of bytes to form int16 samples. If total bytes n is odd, the last byte is a partial sample.

	if n > 0 {
		// Save last byte if odd
		if n%2 != 0 {
			v.leftover = []byte{p[n-1]}
			n-- // Hide the partial byte from the consumer for now
		}

		// Now n is even. Process samples.
		v.mu.Lock()
		vol := v.volume
		v.mu.Unlock()

		// If volume is 1.0, we can skip processing (optimization)
		if math.Abs(vol-1.0) > 0.001 {
			// Process samples p contains n bytes of int16 little-endian data

			for i := 0; i+1 < n; i += 2 {
				// Read sample
				sample := int16(binary.LittleEndian.Uint16(p[i : i+2]))

				// Scale
				scaled := float64(sample) * vol

				// Clamp to int16 range
				if scaled > 32767 {
					scaled = 32767
				} else if scaled < -32768 {
					scaled = -32768
				}

				// Write back
				binary.LittleEndian.PutUint16(p[i:i+2], uint16(int16(scaled)))
			}
		}
	}

	// If we hid the last byte (n--), but err is EOF, we might have a problem? Actually, if err is EOF and we have a leftover byte, we return n (even) and nil error (because we still have data buffered). The next call will pick up the leftover byte. If that next call gets EOF from source immediately, it will return n=1 (the leftover) and EOF. But wait, if we return n < real_read, we shouldn't return EOF yet, or we should? Standard io.Reader: "Callers should always process the n > 0 bytes returned before considering the error err." So returning n, EOF is fine.

	return n, err
}

// WavHeader represents the parsed WAV header information we care about.
type WavHeader struct {
	AudioFormat   uint16
	NumChannels   uint16
	SampleRate    uint32
	ByteRate      uint32
	BlockAlign    uint16
	BitsPerSample uint16
}

// ReadWavHeader reads the RIFF WAVE header from the reader. It verifies the format and returns the header info. The reader is advanced past the header, ready to read PCM data.

func ReadWavHeader(r io.Reader) (*WavHeader, error) {
	// RIFF Header (12 bytes)
	riffHeader := make([]byte, 12)
	if _, err := io.ReadFull(r, riffHeader); err != nil {
		return nil, err
	}

	// 1. Check "RIFF"
	if string(riffHeader[0:4]) != "RIFF" {
		return nil, fmt.Errorf("invalid WAV: missing RIFF")
	}
	// 2. Check "WAVE"
	if string(riffHeader[8:12]) != "WAVE" {
		return nil, fmt.Errorf("invalid WAV: missing WAVE")
	}

	h := &WavHeader{}
	foundFmt := false

	// Iterate over chunks until we find "data"
	for {
		chunkHeader := make([]byte, 8)
		if _, err := io.ReadFull(r, chunkHeader); err != nil {
			return nil, err
		}

		chunkID := string(chunkHeader[0:4])
		chunkSize := binary.LittleEndian.Uint32(chunkHeader[4:8])

		if chunkID == "fmt " {
			if chunkSize < 16 {
				return nil, fmt.Errorf("invalid fmt chunk size: %d", chunkSize)
			}
			fmtData := make([]byte, chunkSize)
			if _, err := io.ReadFull(r, fmtData); err != nil {
				return nil, err
			}
			h.AudioFormat = binary.LittleEndian.Uint16(fmtData[0:2])
			h.NumChannels = binary.LittleEndian.Uint16(fmtData[2:4])
			h.SampleRate = binary.LittleEndian.Uint32(fmtData[4:8])
			h.ByteRate = binary.LittleEndian.Uint32(fmtData[8:12])
			h.BlockAlign = binary.LittleEndian.Uint16(fmtData[12:14])
			h.BitsPerSample = binary.LittleEndian.Uint16(fmtData[14:16])
			foundFmt = true
		} else if chunkID == "data" {
			if !foundFmt {
				return nil, fmt.Errorf("invalid WAV: data chunk before fmt chunk")
			}
			// We successfully found the data chunk. The reader is now positioned at the start of the audio data. Note: chunkSize tells us how much data is there, but for streaming it might be max uint32. We don't need to read it all now, just return.

			// Validate PCM
			if h.AudioFormat != 1 && h.AudioFormat != 0xFFFE { // 1 = PCM, 0xFFFE = Extensible (sometimes used for PCM)
				return nil, fmt.Errorf("unsupported WAV format: %d", h.AudioFormat)
			}

			return h, nil
		} else {
			// Skip unknown chunks (LIST, ID3, etc.) We must read and discard 'chunkSize' bytes

			if _, err := io.CopyN(io.Discard, r, int64(chunkSize)); err != nil {
				return nil, fmt.Errorf("failed to skip chunk %s: %v", chunkID, err)
			}
		}

		// WAV chunks are 2-byte aligned. If chunkSize is odd, there's a padding byte.
		if chunkSize%2 != 0 {
			io.CopyN(io.Discard, r, 1)
		}
	}
}
