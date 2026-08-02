package yomesched

import (
	"context"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/u16-io/FindSenryu4Discord/pkg/logger"
	"github.com/u16-io/FindSenryu4Discord/service"
)

// SendFunc posts one random senryu. reactionMessageID may be empty.
type SendFunc func(s *discordgo.Session, channelID, guildID, reactionMessageID string) error

// Manager polls for due scheduled yomes and posts them.
type Manager struct {
	session   *discordgo.Session
	send      SendFunc
	interval  time.Duration
	stopCh    chan struct{}
	stoppedCh chan struct{}
}

// NewManager creates a scheduled-yome poller.
func NewManager(session *discordgo.Session, send SendFunc) *Manager {
	return &Manager{
		session:   session,
		send:      send,
		interval:  time.Second,
		stopCh:    make(chan struct{}),
		stoppedCh: make(chan struct{}),
	}
}

// Start begins polling in a background goroutine.
func (m *Manager) Start() {
	logger.Info("Starting scheduled yome manager")
	go m.run()
}

// Stop gracefully stops the poller.
func (m *Manager) Stop(ctx context.Context) {
	close(m.stopCh)
	select {
	case <-m.stoppedCh:
		logger.Info("Scheduled yome manager stopped")
	case <-ctx.Done():
		logger.Warn("Scheduled yome manager stop timeout")
	}
}

func (m *Manager) run() {
	defer close(m.stoppedCh)

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	// Fire any overdue pending jobs immediately on boot.
	m.tick()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.tick()
		}
	}
}

func (m *Manager) tick() {
	due, err := service.ListDuePendingScheduledYomes(time.Now())
	if err != nil {
		logger.Warn("Failed to list due scheduled yomes", "error", err)
		return
	}
	for _, yome := range due {
		m.fire(yome.ID, yome.GuildID, yome.ChannelID, yome.Count)
	}
}

func (m *Manager) fire(id int, guildID, channelID string, count int) {
	claimed, err := service.ClaimScheduledYomeDone(id)
	if err != nil {
		logger.Warn("Failed to claim scheduled yome", "error", err, "id", id)
		return
	}
	if !claimed {
		return
	}
	if count < 1 {
		count = 1
	}
	for i := 0; i < count; i++ {
		if err := m.send(m.session, channelID, guildID, ""); err != nil {
			logger.Warn("Failed to send scheduled yome",
				"error", err,
				"id", id,
				"guild_id", guildID,
				"channel_id", channelID,
			)
			break
		}
	}
}
