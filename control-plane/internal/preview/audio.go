package preview

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const maxAudioInteger int64 = 9007199254740991

func AudioIntervalFromStream(startPTS, durationTS int64, timeBase string) (*AudioInterval, error) {
	parts := strings.Split(timeBase, "/")
	if len(parts) != 2 {
		return nil, errors.New("missing audio stream time base")
	}
	for _, part := range parts {
		if part == "" || strings.IndexFunc(part, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			return nil, errors.New("invalid audio stream time base")
		}
	}
	num, nerr := strconv.ParseInt(parts[0], 10, 64)
	den, derr := strconv.ParseInt(parts[1], 10, 64)
	if nerr != nil || derr != nil || num <= 0 || den <= 0 || num > maxAudioInteger || den > maxAudioInteger ||
		startPTS < -maxAudioInteger || startPTS > maxAudioInteger || durationTS <= 0 || durationTS > maxAudioInteger {
		return nil, errors.New("invalid audio stream timestamps/time base")
	}
	unit := float64(num) / float64(den)
	return &AudioInterval{StartPTS: startPTS, DurationTS: durationTS, TimeBase: timeBase,
		StartSeconds: float64(startPTS) * unit, DurationSeconds: float64(durationTS) * unit}, nil
}

// ValidateAudioInterval checks demuxed presentation timing. It does not require
// AAC decoded sample counts (which may include priming/padding) to equal target.
func ValidateAudioInterval(interval *AudioInterval, targetSamples int64, sampleRate int, fps float64) error {
	if interval == nil || targetSamples <= 0 || targetSamples > maxAudioInteger || sampleRate <= 0 || int64(sampleRate) > maxAudioInteger ||
		math.IsNaN(fps) || math.IsInf(fps, 0) || fps <= 0 {
		return errors.New("missing audio interval or target")
	}
	actual, err := AudioIntervalFromStream(interval.StartPTS, interval.DurationTS, interval.TimeBase)
	if err != nil {
		return err
	}
	finite := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
	if !finite(interval.StartSeconds) || !finite(interval.DurationSeconds) ||
		math.Abs(interval.StartSeconds-actual.StartSeconds) > 1/float64(sampleRate) ||
		math.Abs(interval.DurationSeconds-actual.DurationSeconds) > 1/float64(sampleRate) {
		return errors.New("audio interval disagrees with stream timestamps")
	}
	target := float64(targetSamples) / float64(sampleRate)
	tolerance := math.Max(1024/float64(sampleRate), 1/fps) + 1/float64(sampleRate)
	end := actual.StartSeconds + actual.DurationSeconds
	if math.Abs(actual.StartSeconds) > tolerance || math.Abs(actual.DurationSeconds-target) > tolerance || math.Abs(end-target) > tolerance {
		return fmt.Errorf("audio stream interval %g..%gs does not cover target 0..%gs", actual.StartSeconds, end, target)
	}
	return nil
}
