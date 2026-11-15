package database

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/alireza0/x-ui/config"
	"github.com/alireza0/x-ui/database/migrations"
	"github.com/alireza0/x-ui/database/model"
	xlogger "github.com/alireza0/x-ui/logger"
	"github.com/alireza0/x-ui/util/common"
	"github.com/alireza0/x-ui/xray"
	"github.com/shirou/gopsutil/v4/mem"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	sqliteMaxOpenConns   = 4
	sqliteMaxIdleConns   = 2
	sqliteConnMaxIdle    = 3 * time.Minute
	sqliteCacheTooSmall  = 512 << 20
	sqliteCacheMedium    = 1 << 30
	sqliteCacheSizeSmall = -4000 // KiB per connection
	sqliteCacheSizeMid   = -8000
	sqliteCacheSizeBig   = -16000
)

var db *gorm.DB

// sqliteCacheSize follows the driver's DSN semantics: a negative value means KiB.
func sqliteCacheSize(limit uint64) (int64, string) {
	if v := os.Getenv("XUI_SQLITE_CACHE_SIZE"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n != 0 {
			return n, "env"
		}
		xlogger.Warning("ignoring invalid XUI_SQLITE_CACHE_SIZE:", v)
	}
	switch {
	case limit > 0 && limit <= sqliteCacheTooSmall:
		return sqliteCacheSizeSmall, "auto"
	case limit > 0 && limit <= sqliteCacheMedium:
		return sqliteCacheSizeMid, "auto"
	default:
		return sqliteCacheSizeBig, "auto"
	}
}

func memoryLimitBytes() uint64 {
	var limit uint64
	if vm, err := mem.VirtualMemory(); err == nil {
		limit = vm.Total
	}
	if cg := cgroupMemoryLimit(); cg > 0 && (limit == 0 || cg < limit) {
		limit = cg
	}
	return limit
}

func cgroupMemoryLimit() uint64 {
	if data, err := os.ReadFile("/proc/self/cgroup"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			parts := strings.SplitN(line, ":", 3)
			if len(parts) != 3 {
				continue
			}
			if parts[0] == "0" && parts[1] == "" {
				if n := readMemoryLimit("/sys/fs/cgroup" + parts[2] + "/memory.max"); n > 0 {
					return n
				}
			} else if parts[1] == "memory" {
				if n := readMemoryLimit("/sys/fs/cgroup/memory" + parts[2] + "/memory.limit_in_bytes"); n > 0 {
					return n
				}
			}
		}
	}
	// Root paths also cover containers with a private cgroup namespace.
	for _, path := range []string{
		"/sys/fs/cgroup/memory.max",
		"/sys/fs/cgroup/memory/memory.limit_in_bytes",
	} {
		if n := readMemoryLimit(path); n > 0 {
			return n
		}
	}
	return 0
}

func readMemoryLimit(path string) uint64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	value := strings.TrimSpace(string(data))
	if value == "" || value == "max" {
		return 0
	}
	n, err := strconv.ParseUint(value, 10, 64)
	// cgroup v1 reports a near-max sentinel when unlimited
	if err != nil || n == 0 || n >= 1<<60 {
		return 0
	}
	return n
}

func initUser() error {
	err := db.AutoMigrate(&model.User{})
	if err != nil {
		return err
	}
	var count int64
	err = db.Model(&model.User{}).Count(&count).Error
	if err != nil {
		return err
	}
	if count == 0 {
		user := &model.User{
			Username: "admin",
			Password: "admin",
		}
		return db.Create(user).Error
	}
	return nil
}

