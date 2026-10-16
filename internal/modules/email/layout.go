package email

import "html"

// wrapLayout puts template content into the shared GreenRoot email shell so
// templates hold only their message and branding changes in one place.
// Inline styles and tables because many mail clients ignore <style>.
func wrapLayout(subject, content string) string {
	return `<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>` + html.EscapeString(subject) + `</title></head>
<body style="margin:0;padding:0;background:#f4f7f5;font-family:Arial,Helvetica,sans-serif;color:#1f2933;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#f4f7f5;padding:24px 12px;">
<tr><td align="center">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:560px;background:#ffffff;border-radius:12px;overflow:hidden;border:1px solid #e3e8e5;">
<tr><td style="background:#14532d;padding:18px 24px;color:#ffffff;font-size:20px;font-weight:bold;">GreenRoot</td></tr>
<tr><td style="padding:24px;font-size:15px;line-height:1.6;">` + content + `</td></tr>
<tr><td style="padding:16px 24px;border-top:1px solid #e3e8e5;font-size:12px;color:#6b7c72;">
You received this email because of activity on your GreenRoot account. If this wasn't you, you can ignore it.
</td></tr>
</table>
</td></tr>
</table>
</body></html>`
}
