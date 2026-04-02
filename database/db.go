// Package database provides database initialization, migration, and management utilities
// for the 3x-ui panel using GORM with SQLite or MySQL.
package database

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v2/config"
	"github.com/mhsanaei/3x-ui/v2/database/model"
	"github.com/mhsanaei/3x-ui/v2/util/crypto"
	"github.com/mhsanaei/3x-ui/v2/xray"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var db *gorm.DB
var dbType string

const (
	defaultUsername = "admin"
	defaultPassword = "admin"
)

func initModels() error {
	models := []any{
		&model.User{},
		&model.Inbound{},
		&model.OutboundTraffics{},
		&model.Setting{},
		&model.InboundClientIps{},
		&xray.ClientTraffic{},
		&model.HistoryOfSeeders{},
		&model.TrafficDaily{},
		&model.PanelRestart{},
		&model.BlockedIP{},
	}
	for _, model := range models {
		if err := db.AutoMigrate(model); err != nil {
			log.Printf("Error auto migrating model: %v", err)
			return err
		}
	}
	return nil
}

// initUser creates a default admin user if the users table is empty.
func initUser() error {
	empty, err := isTableEmpty("users")
	if err != nil {
		log.Printf("Error checking if users table is empty: %v", err)
		return err
	}
	if empty {
		hashedPassword, err := crypto.HashPasswordAsBcrypt(defaultPassword)

		if err != nil {
			log.Printf("Error hashing default password: %v", err)
			return err
		}

		user := &model.User{
			Username: defaultUsername,
			Password: hashedPassword,
		}
		return db.Create(user).Error
	}
	return nil
}

// runSeeders migrates user passwords to bcrypt and records seeder execution to prevent re-running.
func runSeeders(isUsersEmpty bool) error {
	empty, err := isTableEmpty("history_of_seeders")
	if err != nil {
		log.Printf("Error checking if users table is empty: %v", err)
		return err
	}

	if empty && isUsersEmpty {
		hashSeeder := &model.HistoryOfSeeders{
			SeederName: "UserPasswordHash",
		}
		return db.Create(hashSeeder).Error
	} else {
		var seedersHistory []string
		db.Model(&model.HistoryOfSeeders{}).Pluck("seeder_name", &seedersHistory)

		if !slices.Contains(seedersHistory, "UserPasswordHash") && !isUsersEmpty {
			var users []model.User
			db.Find(&users)

			for _, user := range users {
				hashedPassword, err := crypto.HashPasswordAsBcrypt(user.Password)
				if err != nil {
					log.Printf("Error hashing password for user '%s': %v", user.Username, err)
					return err
				}
				db.Model(&user).Update("password", hashedPassword)
			}

			hashSeeder := &model.HistoryOfSeeders{
				SeederName: "UserPasswordHash",
			}
			return db.Create(hashSeeder).Error
		}
	}

	return nil
}

// isTableEmpty returns true if the named table contains zero rows.
func isTableEmpty(tableName string) (bool, error) {
	var count int64
	err := db.Table(tableName).Count(&count).Error
	return count == 0, err
}

// InitDB sets up the database connection, migrates models, and runs seeders.
// It auto-detects whether to use SQLite or MySQL based on config.GetDBType().
func InitDB(dbPath string) error {
	dbType = config.GetDBType()

	var gormLogger logger.Interface
	if config.IsDebug() {
		gormLogger = logger.Default
	} else {
		gormLogger = logger.Discard
	}

	c := &gorm.Config{
		Logger: gormLogger,
	}

	var err error

	switch dbType {
	case "mysql":
		dsn := config.GetMySQLDSN()
		db, err = gorm.Open(mysql.Open(dsn), c)
		if err != nil {
			return fmt.Errorf("failed to connect to MySQL: %w", err)
		}

		sqlDB, poolErr := db.DB()
		if poolErr != nil {
			return fmt.Errorf("failed to get underlying sql.DB: %w", poolErr)
		}
		sqlDB.SetMaxOpenConns(config.GetMySQLMaxOpenConns())
		sqlDB.SetMaxIdleConns(config.GetMySQLMaxIdleConns())
		sqlDB.SetConnMaxLifetime(time.Duration(config.GetMySQLConnMaxLifetimeSec()) * time.Second)
		sqlDB.SetConnMaxIdleTime(5 * time.Minute)

		log.Printf("Connected to MySQL database at %s:%d/%s (pool: maxOpen=%d, maxIdle=%d, lifetime=%ds)",
			config.GetMySQLHost(), config.GetMySQLPort(), config.GetMySQLDBName(),
			config.GetMySQLMaxOpenConns(), config.GetMySQLMaxIdleConns(), config.GetMySQLConnMaxLifetimeSec())
	default:
		dir := path.Dir(dbPath)
		err = os.MkdirAll(dir, fs.ModePerm)
		if err != nil {
			return err
		}
		db, err = gorm.Open(sqlite.Open(dbPath), c)
		if err != nil {
			return err
		}
	}

	if err := initModels(); err != nil {
		return err
	}

	isUsersEmpty, err := isTableEmpty("users")
	if err != nil {
		return err
	}

	if err := initUser(); err != nil {
		return err
	}
	return runSeeders(isUsersEmpty)
}

