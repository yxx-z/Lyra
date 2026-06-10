package transcode

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Service 按客户端参数对音频源直传或转码输出，转码结果落盘缓存。
type Service struct {
	ffmpegPath     string
	defaultBitrate int
	cache          *Cache
}

// NewService 创建转码服务。
func NewService(ffmpegPath string, defaultBitrate int, cache *Cache) *Service {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	if defaultBitrate == 0 {
		defaultBitrate = 192
	}
	return &Service{ffmpegPath: ffmpegPath, defaultBitrate: defaultBitrate, cache: cache}
}

// Serve 处理一个流请求：直传 / 命中缓存 / 管道转码+写缓存 / seek 回退。
func (s *Service) Serve(w http.ResponseWriter, r *http.Request, src Source) {
	dec := Plan(src, parseParams(r), s.defaultBitrate)

	if dec.Passthrough {
		w.Header().Set("Content-Type", dec.ContentType)
		http.ServeFile(w, r, src.Path)
		return
	}

	cachePath := s.cache.Path(src.ID, dec.Codec, dec.Bitrate)
	key := s.cache.key(src.ID, dec.Codec, dec.Bitrate)

	// 命中缓存
	if _, err := os.Stat(cachePath); err == nil {
		s.cache.touch(cachePath)
		prepareAudio(w, r, dec.ContentType)
		http.ServeFile(w, r, cachePath)
		return
	}

	// 缓存生成前带偏移拖动 → 阻塞式转成完整文件再服务（保证可 seek）
	if hasRangeOffset(r) {
		lock := s.cache.lockFor(key)
		lock.Lock()
		if _, err := os.Stat(cachePath); err != nil {
			if terr := s.transcodeToFile(r.Context(), src.Path, cachePath, dec); terr != nil {
				lock.Unlock()
				if r.Context().Err() != nil {
					return
				}
				http.Error(w, "转码失败", http.StatusInternalServerError)
				return
			}
			s.cache.evict(cachePath)
		}
		lock.Unlock()
		prepareAudio(w, r, dec.ContentType)
		http.ServeFile(w, r, cachePath)
		return
	}

	// 正常从头播：抢到锁 → 管道+写缓存；抢不到（同 key 正在转）→ 纯管道不写缓存
	lock := s.cache.lockFor(key)
	if lock.TryLock() {
		defer lock.Unlock()
		s.pipeAndCache(w, r, src.Path, cachePath, dec)
	} else {
		s.runPipe(w, r, src.Path, dec, nil)
	}
}

// parseParams 从请求读取 format / maxBitRate（GET query 与 POST 表单皆可）。
func parseParams(r *http.Request) Params {
	_ = r.ParseForm()
	br, _ := strconv.Atoi(r.Form.Get("maxBitRate"))
	return Params{Format: r.Form.Get("format"), MaxBitRate: br}
}

// hasRangeOffset 判断是否为带非零起点的 Range 请求（"bytes=0-" 视为从头，不算偏移）。
func hasRangeOffset(r *http.Request) bool {
	rng := r.Header.Get("Range")
	return rng != "" && !strings.HasPrefix(rng, "bytes=0-")
}

func prepareAudio(w http.ResponseWriter, r *http.Request, contentType string) {
	r.Header.Del("If-Modified-Since")
	r.Header.Del("If-None-Match")
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
}

// command 构造 ffmpeg 命令；output 为 "pipe:1"（stdout）或目标文件路径。
func (s *Service) command(ctx context.Context, srcPath, output string, dec Decision) *exec.Cmd {
	args := []string{"-hide_banner", "-loglevel", "error", "-i", srcPath, "-vn"}
	args = append(args, codecFor(dec.Codec).Args...)
	args = append(args, "-b:a", strconv.Itoa(dec.Bitrate)+"k", "-y", output)
	return exec.CommandContext(ctx, s.ffmpegPath, args...)
}

// pipeAndCache 管道转码：边写响应边写临时缓存，成功后原子提升并回收。
func (s *Service) pipeAndCache(w http.ResponseWriter, r *http.Request, srcPath, cachePath string, dec Decision) {
	if err := os.MkdirAll(s.cache.dir, 0o755); err != nil {
		http.Error(w, "转码失败", http.StatusInternalServerError)
		return
	}
	tmp := cachePath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		http.Error(w, "转码失败", http.StatusInternalServerError)
		return
	}
	ok := s.runPipe(w, r, srcPath, dec, f)
	f.Close()
	if !ok {
		_ = os.Remove(tmp)
		return
	}
	if err := os.Rename(tmp, cachePath); err != nil {
		_ = os.Remove(tmp)
		slog.Warn("提升转码缓存失败", "err", err)
		return
	}
	s.cache.evict(cachePath)
}

// runPipe 起 ffmpeg 把输出写到 w（以及可选 extra，如缓存文件）。
// 首字节前失败 → 写 500 返回 false；已发出字节后失败 → 记录并返回 false（无法改状态码）。
func (s *Service) runPipe(w http.ResponseWriter, r *http.Request, srcPath string, dec Decision, extra io.Writer) bool {
	cmd := s.command(r.Context(), srcPath, "pipe:1", dec)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, "转码失败", http.StatusInternalServerError)
		return false
	}
	if err := cmd.Start(); err != nil {
		http.Error(w, "转码失败", http.StatusInternalServerError)
		return false
	}

	// 先读首块，确认有输出再写响应头
	buf := make([]byte, 32*1024)
	n, rerr := stdout.Read(buf)
	if n == 0 {
		_ = cmd.Wait()
		if r.Context().Err() != nil {
			return false
		}
		http.Error(w, "转码失败", http.StatusInternalServerError)
		return false
	}

	prepareAudio(w, r, dec.ContentType)
	dst := io.Writer(w)
	if extra != nil {
		dst = io.MultiWriter(w, extra)
	}
	if _, werr := dst.Write(buf[:n]); werr != nil {
		_, _ = io.Copy(io.Discard, stdout)
		_ = cmd.Wait()
		return false
	}
	var copyErr error
	if rerr == nil {
		_, copyErr = io.Copy(dst, stdout)
	} else if rerr != io.EOF {
		copyErr = rerr
	}
	waitErr := cmd.Wait()
	if copyErr != nil || waitErr != nil {
		if r.Context().Err() != nil {
			return false // 客户端断开/取消
		}
		slog.Warn("转码管道失败", "err", errors.Join(copyErr, waitErr))
		return false
	}
	return true
}

// transcodeToFile 阻塞式转成完整文件（seek 回退用）：写临时文件再原子改名。
func (s *Service) transcodeToFile(ctx context.Context, srcPath, dst string, dec Decision) error {
	if err := os.MkdirAll(s.cache.dir, 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	if err := s.command(ctx, srcPath, tmp, dec).Run(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
