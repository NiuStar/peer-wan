package db

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"peer-wan/pkg/model"
)

// Init connects to MySQL and runs migrations.
// Env:
//
//	MYSQL_DSN or MYSQL_HOST, MYSQL_PORT, MYSQL_USER, MYSQL_PASS, MYSQL_DB
func Init() (*gorm.DB, error) {
	_ = loadDotEnv()
	host := getenv("MYSQL_HOST", "127.0.0.1")
	port := getenv("MYSQL_PORT", "3306")
	user := getenv("MYSQL_USER", "root")
	pass := getenv("MYSQL_PASS", "")
	dbname := getenv("MYSQL_DB", "peer_wan")

	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local", user, pass, host, port, dbname)
	}

	cfg := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	}
	db, err := gorm.Open(mysql.Open(dsn), cfg)
	if err != nil {
		// Try to create database if missing
		if strings.Contains(err.Error(), "Unknown database") {
			if cerr := createDatabase(user, pass, host, port, dbname); cerr != nil {
				return nil, fmt.Errorf("create database failed: %w", cerr)
			}
			db, err = gorm.Open(mysql.Open(dsn), cfg)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}
	sqlDB, _ := db.DB()
	sqlDB.SetConnMaxLifetime(time.Hour)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetMaxOpenConns(20)
	if err := db.AutoMigrate(&model.User{}); err != nil {
		return nil, err
	}
	return db, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func loadDotEnv() error {
	if _, err := os.Stat(".env"); err == nil {
		return godotenv.Load(".env")
	}
	return nil
}

func createDatabase(user, pass, host, port, dbname string) error {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/", user, pass, host, port)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` DEFAULT CHARACTER SET utf8mb4", dbname))
	return err
}