// CloseDB closes the database connection if it exists.
func CloseDB() error {
	if db != nil {
		sqlDB, err := db.DB()
		if err != nil {
			return err
		}
		return sqlDB.Close()
	}
	return nil
}

// GetDB returns the global GORM database instance.
func GetDB() *gorm.DB {
	return db
}

// GetDBType returns the current database type ("sqlite" or "mysql").
func GetDBType() string {
	return dbType
}

// IsMySQL returns true if the current database backend is MySQL.
func IsMySQL() bool {
	return dbType == "mysql"
}

// IsNotFound checks if the given error is a GORM record not found error.
func IsNotFound(err error) bool {
	return err == gorm.ErrRecordNotFound
}

// IsSQLiteDB checks if the given file is a valid SQLite database by reading its signature.
func IsSQLiteDB(file io.ReaderAt) (bool, error) {
	signature := []byte("SQLite format 3\x00")
	buf := make([]byte, len(signature))
	_, err := file.ReadAt(buf, 0)
	if err != nil {
		return false, err
	}
	return bytes.Equal(buf, signature), nil
}

// IsRetryableDBError returns true if the error is a MySQL deadlock (1213)
// or lock wait timeout (1205) that can be retried.
func IsRetryableDBError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "Error 1213") ||
		strings.Contains(s, "Deadlock found") ||
		strings.Contains(s, "Error 1205") ||
		strings.Contains(s, "Lock wait timeout")
}

// IsDeadlock is an alias kept for backward compatibility.
func IsDeadlock(err error) bool {
	return IsRetryableDBError(err)
}

// RetryOnDeadlock wraps a GORM transaction with automatic retry on MySQL
// deadlocks (1213) and lock wait timeouts (1205).
func RetryOnDeadlock(fn func(tx *gorm.DB) error) error {
	maxRetries := 1
	if IsMySQL() {
		maxRetries = 5
	}
	for attempt := range maxRetries {
		err := db.Transaction(fn)
		if err == nil {
			return nil
		}
		if IsMySQL() && IsRetryableDBError(err) && attempt < maxRetries-1 {
			time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
			continue
		}
		return err
	}
	return nil
}

// Checkpoint performs a WAL checkpoint on the SQLite database.
// It is a no-op when using MySQL.
func Checkpoint() error {
	if IsMySQL() {
		return nil
	}
	err := db.Exec("PRAGMA wal_checkpoint;").Error
	if err != nil {
		return err
	}
	return nil
}

// ValidateSQLiteDB opens the provided sqlite DB path with a throw-away connection
// and runs a PRAGMA integrity_check to ensure the file is structurally sound.
// Returns an error if the current backend is MySQL.
func ValidateSQLiteDB(dbPath string) error {
	if IsMySQL() {
		return errors.New("ValidateSQLiteDB is not supported when using MySQL")
	}
	if _, err := os.Stat(dbPath); err != nil {
		return err
	}
	gdb, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		return err
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	var res string
	if err := gdb.Raw("PRAGMA integrity_check;").Scan(&res).Error; err != nil {
		return err
	}
	if res != "ok" {
		return errors.New("sqlite integrity check failed: " + res)
	}
	return nil
}
