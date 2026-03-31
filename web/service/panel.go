package service

import (
	"os"
	"syscall"
	"time"

	"github.com/mhsanaei/3x-ui/v2/database"
	"github.com/mhsanaei/3x-ui/v2/database/model"
	"github.com/mhsanaei/3x-ui/v2/logger"
)

type PanelService struct {
	lastRestartCheck int64
}

// RestartPanel writes a restart signal to the shared DB (so other instances pick it up)
// and restarts the local instance after the given delay.
func (s *PanelService) RestartPanel(delay time.Duration) error {
	now := time.Now().UnixMilli()
	db := database.GetDB()

	if database.IsMySQL() {
		db.Exec(
			"INSERT INTO panel_restarts (id, requested_at) VALUES (1, ?) ON DUPLICATE KEY UPDATE requested_at = ?",
			now, now,
		)
	} else {
		var row model.PanelRestart
		if db.First(&row, 1).Error != nil {
			db.Create(&model.PanelRestart{Id: 1, RequestedAt: now})
		} else {
			db.Model(&row).Update("requested_at", now)
		}
	}

	return s.restartLocal(delay)
}

func (s *PanelService) restartLocal(delay time.Duration) error {
	p, err := os.FindProcess(syscall.Getpid())
	if err != nil {
		return err
	}
	go func() {
		time.Sleep(delay)
		if err := p.Signal(syscall.SIGHUP); err != nil {
			logger.Error("failed to send SIGHUP signal:", err)
		}
	}()
	return nil
}

// CheckRemoteRestart polls the DB for restart requests from other instances.
// Returns true if a restart was triggered.
func (s *PanelService) CheckRemoteRestart() bool {
	if s.lastRestartCheck == 0 {
		s.lastRestartCheck = time.Now().UnixMilli()
		return false
	}

	db := database.GetDB()
	var row model.PanelRestart
	if db.First(&row, 1).Error != nil {
		return false
	}

	if row.RequestedAt > s.lastRestartCheck {
		s.lastRestartCheck = row.RequestedAt
		logger.Info("Remote restart signal detected, restarting panel...")
		s.restartLocal(time.Second * 2)
		return true
	}
	return false
}
