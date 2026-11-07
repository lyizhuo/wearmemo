package main

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"
)

// The web UI is deliberately three small pages rendered by html/template and
// compiled into the binary. No SPA, no build step, no static asset directory.
var pageHead = `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>WearMemo relay</title><style>
:root{color-scheme:dark}
*{box-sizing:border-box}
body{margin:0;background:#0b0b0c;color:#e8e8ea;font:15px/1.6 -apple-system,system-ui,"PingFang SC","Microsoft YaHei",sans-serif}
.wrap{max-width:680px;margin:0 auto;padding:48px 20px 80px}
h1{font-size:20px;font-weight:600;margin:0 0 4px}
.sub{color:#8b8b93;font-size:13px;margin-bottom:28px}
h2{font-size:14px;font-weight:600;color:#b8b8c0;margin:36px 0 12px;letter-spacing:.02em}
label{display:block;font-size:12px;color:#8b8b93;margin:14px 0 6px}
input{width:100%;padding:9px 11px;background:#17171a;border:1px solid #2a2a30;border-radius:8px;color:#e8e8ea;font:inherit}
input:focus{outline:none;border-color:#5b5bd6}
button{padding:9px 16px;border:0;border-radius:8px;background:#5b5bd6;color:#fff;font:inherit;font-weight:500;cursor:pointer}
button.ghost{background:#26262c;color:#d8d8de}
button.small{padding:5px 11px;font-size:12px}
.row{display:flex;gap:10px;align-items:center;flex-wrap:wrap;margin-top:22px}
.card{background:#141417;border:1px solid #232329;border-radius:12px;padding:16px;margin-top:14px}
.mono{font-family:ui-monospace,SFMono-Regular,Menlo,monospace}
.code{font-size:34px;letter-spacing:.22em;font-weight:600;color:#fff}
.muted{color:#8b8b93;font-size:12px}
.bad{color:#ff6b6b}.good{color:#3ddc84}.warn{color:#ffb020}
table{width:100%;border-collapse:collapse;margin-top:10px}
td,th{text-align:left;padding:8px 6px;border-bottom:1px solid #212127;font-size:13px;vertical-align:middle}
th{color:#8b8b93;font-weight:500;font-size:12px}
a{color:#8f8ff0}
</style></head><body><div class="wrap">`

var pageFoot = `</div></body></html>`

type web struct {
	settings *SettingsStore
	devices  *DeviceStore
	admin    string
}

func (w *web) authed(r *http.Request) bool {
	c, err := r.Cookie("wm_admin")
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(w.admin)) == 1
}

func (w *web) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		if !w.authed(r) {
			http.Redirect(rw, r, "/login", http.StatusSeeOther)
			return
		}
		next(rw, r)
	}
}

func render(rw http.ResponseWriter, body string) {
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(rw, pageHead+body+pageFoot)
}

func (w *web) home(rw http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(rw, r)
		return
	}
	if !w.settings.Get().Configured {
		http.Redirect(rw, r, "/setup", http.StatusSeeOther)
		return
	}
	http.Redirect(rw, r, "/admin", http.StatusSeeOther)
}

func (w *web) loginPage(rw http.ResponseWriter, r *http.Request) {
	bad := r.URL.Query().Get("bad") == "1"
	msg := ""
	if bad {
		msg = `<p class="bad">口令不对</p>`
	}
	render(rw, `<h1>WearMemo relay</h1><div class="sub">管理口令</div>`+msg+
		`<form method="post" action="/login"><input type="password" name="token" autofocus placeholder="RELAY_ADMIN_TOKEN">
<div class="row"><button>进入</button></div></form>`)
}

func (w *web) loginSubmit(rw http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.FormValue("token")), []byte(w.admin)) != 1 {
		http.Redirect(rw, r, "/login?bad=1", http.StatusSeeOther)
		return
	}
	http.SetCookie(rw, &http.Cookie{
		Name:     "wm_admin",
		Value:    w.admin,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400 * 30,
		Secure:   r.TLS != nil,
	})
	http.Redirect(rw, r, "/admin", http.StatusSeeOther)
}

func (w *web) setupPage(rw http.ResponseWriter, r *http.Request) {
	s := w.settings.Get()
	render(rw, `<h1>首次配置</h1><div class="sub">填一次，之后都在这个页面改</div>
<form method="post" action="/setup">
<h2>ASR</h2>
<label>服务地址</label><input name="asr_base" value="`+template.HTMLEscapeString(s.ASR.Base)+`" placeholder="http://127.0.0.1:11434">
<label>模型</label><input name="asr_model" value="`+template.HTMLEscapeString(s.ASR.Model)+`" placeholder="frozenlab/qwen3-asr:1.7b">
<label>语言白名单（逗号分隔）</label><input name="asr_langs" value="`+template.HTMLEscapeString(strings.Join(s.ASR.Langs, ","))+`">
<label>静音阈值 RMS（低于此值不调用模型）</label><input name="asr_minrms" value="`+fmt.Sprintf("%.0f", s.ASR.MinRMS)+`">
<h2>Blinko</h2>
<label>服务地址</label><input name="blinko_base" value="`+template.HTMLEscapeString(s.Blinko.Base)+`" placeholder="https://memo.example.com">
<label>API Token</label><input name="blinko_token" type="password" value="`+template.HTMLEscapeString(s.Blinko.Token)+`">
<div class="row"><button>保存</button></div>
</form>`)
}

