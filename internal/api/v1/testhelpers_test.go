// internal/api/v1/testhelpers_test.go
package v1

import (
	"database/sql"
	"testing"

	"github.com/yxx-z/lyra/internal/db"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// insertTestData inserts one artist, one album, and two available tracks.
func insertTestData(t *testing.T, d *sql.DB) {
	t.Helper()
	stmts := []string{
		`INSERT INTO artists(id,name,created_at,updated_at) VALUES('a1','蔡琴',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		`INSERT INTO albums(id,title,artist_id,release_date,created_at,updated_at) VALUES('al1','金片子','a1','1984',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		`INSERT INTO tracks(id,title,artist_id,album_id,track_number,disc_number,duration,file_path,format,is_available,created_at,updated_at,scrape_status) VALUES('t1','渡口','a1','al1',1,1,245,'/tmp/t1.flac','flac',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,'pending')`,
		`INSERT INTO tracks(id,title,artist_id,album_id,track_number,disc_number,duration,file_path,format,is_available,created_at,updated_at,scrape_status) VALUES('t2','被遗忘的时光','a1','al1',2,1,210,'/tmp/t2.flac','flac',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,'pending')`,
	}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			t.Fatalf("insertTestData: %v", err)
		}
	}
}
