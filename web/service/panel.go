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

// RestartPanel creates a new restart event in the shared DB with this panel's hostname,
// then restarts the local instance. Other panels will detect the new epoch and restart too.
func (s *PanelService) RestartPanel(delay time.Duration) error {
	epoch := time.Now().UnixMilli()
	hostname, _ := os.Hostname()
	db := database.GetDB()

	// Clean up old restart records (older than 1 hour)
	cutoff := epoch - 3600000
	db.Where("restart_epoch < ?", cutoff).Delete(&model.PanelRestart{})

	record := &model.PanelRestart{
		RestartEpoch: epoch,
		Hostname:     hostname,
		RestartedAt:  epoch,
	}
	if err := db.Create(record).Error; err != nil {
		logger.Warning("RestartPanel: failed to write restart signal:", err)
	} else {
		logger.Infof("RestartPanel: signal written (epoch=%d, hostname=%s). Other nodes will pick this up.", epoch, hostname)
	}

	s.lastRestartCheck = epoch
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

// CheckRemoteRestart polls the DB for restart requests from other panel instances.
// If a new restart epoch is found and this panel's hostname is not recorded for it,
// the hostname is added and a local restart is triggered.
func (s *PanelService) CheckRemoteRestart() bool {
	db := database.GetDB()
	if db == nil {
		return false
	}

	hostname, _ := os.Hostname()

	var maxEpoch int64
	row := db.Model(&model.PanelRestart{}).Select("COALESCE(MAX(restart_epoch), 0)").Row()
	if err := row.Scan(&maxEpoch); err != nil || maxEpoch == 0 {
		if s.lastRestartCheck == 0 {
			s.lastRestartCheck = time.Now().UnixMilli()
		}
		return false
	}

	if s.lastRestartCheck == 0 {
		s.lastRestartCheck = maxEpoch
		logger.Infof("CheckRemoteRestart: baseline set from DB (epoch=%d)", maxEpoch)
		return false
	}

	if maxEpoch <= s.lastRestartCheck {
		return false
	}

	// New restart epoch detected — check if our hostname already acknowledged it
	var count int64
	db.Model(&model.PanelRestart{}).
		Where("restart_epoch = ? AND hostname = ?", maxEpoch, hostname).
		Count(&count)

	if count > 0 {
		s.lastRestartCheck = maxEpoch
		return false
	}

	// Our hostname not found for this epoch — record it and restart
	record := &model.PanelRestart{
		RestartEpoch: maxEpoch,
		Hostname:     hostname,
		RestartedAt:  time.Now().UnixMilli(),
	}
	db.Create(record)

	logger.Infof("Remote restart detected! epoch=%d, hostname=%s not found — adding and restarting in 3s...", maxEpoch, hostname)
	s.lastRestartCheck = maxEpoch
	s.restartLocal(time.Second * 3)
	return true
}
