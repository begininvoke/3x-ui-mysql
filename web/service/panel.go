package service

import (
	"os"
	"syscall"
	"time"

	"github.com/mhsanaei/3x-ui/v2/database"
	"github.com/mhsanaei/3x-ui/v2/database/model"
	"github.com/mhsanaei/3x-ui/v2/logger"

	"gorm.io/gorm"
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
		err := db.Exec(
			"INSERT INTO panel_restarts (id, requested_at) VALUES (1, ?) ON DUPLICATE KEY UPDATE requested_at = ?",
			now, now,
		).Error
		if err != nil {
			logger.Warning("RestartPanel: failed to write restart signal to MySQL:", err)
		} else {
			logger.Infof("RestartPanel: signal written to DB (requested_at=%d). All nodes will pick this up.", now)
		}
	} else {
		result := db.Model(&model.PanelRestart{}).Where("id = 1").Update("requested_at", now)
		if result.RowsAffected == 0 {
			db.Create(&model.PanelRestart{Id: 1, RequestedAt: now})
		}
		logger.Infof("RestartPanel: signal written to DB (requested_at=%d)", now)
	}

	// Verify the write succeeded by reading it back
	var verify model.PanelRestart
	if err := db.First(&verify, 1).Error; err != nil {
		logger.Warning("RestartPanel: verification read failed:", err)
	} else {
		logger.Infof("RestartPanel: verified DB value requested_at=%d", verify.RequestedAt)
	}

	s.lastRestartCheck = now
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
	db := database.GetDB()
	if db == nil {
		logger.Debug("CheckRemoteRestart: db is nil, skipping")
		return false
	}

	var row model.PanelRestart
	result := db.First(&row, 1)
	if result.Error != nil {
		if result.Error != gorm.ErrRecordNotFound {
			logger.Warningf("CheckRemoteRestart: DB query error: %v", result.Error)
		}
		if s.lastRestartCheck == 0 {
			s.lastRestartCheck = time.Now().UnixMilli()
			logger.Infof("CheckRemoteRestart: no DB record yet, baseline set to now (%d)", s.lastRestartCheck)
		}
		return false
	}

	if s.lastRestartCheck == 0 {
		s.lastRestartCheck = row.RequestedAt
		logger.Infof("CheckRemoteRestart: initialized baseline from DB (requested_at=%d)", s.lastRestartCheck)
		return false
	}

	if row.RequestedAt > s.lastRestartCheck {
		logger.Infof("Remote restart detected! DB requested_at=%d > local baseline=%d. Restarting in 3s...", row.RequestedAt, s.lastRestartCheck)
		s.lastRestartCheck = row.RequestedAt
		s.restartLocal(time.Second * 3)
		return true
	}
	return false
}
