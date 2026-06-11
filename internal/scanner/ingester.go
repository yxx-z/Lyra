// internal/scanner/ingester.go
package scanner

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
)

// decodeToUTF8 把歌词文件字节统一转为 UTF-8。
// 很多中文 .lrc 是 GBK/GB2312 编码，原样存库会在网页（按 UTF-8 渲染）显示乱码。
// 策略：带 BOM 的 UTF-16 按 BOM 解；去掉 UTF-8 BOM；已是合法 UTF-8 则保留；
// 否则按 GB18030（GBK/GB2312 的超集，覆盖几乎所有中文 .lrc）解码；实在失败原样返回。
func decodeToUTF8(b []byte) string {
	if len(b) >= 2 && ((b[0] == 0xFF && b[1] == 0xFE) || (b[0] == 0xFE && b[1] == 0xFF)) {
		if out, err := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder().Bytes(b); err == nil {
			return string(out)
		}
	}
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	if utf8.Valid(b) {
		return string(b)
	}
	if out, err := simplifiedchinese.GB18030.NewDecoder().Bytes(b); err == nil {
		return string(out)
	}
	return string(b)
}

// Ingester writes TrackMeta records to the database.
type Ingester struct {
	db *sql.DB
}

// NewIngester creates an Ingester backed by db.
func NewIngester(db *sql.DB) *Ingester {
	return &Ingester{db: db}
}

// Ingest upserts artist, album, and track for the given metadata.
func (ing *Ingester) Ingest(meta TrackMeta) error {
	trackArtistID, err := ing.findOrCreateArtist(meta.Artist)
	if err != nil {
		return fmt.Errorf("artist: %w", err)
	}

	albumArtistName := meta.AlbumArtist
	if albumArtistName == "" {
		albumArtistName = meta.Artist
	}
	albumArtistID, err := ing.findOrCreateArtist(albumArtistName)
	if err != nil {
		return fmt.Errorf("album artist: %w", err)
	}

	albumID, err := ing.findOrCreateAlbum(meta.Album, albumArtistID, meta.Year)
	if err != nil {
		return fmt.Errorf("album: %w", err)
	}

	trackID, err := ing.upsertTrack(meta, trackArtistID, albumID)
	if err != nil {
		return err
	}
	if err := ing.importSidecarLyrics(trackID, meta.FilePath); err != nil {
		return fmt.Errorf("lyrics: %w", err)
	}
	return nil
}

// MarkUnavailable sets is_available=0 for the track at filePath.
func (ing *Ingester) MarkUnavailable(filePath string) error {
	_, err := ing.db.Exec(
		`UPDATE tracks SET is_available=0, updated_at=? WHERE file_path=?`,
		time.Now(), filePath,
	)
	return err
}

// normalize 为去重比较做归一：去首尾空白 + 仅小写 ASCII A–Z。
// 必须与 SQL 端 lower()（SQLite 内置 lower 只处理 ASCII）保持一致——
// 不能用 strings.ToLower：它会把 Unicode 大写也小写（如罗马数字 Ⅱ→ⅱ），
// 而 SQLite lower() 保留 Ⅱ，两边不一致会导致含此类字符的专辑/艺术家每次扫描都新建重复行。
// 内部空格保留："AC DC" 与 "ACDC" 仍视为不同。
func normalize(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, strings.TrimSpace(s))
}

func (ing *Ingester) findOrCreateArtist(name string) (string, error) {
	var id string
	err := ing.db.QueryRow(
		`SELECT id FROM artists WHERE lower(trim(name))=?`, normalize(name),
	).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	id = uuid.New().String()
	now := time.Now()
	_, err = ing.db.Exec(
		`INSERT INTO artists(id,name,created_at,updated_at) VALUES(?,?,?,?)`,
		id, name, now, now,
	)
	return id, err
}

func (ing *Ingester) findOrCreateAlbum(title, artistID string, year int) (string, error) {
	var id string
	err := ing.db.QueryRow(
		`SELECT id FROM albums WHERE lower(trim(title))=? AND artist_id=?`,
		normalize(title), artistID,
	).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	id = uuid.New().String()
	releaseDate := ""
	if year > 0 {
		releaseDate = fmt.Sprintf("%d", year)
	}
	now := time.Now()
	_, err = ing.db.Exec(
		`INSERT INTO albums(id,title,artist_id,release_date,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
		id, title, artistID, releaseDate, now, now,
	)
	return id, err
}

func (ing *Ingester) upsertTrack(meta TrackMeta, artistID, albumID string) (string, error) {
	now := time.Now()
	_, err := ing.db.Exec(`
		INSERT INTO tracks(
			id,title,artist_id,album_id,track_number,disc_number,
			duration,file_path,file_size,format,bitrate,sample_rate,
			channels,scrape_status,is_available,created_at,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,'pending',1,?,?)
		ON CONFLICT(file_path) DO UPDATE SET
			title=excluded.title,
			artist_id=excluded.artist_id,
			album_id=excluded.album_id,
			track_number=excluded.track_number,
			disc_number=excluded.disc_number,
			duration=excluded.duration,
			file_size=excluded.file_size,
			format=excluded.format,
			bitrate=excluded.bitrate,
			sample_rate=excluded.sample_rate,
			channels=excluded.channels,
			is_available=1,
			updated_at=excluded.updated_at`,
		uuid.New().String(),
		meta.Title, artistID, albumID,
		meta.TrackNumber, meta.DiscNumber,
		meta.Duration, meta.FilePath, meta.FileSize, meta.Format,
		meta.Bitrate, meta.SampleRate, meta.Channels,
		now, now,
	)
	if err != nil {
		return "", err
	}

	var id string
	if err := ing.db.QueryRow(`SELECT id FROM tracks WHERE file_path=?`, meta.FilePath).Scan(&id); err != nil {
		return "", err
	}
	return id, nil
}

func (ing *Ingester) importSidecarLyrics(trackID, audioPath string) error {
	lrcPath := strings.TrimSuffix(audioPath, filepath.Ext(audioPath)) + ".lrc"
	content, err := os.ReadFile(lrcPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	lrc := decodeToUTF8(content)
	if strings.TrimSpace(lrc) == "" {
		return nil
	}

	_, err = ing.db.Exec(`
		INSERT INTO lyrics(track_id,lrc_content,yrc_content,source,updated_at)
		VALUES(?,?,'','sidecar',CURRENT_TIMESTAMP)
		ON CONFLICT(track_id) DO UPDATE SET
			lrc_content=excluded.lrc_content,
			source=excluded.source,
			updated_at=CURRENT_TIMESTAMP
		WHERE lyrics.source = 'sidecar'`,
		trackID, lrc,
	)
	return err
}
