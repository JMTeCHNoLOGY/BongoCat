package config

import (
	"errors"
	"flag"
	"fmt"
	"time"

	"bongocat-server/internal/protocol"
)

type Config struct {
	Listen                 string
	MaxRooms               int
	MaxPlayersPerRoom      int
	StreamMode             string
	MaxEventsPerSecond     int
	ContinuousHz           int
	MaxMessageBytes        int64
	ResumeGrace            time.Duration
	SnapshotIntervalMillis int
}

func Parse(args []string) (Config, error) {
	config := Config{}
	set := flag.NewFlagSet("bongocat-server", flag.ContinueOnError)

	set.StringVar(&config.Listen, "listen", ":8080", "HTTP listen address")
	set.IntVar(&config.MaxRooms, "max-rooms", 2, "maximum active rooms")
	set.IntVar(&config.MaxPlayersPerRoom, "max-players-per-room", 8, "maximum players per room")
	set.StringVar(&config.StreamMode, "stream-mode", protocol.StreamModeRaw, "input stream mode: raw or limited")
	set.IntVar(&config.MaxEventsPerSecond, "max-events-per-second", 512, "hard per-client input event rate")
	set.IntVar(&config.ContinuousHz, "continuous-hz", 20, "limited-mode continuous state rate")
	set.Int64Var(&config.MaxMessageBytes, "max-message-bytes", 16384, "maximum WebSocket message size")
	set.DurationVar(&config.ResumeGrace, "resume-grace", 15*time.Second, "disconnected session retention")

	if err := set.Parse(args); err != nil {
		return Config{}, err
	}

	config.SnapshotIntervalMillis = 1000

	if err := config.Validate(); err != nil {
		return Config{}, err
	}

	return config, nil
}

func (config Config) Validate() error {
	if config.Listen == "" {
		return errors.New("listen address is required")
	}
	if config.MaxRooms < 1 {
		return errors.New("max-rooms must be at least 1")
	}
	if config.MaxPlayersPerRoom < 1 {
		return errors.New("max-players-per-room must be at least 1")
	}
	if config.StreamMode != protocol.StreamModeRaw && config.StreamMode != protocol.StreamModeLimited {
		return fmt.Errorf("stream-mode must be %q or %q", protocol.StreamModeRaw, protocol.StreamModeLimited)
	}
	if config.MaxEventsPerSecond < 1 {
		return errors.New("max-events-per-second must be at least 1")
	}
	if config.ContinuousHz < 1 {
		return errors.New("continuous-hz must be at least 1")
	}
	if config.MaxMessageBytes < 1024 {
		return errors.New("max-message-bytes must be at least 1024")
	}
	if config.ResumeGrace < 0 {
		return errors.New("resume-grace cannot be negative")
	}

	return nil
}

func (config Config) Policy() protocol.RoomPolicy {
	return protocol.RoomPolicy{
		StreamMode:        config.StreamMode,
		MaxPlayers:        config.MaxPlayersPerRoom,
		MaxEventsPerSec:   config.MaxEventsPerSecond,
		ContinuousHz:      config.ContinuousHz,
		MaxMessageBytes:   config.MaxMessageBytes,
		SnapshotIntervalM: config.SnapshotIntervalMillis,
	}
}
