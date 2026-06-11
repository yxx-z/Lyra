// internal/api/v1/library_delete.go
package v1

import (
	"database/sql"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
)

// deleteTracksByIDs 在事务内删除给定曲目及其依赖（lyrics、starred(song)）；
// bookmarks/play_stats/playlist_tracks 经 FK ON DELETE CASCADE 自动清。
func deleteTracksByIDs(tx *sql.Tx, ids []string) error {
	for _, id := range ids {
		if _, err := tx.Exec(`DELETE FROM lyrics WHERE track_id=?`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM starred WHERE item_type='song' AND item_id=?`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM tracks WHERE id=?`, id); err != nil {
			return err
		}
	}
	return nil
}

// collectTracks 跑给定查询，返回曲目 id 与 file_path 两个切片（已排空 rows）。
func collectTracks(db *sql.DB, query string, args ...any) (ids []string, paths []string, err error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, p string
		if err := rows.Scan(&id, &p); err != nil {
			return nil, nil, err
		}
		ids = append(ids, id)
		paths = append(paths, p)
	}
	return ids, paths, rows.Err()
}

// removeFiles 尽力删除磁盘文件，返回成功数与错误描述。
func removeFiles(paths []string) (int, []string) {
	deleted := 0
	errs := []string{}
	for _, p := range paths {
		if p == "" {
			continue
		}
		if err := os.Remove(p); err != nil {
			errs = append(errs, p+": "+err.Error())
		} else {
			deleted++
		}
	}
	return deleted, errs
}

// DeleteAlbum 处理 DELETE /api/v1/albums/{id}?deleteFiles=（管理员）。
func (h *AlbumsHandler) DeleteAlbum(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var exists string
	if err := h.db.QueryRow(`SELECT id FROM albums WHERE id=?`, id).Scan(&exists); err != nil {
		http.NotFound(w, r)
		return
	}
	ids, paths, err := collectTracks(h.db, `SELECT id, file_path FROM tracks WHERE album_id=?`, id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	tx, err := h.db.Begin()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "删除失败")
		return
	}
	if err := deleteTracksByIDs(tx, ids); err != nil {
		tx.Rollback()
		writeJSONError(w, http.StatusInternalServerError, "删除失败")
		return
	}
	if _, err := tx.Exec(`DELETE FROM starred WHERE item_type='album' AND item_id=?`, id); err != nil {
		tx.Rollback()
		writeJSONError(w, http.StatusInternalServerError, "删除失败")
		return
	}
	if _, err := tx.Exec(`DELETE FROM albums WHERE id=?`, id); err != nil {
		tx.Rollback()
		writeJSONError(w, http.StatusInternalServerError, "删除失败")
		return
	}
	if err := tx.Commit(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeDeleteResult(w, r, paths)
}

// writeDeleteResult 按 deleteFiles 参数尽力删文件并写统一响应。
func writeDeleteResult(w http.ResponseWriter, r *http.Request, paths []string) {
	filesDeleted := 0
	fileErrors := []string{}
	if r.URL.Query().Get("deleteFiles") == "true" {
		filesDeleted, fileErrors = removeFiles(paths)
	}
	writeJSON(w, map[string]any{"ok": true, "filesDeleted": filesDeleted, "fileErrors": fileErrors})
}
