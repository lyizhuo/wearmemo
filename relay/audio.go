package main

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
)

var errNotWAV = errors.New("not a RIFF/WAVE file")

// rmsOfWAV returns the RMS amplitude of 16-bit PCM samples on a 0..32767 scale.
//
// This is our cheapest defence against the worst failure mode we observed in
// testing: a silent recording does not come back empty, it comes back as a
// confidently transcribed sentence in some arbitrary language. Rejecting near
// silence before spending 10 s of ASR also makes the common mis-tap instant.
func rmsOfWAV(path string) (float64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	if len(b) < 44 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return 0, errNotWAV
	}

	// Walk the chunk list instead of assuming a canonical 44-byte header.
	pos := 12
	for pos+8 <= len(b) {
		id := string(b[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(b[pos+4 : pos+8]))
		body := pos + 8
		if body+size > len(b) {
			size = len(b) - body
		}
		if id == "data" {
			samples := size / 2
			if samples == 0 {
				return 0, nil
			}
			var sum float64
			for i := 0; i < samples; i++ {
				v := float64(int16(binary.LittleEndian.Uint16(b[body+i*2 : body+i*2+2])))
				sum += v * v
			}
			return math.Sqrt(sum / float64(samples)), nil
		}
		pos = body + size + (size & 1) // chunks are word-aligned
	}
	return 0, errors.New("no data chunk")
}
