package mailer

import (
	"bytes"
	"fmt"
	htmltmpl "html/template"
	"strings"
	texttmpl "text/template"
)

// SummaryKV 代表郵件頂部摘要數字列之單一項目。
type SummaryKV struct {
	Label string
	Value string
	Tone  string // default | success | warning | danger
}

// EmailCell 代表明細表格中之單一儲存格。
type EmailCell struct {
	Text      string
	Monospace bool
	BadgeTone string // 空字串表示一般文字，否則為 online | warning | danger | muted
}

// EmailTemplateData 定義所有三種通知信與測試信共用之資料結構，
// 確保 HTML 與純文字（text/plain）產出完全對等之內容。
type EmailTemplateData struct {
	CampusName    string
	Environment   string
	Subject       string
	Preheader     string
	Greeting      string
	Intro         string
	Timezone      string
	SummaryItems  []SummaryKV
	Columns       []string
	Rows          [][]EmailCell
	TruncatedNote string
	PrimaryCTA    *CTAButton
	SecondaryLink *CTAButton
	FooterLines   []string
}

type CTAButton struct {
	Label string
	URL   string
}

const htmlLayoutSource = `<!DOCTYPE html>
<html lang="zh-Hant">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>{{.Subject}}</title>
</head>
<body style="margin:0;padding:0;background-color:#f6f8f6;color:#0f172a;font-family:Arial,Helvetica,'Microsoft JhengHei','PingFang TC','Noto Sans TC',sans-serif;-webkit-font-smoothing:antialiased;">
<div style="display:none;max-height:0;overflow:hidden;font-size:1px;line-height:1px;color:#f6f8f6;">{{.Preheader}}</div>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="width:100%;background-color:#f6f8f6;margin:0;padding:24px 12px;">
  <tr>
    <td align="center">
      <table role="presentation" width="600" cellpadding="0" cellspacing="0" border="0" style="width:100%;max-width:600px;background-color:#ffffff;border:1px solid #e2e8f0;">
        <!-- Header -->
        <tr>
          <td style="padding:24px 24px 16px 24px;border-bottom:1px solid #e2e8f0;">
            <div style="font-size:12px;font-weight:600;color:#475569;margin-bottom:4px;">{{.CampusName}}（IP 管理系統）{{if .Environment}} · {{.Environment}}{{end}}</div>
            <h1 style="margin:0;font-size:20px;line-height:1.35;font-weight:600;color:#0f172a;">{{.Subject}}</h1>
          </td>
        </tr>

        <!-- Greeting & Intro -->
        <tr>
          <td style="padding:20px 24px 12px 24px;font-size:14px;line-height:1.6;color:#1e293b;">
            <p style="margin:0 0 10px 0;">{{.Greeting}}</p>
            <p style="margin:0;">{{.Intro}}</p>
          </td>
        </tr>

        <!-- Summary Strip -->
        {{if .SummaryItems}}
        <tr>
          <td style="padding:8px 24px 16px 24px;">
            <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="width:100%;border:1px solid #e2e8f0;background-color:#eff3f0;">
              <tr>
                {{range .SummaryItems}}
                <td style="padding:12px 10px;border-right:1px solid #e2e8f0;vertical-align:top;">
                  <div style="font-size:12px;color:#475569;margin-bottom:2px;">{{.Label}}</div>
                  <div style="font-size:18px;font-weight:600;font-family:'Courier New',Courier,monospace;color:{{toneColor .Tone}};">{{.Value}}</div>
                </td>
                {{end}}
              </tr>
            </table>
          </td>
        </tr>
        {{end}}

        <!-- Detail Table -->
        {{if .Rows}}
        <tr>
          <td style="padding:4px 24px 16px 24px;">
            <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="width:100%;border-collapse:collapse;border:1px solid #e2e8f0;">
              <thead>
                <tr>
                  {{range .Columns}}
                  <th align="left" style="background-color:#eff3f0;color:#1e293b;font-size:12px;font-weight:600;padding:8px 8px;border-bottom:1px solid #e2e8f0;">{{.}}</th>
                  {{end}}
                </tr>
              </thead>
              <tbody>
                {{range .Rows}}
                <tr>
                  {{range .}}
                  <td style="padding:8px 8px;font-size:13px;line-height:1.4;border-bottom:1px solid #e2e8f0;color:#0f172a;{{if .Monospace}}font-family:'Courier New',Courier,monospace;{{end}}">
                    {{if .BadgeTone}}
                    <span style="display:inline-block;padding:2px 6px;font-size:12px;font-weight:600;background-color:{{badgeBg .BadgeTone}};color:{{badgeText .BadgeTone}};border:1px solid {{badgeBorder .BadgeTone}};">{{.Text}}</span>
                    {{else}}
                    {{.Text}}
                    {{end}}
                  </td>
                  {{end}}
                </tr>
                {{end}}
              </tbody>
            </table>
            {{if .TruncatedNote}}
            <p style="margin:10px 0 0 0;font-size:12px;color:#475569;">{{.TruncatedNote}}</p>
            {{end}}
          </td>
        </tr>
        {{end}}

        <!-- CTA -->
        {{if .PrimaryCTA}}
        <tr>
          <td style="padding:12px 24px 20px 24px;">
            <table role="presentation" cellpadding="0" cellspacing="0" border="0">
              <tr>
                <td align="center" bgcolor="#222222" style="background-color:#222222;">
                  <a href="{{.PrimaryCTA.URL}}" target="_blank" style="display:inline-block;padding:12px 20px;font-size:14px;font-weight:600;color:#ffffff;text-decoration:none;background-color:#222222;">{{.PrimaryCTA.Label}}</a>
                </td>
              </tr>
            </table>
            <p style="margin:10px 0 0 0;font-size:12px;color:#475569;word-break:break-all;">
              若按鈕無法點擊，請複製網址至瀏覽器開啟：<a href="{{.PrimaryCTA.URL}}" style="color:#222222;text-decoration:underline;">{{.PrimaryCTA.URL}}</a>
            </p>
            {{if .SecondaryLink}}
            <p style="margin:10px 0 0 0;font-size:13px;">
              <a href="{{.SecondaryLink.URL}}" style="color:#222222;text-decoration:underline;">{{.SecondaryLink.Label}}（{{.SecondaryLink.URL}}）</a>
            </p>
            {{end}}
          </td>
        </tr>
        {{end}}

        <!-- Footer -->
        <tr>
          <td style="padding:16px 24px;border-top:1px solid #e2e8f0;background-color:#f6f8f6;font-size:12px;line-height:1.5;color:#64748b;">
            {{range .FooterLines}}
            <div style="margin-bottom:4px;">{{.}}</div>
            {{end}}
            <div>顯示時區：{{.Timezone}}。本信件由「{{.CampusName}}」自動寄出，請勿直接回覆。</div>
          </td>
        </tr>
      </table>
    </td>
  </tr>
</table>
</body>
</html>`

