package lyrics

import (
	"context"
	"errors"
	"testing"
)

func TestHasTimestamps(t *testing.T) {
	if !hasTimestamps("[00:01.00]hi") {
		t.Error("带时间轴应 true")
	}
	if hasTimestamps("纯文本\n第二行") {
		t.Error("纯文本应 false")
	}
}

func TestUpgradeToSynced_ReplacesWithSynced(t *testing.T) {
	d := newServiceTestDB(t)
	if _, err := d.Exec(`INSERT INTO lyrics(track_id,lrc_content,source) VALUES('t1','纯文本一行','embedded')`); err != nil {
		t.Fatal(err)
	}
	embCalls := 0
	emb := &fakeProvider{name: "embedded", result: Result{LRCContent: "纯文本一行", Source: "embedded"}, calls: &embCalls}
	lrc := &fakeProvider{name: "lrclib", result: Result{LRCContent: "[00:01.00]同步", Source: "lrclib"}}
	svc := NewLyricsService(d, emb, lrc)

	out, err := svc.UpgradeToSynced(context.Background(), "t1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Status != "upgraded" || out.Source != "lrclib" {
		t.Fatalf("out=%+v", out)
	}
	if embCalls != 0 {
		t.Errorf("embedded 应被跳过，实际调用 %d 次", embCalls)
	}
	var got string
	d.QueryRow(`SELECT lrc_content FROM lyrics WHERE track_id='t1'`).Scan(&got)
	if got != "[00:01.00]同步" {
		t.Errorf("应替换为同步歌词，得到 %q", got)
	}
}

func TestUpgradeToSynced_NoSyncedKeepsExisting(t *testing.T) {
	d := newServiceTestDB(t)
	if _, err := d.Exec(`INSERT INTO lyrics(track_id,lrc_content,source) VALUES('t1','纯文本一行','embedded')`); err != nil {
		t.Fatal(err)
	}
	emb := &fakeProvider{name: "embedded", result: Result{LRCContent: "纯文本一行", Source: "embedded"}}
	lrc := &fakeProvider{name: "lrclib", result: Result{LRCContent: "还是纯文本无时间轴", Source: "lrclib"}}
	svc := NewLyricsService(d, emb, lrc)

	out, err := svc.UpgradeToSynced(context.Background(), "t1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Status != "no_synced" {
		t.Errorf("应 no_synced，得到 %q", out.Status)
	}
	var got string
	d.QueryRow(`SELECT lrc_content FROM lyrics WHERE track_id='t1'`).Scan(&got)
	if got != "纯文本一行" {
		t.Errorf("原歌词不应被改，得到 %q", got)
	}
}

func TestUpgradeToSynced_YRCCountsSynced(t *testing.T) {
	d := newServiceTestDB(t)
	lrc := &fakeProvider{name: "lrclib", result: Result{YRCContent: `{"lines":[{"start":1}]}`, Source: "lrclib"}}
	svc := NewLyricsService(d, lrc)

	out, err := svc.UpgradeToSynced(context.Background(), "t1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Status != "upgraded" {
		t.Errorf("YRC 命中应算同步 upgraded，得到 %q", out.Status)
	}
}

func TestUpgradeToSynced_TrackNotFound(t *testing.T) {
	d := newServiceTestDB(t)
	svc := NewLyricsService(d)
	if _, err := svc.UpgradeToSynced(context.Background(), "nope"); !errors.Is(err, ErrTrackNotFound) {
		t.Errorf("want ErrTrackNotFound, got %v", err)
	}
}

