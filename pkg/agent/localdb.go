package agent

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var (
	sqliteOnce sync.Once
	sqliteDB   *sql.DB
)

const sqlitePath = "/var/lib/peer-wan/state.db"

type policyOp struct {
	RuleHash string
	Op       string
	Detail   string
	Time     time.Time
}

func initSQLite() {
	sqliteOnce.Do(func() {
		if err := os.MkdirAll(filepath.Dir(sqlitePath), 0o755); err != nil {
			log.Printf("sqlite init mkdir failed: %v", err)
			return
		}
		dsn := "file:" + sqlitePath + "?_pragma=busy_timeout=5000"
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			log.Printf("sqlite open failed: %v", err)
			return
		}
		db.SetMaxOpenConns(1)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			log.Printf("sqlite ping failed: %v", err)
			_ = db.Close()
			return
		}
		if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS policy_ops(rule_hash TEXT, op TEXT, detail TEXT, ts INTEGER); CREATE INDEX IF NOT EXISTS idx_policy_ops_rule ON policy_ops(rule_hash);`); err != nil {
			log.Printf("sqlite init schema failed: %v", err)
			_ = db.Close()
			return
		}
		sqliteDB = db
	})
}

// hashRule produces a stable hash for a policy rule to track apply/remove.
func hashRule(rule interface{}) string {
	b, _ := json.Marshal(rule)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func recordPolicyOp(ruleHash, op, detail string) {
	initSQLite()
	if sqliteDB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = sqliteDB.ExecContext(ctx, `INSERT INTO policy_ops(rule_hash, op, detail, ts) VALUES(?,?,?,?)`, ruleHash, op, detail, time.Now().Unix())
}

func purgeMissingHashes(current map[string]struct{}) {
	initSQLite()
	if sqliteDB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rows, err := sqliteDB.QueryContext(ctx, `SELECT rule_hash FROM policy_ops GROUP BY rule_hash`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			continue
		}
		if _, ok := current[h]; !ok {
			_, _ = sqliteDB.ExecContext(ctx, `DELETE FROM policy_ops WHERE rule_hash=?`, h)
			recordPolicyOp(h, "purge", "rule removed; records purged")
		}
	}
}
