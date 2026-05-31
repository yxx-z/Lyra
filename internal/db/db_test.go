package db

import (
	"testing"
)

func TestOpen_CreatesTablesOnFirstRun(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	defer db.Close()

	var count int
	row := db.QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('artists','albums','tracks','lyrics')`,
	)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if count != 4 {
		t.Errorf("期望 4 张核心表，实际 %d", count)
	}
}

func TestOpen_IdempotentMigrations(t *testing.T) {
	tmp := t.TempDir() + "/test.db"
	db1, err := Open(tmp)
	if err != nil {
		t.Fatalf("第一次 Open 失败: %v", err)
	}
	db1.Close()

	// 第二次打开同一文件，迁移应幂等，不报错
	db2, err := Open(tmp)
	if err != nil {
		t.Fatalf("第二次 Open（幂等性测试）失败: %v", err)
	}
	db2.Close()
}