func (w *web) setupSubmit(rw http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(rw, "bad form", http.StatusBadRequest)
		return
	}
	s := w.settings.Get()
	s.ASR.Base = strings.TrimSpace(r.FormValue("asr_base"))
	s.ASR.Model = strings.TrimSpace(r.FormValue("asr_model"))
	s.ASR.Langs = splitLangs(r.FormValue("asr_langs"))
	s.ASR.MinRMS = envFloatValue(r.FormValue("asr_minrms"), s.ASR.MinRMS)
	if s.ASR.KeepAlive == "" {
		s.ASR.KeepAlive = "30m"
	}
	s.Blinko.Base = strings.TrimRight(strings.TrimSpace(r.FormValue("blinko_base")), "/")
	s.Blinko.Token = strings.TrimSpace(r.FormValue("blinko_token"))
	_ = w.settings.Save(s)
	http.Redirect(rw, r, "/admin", http.StatusSeeOther)
}

func (w *web) adminPage(rw http.ResponseWriter, r *http.Request) {
	s := w.settings.Get()
	state := `<span class="warn">未完成配置</span>`
	if s.Configured {
		state = `<span class="good">已配置</span>`
	}

	var rows strings.Builder
	for _, d := range w.devices.List() {
		status := `<span class="good">在线</span>`
		action := `<form method="post" action="/admin/revoke" style="margin:0">
<input type="hidden" name="id" value="` + template.HTMLEscapeString(d.ID) + `">
<button class="ghost small">吊销</button></form>`
		if d.RevokedAt != nil {
			status = `<span class="muted">已吊销</span>`
			action = ""
		}
		last := "—"
		if !d.LastSeenAt.IsZero() {
			last = d.LastSeenAt.Local().Format("01-02 15:04")
		}
		fmt.Fprintf(&rows, `<tr><td>%s</td><td class="muted mono">%s</td><td>%s</td><td class="muted">%s</td><td>%s</td></tr>`,
			template.HTMLEscapeString(d.Name), template.HTMLEscapeString(d.ID[:6]), status, last, action)
	}
	if rows.Len() == 0 {
		rows.WriteString(`<tr><td colspan="5" class="muted">还没有设备</td></tr>`)
	}

	render(rw, `<h1>WearMemo relay</h1><div class="sub">ASR `+state+`</div>
<h2>配对一台手表</h2>
<div class="card">
<div class="row" style="margin:0"><button onclick="mint()">生成配对码</button>
<span class="muted">5 分钟内有效，只能用一次</span></div>
<div id="out" style="display:none;margin-top:16px">
<div class="code mono" id="code"></div>
<div class="muted" id="exp"></div></div>
</div>
<script>
async function mint(){
  const r = await fetch('/admin/pair',{method:'POST'});
  if(!r.ok){ alert('生成失败: '+r.status); return; }
  const j = await r.json();
  document.getElementById('code').textContent = j.code.slice(0,3)+'-'+j.code.slice(3);
  document.getElementById('exp').textContent = '在手表上输入这串码，' + new Date(j.expiresAt).toLocaleTimeString() + ' 前有效';
  document.getElementById('out').style.display='block';
}
</script>
<h2>设备</h2>
<table><tr><th>名称</th><th>ID</th><th>状态</th><th>最近使用</th><th></th></tr>`+rows.String()+`</table>
<h2>配置</h2>
<div class="row"><a href="/setup">修改 ASR / Blinko</a></div>`)
}

func (w *web) mintCode(rw http.ResponseWriter, r *http.Request) {
	code, expires := w.devices.NewPairCode()
	rw.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(rw).Encode(map[string]any{
		"code":      code,
		"expiresAt": expires.UTC().Format(time.RFC3339),
	})
}

func (w *web) revoke(rw http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	w.devices.Revoke(r.FormValue("id"))
	http.Redirect(rw, r, "/admin", http.StatusSeeOther)
}

// POST /v1/pair — the only thing the watch sends before it has a token.
func (w *web) pairRedeem(rw http.ResponseWriter, r *http.Request) {
	var body struct {
		Code       string `json:"code"`
		DeviceName string `json:"deviceName"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(rw, r.Body, 4<<10)).Decode(&body); err != nil {
		http.Error(rw, "bad json", http.StatusBadRequest)
		return
	}

	code := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(body.Code), "-", ""))
	token, deviceID, ok := w.devices.Redeem(code, body.DeviceName)
	if !ok {
		http.Error(rw, "code invalid or expired", http.StatusNotFound)
		return
	}

	rw.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(rw).Encode(map[string]any{
		"token":    token,
		"deviceId": deviceID,
	})
}
