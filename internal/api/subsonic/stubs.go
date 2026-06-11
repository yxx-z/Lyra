package subsonic

import "net/http"

// 第一期未实现、但客户端启动探测会调用的只读端点：返回合法的空响应，
// 避免客户端（Symfonium 等）因拿到纯文本 404 无法解析而中断同步循环。
// 真正的实现（收藏/书签/流派）见第二期。

func (h *Handler) getGenres(w http.ResponseWriter, r *http.Request) {
	writeResponse(w, r, &Response{Genres: &Genres{Genre: []Genre{}}})
}
