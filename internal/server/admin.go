package server

import (
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/store"
)

type leonardoAdminAccount struct {
	ID             int64
	Status         string
	StatusClass    string
	Balance        int64
	JWTExpiry      string
	LastChecked    string
	RefreshFailure string
	Recoverable    string
}

type leonardoAdminView struct {
	GeneratedAt  string
	Total        int
	Ready        int
	Cooling      int
	Disabled     int
	TotalBalance int64
	Accounts     []leonardoAdminAccount
}

func (s *Server) handleLeonardoAdmin(c *gin.Context) {
	if !requestIsLoopback(c) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Leonardo 高级后台仅允许本机访问"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Frame-Options", "DENY")

	rows, err := s.store.ListCookies()
	if err != nil {
		c.String(http.StatusInternalServerError, "无法读取 Leonardo 账号状态")
		return
	}
	view := buildLeonardoAdminView(rows, time.Now())
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	if err := leonardoAdminTemplate.Execute(c.Writer, view); err != nil {
		return
	}
}

func (s *Server) handleLeonardoAdminStatus(c *gin.Context) {
	if !requestIsLoopback(c) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Leonardo 服务状态仅允许本机访问"})
		return
	}
	rows, err := s.store.ListCookies()
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "无法读取 Leonardo 服务状态"})
		return
	}
	view := buildLeonardoAdminView(rows, time.Now())
	s.videoMu.RLock()
	activeLeonardoJobs := 0
	for _, job := range s.videoJobs {
		if job != nil && job.Status != "completed" && job.Status != "failed" {
			activeLeonardoJobs++
		}
	}
	s.videoMu.RUnlock()
	s.adobeMu.RLock()
	activeAdobeJobs := 0
	for _, job := range s.adobeJobs {
		if job != nil && job.Status != "completed" && job.Status != "failed" {
			activeAdobeJobs++
		}
	}
	s.adobeMu.RUnlock()
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"status":         "ok",
		"total":          view.Total,
		"ready":          view.Ready,
		"cooling":        view.Cooling,
		"disabled":       view.Disabled,
		"total_balance":  view.TotalBalance,
		"active_tasks":   activeLeonardoJobs + activeAdobeJobs,
		"leonardo_tasks": activeLeonardoJobs,
	})
}

func buildLeonardoAdminView(rows []store.Cookie, now time.Time) leonardoAdminView {
	view := leonardoAdminView{
		GeneratedAt: now.Format("2006-01-02 15:04:05"),
		Total:       len(rows),
		Accounts:    make([]leonardoAdminAccount, 0, len(rows)),
	}
	for _, row := range rows {
		status, class := leonardoAdminCookieStatus(row)
		switch class {
		case "ok":
			view.Ready++
		case "warn":
			view.Cooling++
		default:
			view.Disabled++
		}
		view.TotalBalance += row.LastBalance
		view.Accounts = append(view.Accounts, leonardoAdminAccount{
			ID:             row.ID,
			Status:         status,
			StatusClass:    class,
			Balance:        row.LastBalance,
			JWTExpiry:      adminTimestamp(row.JWTExpiresAt),
			LastChecked:    adminTimestamp(row.LastCheckedAt),
			RefreshFailure: adminRefreshFailure(row),
			Recoverable:    adminCookieRecoverable(row.Value),
		})
	}
	return view
}

func leonardoAdminCookieStatus(row store.Cookie) (string, string) {
	if row.IsActive != 1 {
		return "已停用", "bad"
	}
	switch strings.ToLower(strings.TrimSpace(row.SessionStatus)) {
	case "temporary_unavailable":
		return "刷新冷却", "warn"
	case "invalid", "abnormal":
		return "会话异常", "bad"
	}
	if row.LastBalance <= 0 {
		return "余额耗尽", "warn"
	}
	return "可用", "ok"
}

func adminTimestamp(value int64) string {
	if value <= 0 {
		return "-"
	}
	return time.Unix(value, 0).Local().Format("01-02 15:04:05")
}

func adminRefreshFailure(row store.Cookie) string {
	if row.RefreshFails > 0 {
		return "连续失败 " + adminInt(row.RefreshFails) + " 次"
	}
	if strings.TrimSpace(row.DisabledReason) != "" {
		return "需要重新认证"
	}
	if strings.TrimSpace(row.RefreshReason) != "" || strings.TrimSpace(row.LastError) != "" {
		return "最近刷新失败"
	}
	return "正常"
}

