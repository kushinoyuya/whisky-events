package htmltext

import "testing"

func TestExtract(t *testing.T) {
	page := `<html><head><title>ウイスキー祭 &amp; 試飲会</title><style>p{}</style></head>
<body><nav>メニュー</nav><!-- note -->
<h1>ウイスキーフェスティバル</h1>
<p>日時：<b>2026年11月7日</b>&nbsp;10:00〜</p>
<script>alert(1)</script>
<ul><li>会場：東京国際フォーラム</li></ul>
<footer>copyright</footer></body></html>`

	want := "ウイスキーフェスティバル\n日時：2026年11月7日 10:00〜\n会場：東京国際フォーラム"
	if got := Extract(page); got != want {
		t.Errorf("Extract =\n%q\nwant\n%q", got, want)
	}
	if got := Title(page); got != "ウイスキー祭 & 試飲会" {
		t.Errorf("Title = %q", got)
	}
}
