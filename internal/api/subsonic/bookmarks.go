package subsonic

import (
	"net/http"
	"strconv"
	"strings"
)

// childByID 按 trackID 查一首可用曲目并构造 Child；不存在/不可用返回 ok=false。
// 复用 browse.go 的 trackSelect 与 scanChild。
func (h *Handler) childByID(trackID string) (Child, bool) {
	rows, err := h.db.Query(trackSelect+` WHERE tr.id=? AND tr.is_available=1`, trackID)
	if err != nil {
		return Child{}, false
	}
	defer rows.Close()
	if !rows.Next() {
		return Child{}, false
	}
	c, err := scanChild(rows)
	if err != nil {
		return Child{}, false
	}
	return c, true
}

func (h *Handler) createBookmark(w http.ResponseWriter, r *http.Request) {
	id := r.Form.Get("id")
	position, _ := strconv.ParseInt(r.Form.Get("position"), 10, 64)
	comment := r.Form.Get("comment")

	var exists string
	if err := h.db.QueryRow(`SELECT id FROM tracks WHERE id=? AND is_available=1`, id).Scan(&exists); err != nil {
		writeError(w, r, 70, "曲目不存在")
		return
	}
	if _, err := h.db.Exec(`
		INSERT INTO bookmarks(track_id, position, comment) VALUES(?,?,?)
		ON CONFLICT(track_id) DO UPDATE SET
			position=excluded.position, comment=excluded.comment, updated_at=datetime('now')`,
		id, position, comment); err != nil {
		writeError(w, r, 0, "保存书签失败")
		return
	}
	writeResponse(w, r, &Response{})
}

func (h *Handler) getBookmarks(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(`SELECT track_id, position, comment, created_at, updated_at FROM bookmarks ORDER BY updated_at DESC`)
	if err != nil {
		writeError(w, r, 0, "查询失败")
		return
	}
	type bmRow struct {
		trackID, comment, created, changed string
		position                           int64
	}
	var raw []bmRow
	for rows.Next() {
		var bm bmRow
		if err := rows.Scan(&bm.trackID, &bm.position, &bm.comment, &bm.created, &bm.changed); err != nil {
			continue
		}
		raw = append(raw, bm)
	}
	rows.Close()

	bms := &Bookmarks{}
	for _, bm := range raw {
		child, ok := h.childByID(bm.trackID)
		if !ok {
			continue
		}
		bms.Bookmark = append(bms.Bookmark, Bookmark{
			Position: bm.position,
			Username: h.cfg.Auth.Username,
			Comment:  bm.comment,
			Created:  bm.created,
			Changed:  bm.changed,
			Entry:    child,
		})
	}
	writeResponse(w, r, &Response{Bookmarks: bms})
}

func (h *Handler) deleteBookmark(w http.ResponseWriter, r *http.Request) {
	_, _ = h.db.Exec(`DELETE FROM bookmarks WHERE track_id=?`, r.Form.Get("id"))
	writeResponse(w, r, &Response{})
}

func (h *Handler) savePlayQueue(w http.ResponseWriter, r *http.Request) {
	trackIDs := strings.Join(r.Form["id"], ",")
	current := r.Form.Get("current")
	position, _ := strconv.ParseInt(r.Form.Get("position"), 10, 64)
	changedBy := r.Form.Get("c")
	if _, err := h.db.Exec(`
		INSERT INTO play_queue(id, track_ids, current, position, changed_at, changed_by)
		VALUES(1, ?, ?, ?, datetime('now'), ?)
		ON CONFLICT(id) DO UPDATE SET
			track_ids=excluded.track_ids, current=excluded.current,
			position=excluded.position, changed_at=datetime('now'), changed_by=excluded.changed_by`,
		trackIDs, current, position, changedBy); err != nil {
		writeError(w, r, 0, "保存播放队列失败")
		return
	}
	writeResponse(w, r, &Response{})
}

func (h *Handler) getPlayQueue(w http.ResponseWriter, r *http.Request) {
	var trackIDs, current, changed, changedBy string
	var position int64
	err := h.db.QueryRow(`SELECT track_ids, current, position, changed_at, changed_by FROM play_queue WHERE id=1`).
		Scan(&trackIDs, &current, &position, &changed, &changedBy)
	if err != nil {
		writeResponse(w, r, &Response{})
		return
	}
	pq := &PlayQueue{
		Current:   current,
		Position:  position,
		Username:  h.cfg.Auth.Username,
		Changed:   changed,
		ChangedBy: changedBy,
	}
	if trackIDs != "" {
		for _, id := range strings.Split(trackIDs, ",") {
			if c, ok := h.childByID(id); ok {
				pq.Entry = append(pq.Entry, c)
			}
		}
	}
	writeResponse(w, r, &Response{PlayQueue: pq})
}