func adminInt(value int) string {
	if value <= 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

func adminCookieRecoverable(value string) string {
	lower := strings.ToLower(value)
	if strings.Contains(lower, "session_token") || strings.Contains(lower, "session-token") {
		return "长期 Cookie"
	}
	return "短期会话"
}

var leonardoAdminTemplate = template.Must(template.New("leonardo-admin").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <meta http-equiv="refresh" content="20">
  <title>Leonardo 高级后台</title>
  <style>
    :root{color-scheme:dark;--bg:#0d1015;--panel:#151922;--line:#292f3a;--text:#edf1f7;--muted:#98a2b3;--green:#38c98b;--amber:#e3ad4f;--red:#ef6b73;--blue:#67a7ff}
    *{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--text);font:14px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif;letter-spacing:0}.wrap{max-width:1180px;margin:0 auto;padding:28px 22px 48px}
    header{display:flex;align-items:center;justify-content:space-between;gap:18px;margin-bottom:20px}h1{font-size:22px;margin:0}p{margin:4px 0 0;color:var(--muted)}.actions{display:flex;gap:8px}.btn{border:1px solid var(--line);color:var(--text);background:#191e28;padding:8px 12px;text-decoration:none;border-radius:6px}.btn:hover{border-color:var(--blue)}
    .stats{display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:10px;margin-bottom:16px}.stat,.table{border:1px solid var(--line);background:var(--panel);border-radius:7px}.stat{padding:14px}.stat span{color:var(--muted);font-size:12px}.stat strong{display:block;font-size:22px;margin-top:5px}
    .table{overflow:hidden}table{width:100%;border-collapse:collapse}th,td{text-align:left;padding:12px 14px;border-bottom:1px solid var(--line);white-space:nowrap}th{font-size:12px;color:var(--muted);font-weight:600;background:#12161e}tr:last-child td{border-bottom:0}.tag{display:inline-block;border:1px solid currentColor;padding:2px 7px;border-radius:999px;font-size:12px}.ok{color:var(--green)}.warn{color:var(--amber)}.bad{color:var(--red)}.empty{padding:42px;text-align:center;color:var(--muted)}footer{margin-top:14px;color:var(--muted);font-size:12px}
    @media(max-width:780px){header{align-items:flex-start;flex-direction:column}.stats{grid-template-columns:repeat(2,minmax(0,1fr))}.table{overflow-x:auto}.wrap{padding:18px 12px}}
  </style>
</head>
<body><main class="wrap">
  <header><div><h1>Leonardo 本地反代</h1><p>高级状态后台 · 仅本机访问 · 自动刷新</p></div><div class="actions"><a class="btn" href="/admin">刷新</a><a class="btn" href="/infinite-canvas/#/canvas">无限画布</a></div></header>
  <section class="stats"><div class="stat"><span>账号总数</span><strong>{{.Total}}</strong></div><div class="stat"><span>可用</span><strong class="ok">{{.Ready}}</strong></div><div class="stat"><span>冷却 / 耗尽</span><strong class="warn">{{.Cooling}}</strong></div><div class="stat"><span>停用 / 异常</span><strong class="bad">{{.Disabled}}</strong></div><div class="stat"><span>余额合计</span><strong>{{.TotalBalance}}</strong></div></section>
  <section class="table">{{if .Accounts}}<table><thead><tr><th>本地账号</th><th>状态</th><th>余额</th><th>会话类型</th><th>JWT 到期</th><th>最近检查</th><th>刷新状态</th></tr></thead><tbody>{{range .Accounts}}<tr><td>账号 #{{.ID}}</td><td><span class="tag {{.StatusClass}}">{{.Status}}</span></td><td>{{.Balance}}</td><td>{{.Recoverable}}</td><td>{{.JWTExpiry}}</td><td>{{.LastChecked}}</td><td>{{.RefreshFailure}}</td></tr>{{end}}</tbody></table>{{else}}<div class="empty">账号池为空</div>{{end}}</section>
  <footer>状态时间：{{.GeneratedAt}}。此页面不显示 Cookie、JWT、邮箱、完整账号 ID 或 API Key。</footer>
</main></body></html>`))