const textLayoutSource = `主旨：{{.Subject}}
系統：{{.CampusName}}{{if .Environment}} ({{.Environment}}){{end}}

{{.Greeting}}

{{.Intro}}
{{if .SummaryItems}}
摘要
{{range .SummaryItems}}- {{.Label}}：{{.Value}}
{{end}}{{end}}{{if .Rows}}
明細
{{joinCols .Columns}}
{{range .Rows}}{{joinRow .}}
{{end}}{{if .TruncatedNote}}
註：{{.TruncatedNote}}
{{end}}{{end}}{{if .PrimaryCTA}}
行動連結：
{{.PrimaryCTA.Label}}：{{.PrimaryCTA.URL}}
{{end}}{{if .SecondaryLink}}
{{.SecondaryLink.Label}}：{{.SecondaryLink.URL}}
{{end}}
---
{{range .FooterLines}}{{.}}
{{end}}顯示時區：{{.Timezone}}。本信件由「{{.CampusName}}」自動寄出，請勿直接回覆。
`

var (
	parsedHTML = htmltmpl.Must(htmltmpl.New("mail_html").Funcs(htmltmpl.FuncMap{
		"toneColor": func(tone string) string {
			switch tone {
			case "success":
				return "#2f6b3f"
			case "warning":
				return "#8b5b21"
			case "danger":
				return "#9f2d20"
			default:
				return "#0f172a"
			}
		},
		"badgeBg": func(tone string) string {
			switch tone {
			case "online":
				return "#f0fdf4"
			case "warning":
				return "#fff7e9"
			case "danger":
				return "#fff1ee"
			default:
				return "#f6f8f6"
			}
		},
		"badgeText": func(tone string) string {
			switch tone {
			case "online":
				return "#15803d"
			case "warning":
				return "#8b5b21"
			case "danger":
				return "#9f2d20"
			default:
				return "#475569"
			}
		},
		"badgeBorder": func(tone string) string {
			switch tone {
			case "online":
				return "#bbf7d0"
			case "warning":
				return "#dfbf91"
			case "danger":
				return "#dfb7b0"
			default:
				return "#e2e8f0"
			}
		},
	}).Parse(htmlLayoutSource))

	parsedText = texttmpl.Must(texttmpl.New("mail_text").Funcs(texttmpl.FuncMap{
		"joinCols": func(cols []string) string {
			return strings.Join(cols, " | ")
		},
		"joinRow": func(cells []EmailCell) string {
			parts := make([]string, len(cells))
			for i, c := range cells {
				parts[i] = c.Text
			}
			return strings.Join(parts, " | ")
		},
	}).Parse(textLayoutSource))
)

// RenderEmail 同時產生 HTML 與對等純文字版本。所有使用者字串於 HTML 中皆會自動進行跳脫處理。
func RenderEmail(data EmailTemplateData) (htmlBody string, textBody string, err error) {
	if data.CampusName == "" {
		data.CampusName = "Würzburg"
	}
	if data.Timezone == "" {
		data.Timezone = "Asia/Taipei"
	}

	var htmlBuf, textBuf bytes.Buffer
	if err = parsedHTML.Execute(&htmlBuf, data); err != nil {
		return "", "", fmt.Errorf("渲染 HTML 郵件失敗: %w", err)
	}
	if err = parsedText.Execute(&textBuf, data); err != nil {
		return "", "", fmt.Errorf("渲染純文字郵件失敗: %w", err)
	}
	return htmlBuf.String(), textBuf.String(), nil
}
