package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// OpenDatabase opens the configured backend and initializes the schema and system data.
func OpenDatabase(ctx context.Context, driver, dsn, adminEmail, adminName string) (*Store, error) {
	var dialector gorm.Dialector
	switch driver {
	case "", "sqlite":
		if !strings.HasPrefix(dsn, "file:") {
			return nil, errors.New("database dsn must start with file")
		}
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		dialector = sqlite.Open(dsn + sep + "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	case "mysql":
		dialector = mysql.Open(dsn)
	default:
		return nil, errors.New("unsupported database driver")
	}
	return Open(ctx, dialector, adminEmail, adminName)
}

// Close releases the database connection pool owned by the store.
func (s *Store) Close() error {
	db, err := s.db.DB()
	if err != nil {
		return fmt.Errorf("get database connection: %w", err)
	}
	if err := db.Close(); err != nil {
		return fmt.Errorf("close database: %w", err)
	}
	return nil
}
