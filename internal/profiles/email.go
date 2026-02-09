package profiles

import (
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
)

type EmailProfile struct{}

func (p *EmailProfile) ID() string { return "email" }

func (p *EmailProfile) Tools() []Tool {
	return []Tool{
		{
			Name:        "send_email",
			Description: "Send an email via SMTP",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"to":      map[string]interface{}{"type": "string", "description": "Recipient email address(es), comma-separated"},
					"subject": map[string]interface{}{"type": "string", "description": "Email subject"},
					"body":    map[string]interface{}{"type": "string", "description": "Email body (plain text)"},
					"cc":      map[string]interface{}{"type": "string", "description": "CC recipients (optional, comma-separated)"},
					"reply_to": map[string]interface{}{"type": "string", "description": "Reply-To address (optional)"},
				},
				"required": []string{"to", "subject", "body"},
			},
		},
		{
			Name:        "send_html_email",
			Description: "Send an HTML email via SMTP",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"to":      map[string]interface{}{"type": "string", "description": "Recipient email address(es), comma-separated"},
					"subject": map[string]interface{}{"type": "string", "description": "Email subject"},
					"html":    map[string]interface{}{"type": "string", "description": "HTML content"},
					"cc":      map[string]interface{}{"type": "string", "description": "CC recipients (optional, comma-separated)"},
				},
				"required": []string{"to", "subject", "html"},
			},
		},
		{
			Name:        "validate_email",
			Description: "Validate an email address (format check + MX record lookup)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"email": map[string]interface{}{"type": "string", "description": "Email address to validate"},
				},
				"required": []string{"email"},
			},
		},
	}
}

func (p *EmailProfile) CallTool(name string, args map[string]interface{}, env map[string]string) (string, error) {
	switch name {
	case "send_email":
		return p.sendEmail(args, env, false)
	case "send_html_email":
		return p.sendEmail(args, env, true)
	case "validate_email":
		return p.validateEmail(args)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (p *EmailProfile) sendEmail(args map[string]interface{}, env map[string]string, isHTML bool) (string, error) {
	host := env["SMTP_HOST"]
	portStr := env["SMTP_PORT"]
	user := env["SMTP_USER"]
	pass := env["SMTP_PASS"]
	from := env["FROM_ADDRESS"]

	if host == "" || from == "" {
		return "", fmt.Errorf("SMTP_HOST and FROM_ADDRESS must be configured")
	}
	if portStr == "" {
		portStr = "587"
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", fmt.Errorf("invalid SMTP_PORT: %s", portStr)
	}

	to := getStr(args, "to")
	subject := getStr(args, "subject")
	if to == "" || subject == "" {
		return "", fmt.Errorf("to and subject are required")
	}

	var body string
	if isHTML {
		body = getStr(args, "html")
	} else {
		body = getStr(args, "body")
	}
	if body == "" {
		return "", fmt.Errorf("email body is required")
	}

	fromName := env["FROM_NAME"]
	if fromName == "" {
		fromName = "Dublyo MCP"
	}

	recipients := parseEmails(to)
	ccList := parseEmails(getStr(args, "cc"))
	allRecipients := append(recipients, ccList...)

	// Build message
	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("From: %s <%s>\r\n", fromName, from))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(recipients, ", ")))
	if len(ccList) > 0 {
		msg.WriteString(fmt.Sprintf("Cc: %s\r\n", strings.Join(ccList, ", ")))
	}
	if replyTo := getStr(args, "reply_to"); replyTo != "" {
		msg.WriteString(fmt.Sprintf("Reply-To: %s\r\n", replyTo))
	}
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	if isHTML {
		msg.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n")
	} else {
		msg.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n")
	}
	msg.WriteString("\r\n")
	msg.WriteString(body)

	addr := fmt.Sprintf("%s:%d", host, port)
	var auth smtp.Auth
	if user != "" && pass != "" {
		auth = smtp.PlainAuth("", user, pass, host)
	}

	if err := smtp.SendMail(addr, auth, from, allRecipients, []byte(msg.String())); err != nil {
		return "", fmt.Errorf("failed to send email: %s", err)
	}

	return fmt.Sprintf("Email sent successfully!\nTo: %s\nSubject: %s\nFrom: %s <%s>",
		strings.Join(recipients, ", "), subject, fromName, from), nil
}

func (p *EmailProfile) validateEmail(args map[string]interface{}) (string, error) {
	email := getStr(args, "email")
	if email == "" {
		return "", fmt.Errorf("email is required")
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("Email: %s", email))

	// Basic format check
	parts := strings.Split(email, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		lines = append(lines, "Format: INVALID (missing @ or empty parts)")
		return strings.Join(lines, "\n"), nil
	}
	lines = append(lines, "Format: VALID")
	lines = append(lines, fmt.Sprintf("Local Part: %s", parts[0]))
	lines = append(lines, fmt.Sprintf("Domain: %s", parts[1]))

	// MX record check
	mxRecords, err := net.LookupMX(parts[1])
	if err != nil || len(mxRecords) == 0 {
		lines = append(lines, "MX Records: NONE (domain may not accept email)")
	} else {
		var mxNames []string
		for _, mx := range mxRecords {
			mxNames = append(mxNames, fmt.Sprintf("%s (priority %d)", mx.Host, mx.Pref))
		}
		lines = append(lines, fmt.Sprintf("MX Records: %s", strings.Join(mxNames, ", ")))
		lines = append(lines, "Domain: ACCEPTS EMAIL")
	}

	return strings.Join(lines, "\n"), nil
}

func parseEmails(s string) []string {
	if s == "" {
		return nil
	}
	var emails []string
	for _, e := range strings.Split(s, ",") {
		e = strings.TrimSpace(e)
		if e != "" {
			emails = append(emails, e)
		}
	}
	return emails
}