func TestUpgradeStaleLyrics_UpgradesPlainText(t *testing.T) {
	d := newServiceTestDB(t)
	if _, err := d.Exec(`INSERT INTO lyrics(track_id,lrc_content,source,sync_checked) VALUES('t1','纯文本一行','embedded',0)`); err != nil {
		t.Fatal(err)
	}
	lrc := &fakeProvider{name: "lrclib", result: Result{LRCContent: "[00:01.00]同步", Source: "lrclib"}}
	svc := NewLyricsService(d, lrc)

	n, err := svc.UpgradeStaleLyrics(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if n != 1 {
		t.Errorf("应升级 1 首，得到 %d", n)
	}
	var got string
	var checked int
	d.QueryRow(`SELECT lrc_content, sync_checked FROM lyrics WHERE track_id='t1'`).Scan(&got, &checked)
	if got != "[00:01.00]同步" || checked != 1 {
		t.Errorf("应替换为同步且置 checked=1，得到 lrc=%q checked=%d", got, checked)
	}
}

func TestUpgradeStaleLyrics_NoSyncedMarksChecked(t *testing.T) {
	d := newServiceTestDB(t)
	if _, err := d.Exec(`INSERT INTO lyrics(track_id,lrc_content,source,sync_checked) VALUES('t1','纯文本一行','embedded',0)`); err != nil {
		t.Fatal(err)
	}
	lrc := &fakeProvider{name: "lrclib", result: Result{LRCContent: "还是纯文本", Source: "lrclib"}}
	svc := NewLyricsService(d, lrc)

	n, err := svc.UpgradeStaleLyrics(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if n != 0 {
		t.Errorf("无同步版应升级 0 首，得到 %d", n)
	}
	var got string
	var checked int
	d.QueryRow(`SELECT lrc_content, sync_checked FROM lyrics WHERE track_id='t1'`).Scan(&got, &checked)
	if got != "纯文本一行" || checked != 1 {
		t.Errorf("原文应保留但置 checked=1，得到 lrc=%q checked=%d", got, checked)
	}
}

func TestUpgradeStaleLyrics_AlreadySyncedMarksOnly(t *testing.T) {
	d := newServiceTestDB(t)
	if _, err := d.Exec(`INSERT INTO lyrics(track_id,lrc_content,source,sync_checked) VALUES('t1','[00:01.00]已同步','lrclib',0)`); err != nil {
		t.Fatal(err)
	}
	calls := 0
	lrc := &fakeProvider{name: "lrclib", result: Result{LRCContent: "[00:02.00]x", Source: "lrclib"}, calls: &calls}
	svc := NewLyricsService(d, lrc)

	n, err := svc.UpgradeStaleLyrics(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if n != 0 {
		t.Errorf("已同步不计升级，得到 %d", n)
	}
	if calls != 0 {
		t.Errorf("已同步不应调 provider，实际 %d 次", calls)
	}
	var checked int
	d.QueryRow(`SELECT sync_checked FROM lyrics WHERE track_id='t1'`).Scan(&checked)
	if checked != 1 {
		t.Errorf("已同步也应置 checked=1，得到 %d", checked)
	}
}

func TestUpgradeStaleLyrics_TransientErrorNoMark(t *testing.T) {
	d := newServiceTestDB(t)
	if _, err := d.Exec(`INSERT INTO lyrics(track_id,lrc_content,source,sync_checked) VALUES('t1','纯文本一行','embedded',0)`); err != nil {
		t.Fatal(err)
	}
	lrc := &fakeProvider{name: "lrclib", err: errors.New("network boom")}
	svc := NewLyricsService(d, lrc)

	n, err := svc.UpgradeStaleLyrics(context.Background())
	if err != nil {
		t.Fatalf("批处理本身不应报错: %v", err)
	}
	if n != 0 {
		t.Errorf("瞬时错误升级 0 首，得到 %d", n)
	}
	var checked int
	d.QueryRow(`SELECT sync_checked FROM lyrics WHERE track_id='t1'`).Scan(&checked)
	if checked != 0 {
		t.Errorf("瞬时错误不应置 checked，得到 %d", checked)
	}
}

func TestUpgradeStaleLyrics_SkipsAlreadyChecked(t *testing.T) {
	d := newServiceTestDB(t)
	if _, err := d.Exec(`INSERT INTO lyrics(track_id,lrc_content,source,sync_checked) VALUES('t1','纯文本一行','embedded',1)`); err != nil {
		t.Fatal(err)
	}
	calls := 0
	lrc := &fakeProvider{name: "lrclib", result: Result{LRCContent: "[00:01.00]x", Source: "lrclib"}, calls: &calls}
	svc := NewLyricsService(d, lrc)

	n, err := svc.UpgradeStaleLyrics(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if n != 0 || calls != 0 {
		t.Errorf("已 checked 的不应处理，得到 n=%d calls=%d", n, calls)
	}
}
