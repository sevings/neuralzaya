package zaya

import (
	"sync"
	"time"

	tele "gopkg.in/telebot.v3"
)

type ProcessAlbum func(contexts []tele.Context, forceKeepHistory, mentioned bool) error

type AlbumTimer struct {
	Timer     *time.Timer
	Created   time.Time
	Mentioned bool
	ForceKeep bool
	Contexts  []tele.Context
}

type AlbumCache struct {
	mu      sync.RWMutex
	process ProcessAlbum
	timers  map[string]*AlbumTimer
}

func NewAlbumCache(process ProcessAlbum) *AlbumCache {
	return &AlbumCache{
		process: process,
		timers:  make(map[string]*AlbumTimer),
	}
}

func (ac *AlbumCache) AddMessage(c tele.Context, forceKeepHistory, mentioned bool) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	albumID := c.Message().AlbumID
	albumTimer, exists := ac.timers[albumID]
	if exists {
		albumTimer.Timer.Stop()
		albumTimer.Contexts = append(albumTimer.Contexts, c)
		albumTimer.Timer.Reset(2 * time.Second)
	} else {
		albumTimer = &AlbumTimer{
			Created:   time.Now(),
			Contexts:  []tele.Context{c},
			Mentioned: mentioned,
			ForceKeep: forceKeepHistory,
		}

		albumTimer.Timer = time.AfterFunc(2*time.Second, func() {
			ac.processAlbum(albumID)
		})

		ac.timers[albumID] = albumTimer
	}
}

func (ac *AlbumCache) processAlbum(albumID string) {
	ac.mu.Lock()
	albumTimer, exists := ac.timers[albumID]
	if !exists {
		ac.mu.Unlock()
		return
	}

	delete(ac.timers, albumID)
	ac.mu.Unlock()

	if len(albumTimer.Contexts) > 0 {
		ac.process(albumTimer.Contexts, albumTimer.ForceKeep, albumTimer.Mentioned)
	}
}

func (ac *AlbumCache) CleanOld(maxAge time.Duration) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	now := time.Now()
	for albumID, albumTimer := range ac.timers {
		if now.Sub(albumTimer.Created) > maxAge {
			albumTimer.Timer.Stop()
			delete(ac.timers, albumID)
		}
	}
}

func (ac *AlbumCache) CleanAll() {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	for _, albumTimer := range ac.timers {
		albumTimer.Timer.Stop()
	}
	ac.timers = make(map[string]*AlbumTimer)
}