func InitDB(dbPath string) error {
	dir := path.Dir(dbPath)
	err := os.MkdirAll(dir, fs.ModeDir)
	if err != nil {
		return err
	}

	var gormLogger logger.Interface

	if config.IsDebug() {
		gormLogger = logger.Default
	} else {
		gormLogger = logger.Discard
	}

	c := &gorm.Config{
		Logger: gormLogger,
	}
	// WAL plus a busy timeout keeps concurrent writes from failing on slow disks.
	memLimit := memoryLimitBytes()
	cacheSize, cacheSource := sqliteCacheSize(memLimit)
	xlogger.Info("sqlite: cache_size=", cacheSize, " source=", cacheSource, " memory_limit_mb=", memLimit>>20)
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=10000&_cache_size=%d&_foreign_keys=on&_txlock=immediate", dbPath, cacheSize)
	db, err = gorm.Open(sqlite.Open(dsn), c)
	if err != nil {
		return err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	sqlDB.SetMaxOpenConns(sqliteMaxOpenConns)
	sqlDB.SetMaxIdleConns(sqliteMaxIdleConns)
	sqlDB.SetConnMaxIdleTime(sqliteConnMaxIdle)

	if err = db.Exec("PRAGMA journal_mode=WAL;").Error; err != nil {
		return err
	}

	err = initUser()
	if err != nil {
		return err
	}
	err = db.AutoMigrate(
		&model.Inbound{},
		&model.Outbound{},
		&model.RoutingRule{},
		&model.Setting{},
		&model.ClientTrafficDaily{},
		&xray.ClientTraffic{},
	)
	if err != nil {
		return err
	}

	if err = migrations.Run(db); err != nil {
		return err
	}

	// Dashboard queries filter clients by expiry and remaining quota.
	if err := db.Exec("CREATE INDEX IF NOT EXISTS idx_client_traffics_expiry ON client_traffics(enable, expiry_time);").Error; err != nil {
		xlogger.Warning("sqlite: create client expiry index failed: ", err)
	}
	if err := db.Exec("CREATE INDEX IF NOT EXISTS idx_client_traffics_remaining ON client_traffics((total - up - down)) WHERE total > 0;").Error; err != nil {
		xlogger.Warning("sqlite: create client remaining index failed: ", err)
	}

	// Fold any WAL left over from a previous run back into the main file.
	if err := Checkpoint(); err != nil {
		xlogger.Warning("sqlite: startup wal checkpoint failed: ", err)
	}

	return nil
}

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

func GetDB() *gorm.DB {
	return db
}

func IsNotFound(err error) bool {
	return err == gorm.ErrRecordNotFound
}

func IsSQLiteDB(file io.Reader) (bool, error) {
	signature := []byte("SQLite format 3\x00")
	buf := make([]byte, len(signature))
	_, err := file.Read(buf)
	if err != nil {
		return false, err
	}
	return bytes.Equal(buf, signature), nil
}

func Checkpoint() error {
	var busy, log, checkpointed int
	err := db.Raw("PRAGMA wal_checkpoint(TRUNCATE);").Row().Scan(&busy, &log, &checkpointed)
	if err != nil {
		return err
	}
	if busy != 0 {
		return common.NewErrorf("wal checkpoint busy: log=%d checkpointed=%d", log, checkpointed)
	}
	return nil
}

// Snapshot writes a consistent copy of the database through SQLite's
// VACUUM INTO. Unlike copying the live file it includes data still sitting in
// the WAL and never produces a torn or lock-breaking copy.
func Snapshot(destPath string) error {
	if _, err := os.Stat(destPath); err == nil {
		return common.NewError("snapshot destination already exists: " + destPath)
	}
	return db.Exec("VACUUM INTO ?", destPath).Error
}

// CreateSnapshot writes a snapshot into a private temporary directory. The
// returned cleanup function removes it.
func CreateSnapshot() (string, func(), error) {
	dir, err := os.MkdirTemp("", "x-ui-snapshot-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	destPath := path.Join(dir, "x-ui.db")
	if err := Snapshot(destPath); err != nil {
		cleanup()
		return "", nil, err
	}
	return destPath, cleanup, nil
}

func ValidateSQLiteDB(dbPath string) error {
	if _, err := os.Stat(dbPath); err != nil { // file must exist
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
		return common.NewError("sqlite integrity check failed: " + res)
	}
	return nil
}
