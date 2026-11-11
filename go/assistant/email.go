package assistant

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// emailPoll is the pause between inbox reads.
	emailPoll = 30 * time.Second
	// emailTimeout bounds one IMAP session or one SMTP send.
	emailTimeout = 30 * time.Second
	// emailPage bounds the messages one receive reads.
	emailPage = 20
	// emailMaxBytes is the largest message the channel reads.
	emailMaxBytes = 256 << 10
	// emailPartDepth bounds nested multipart parts.
	emailPartDepth = 5
)

// EmailConfig is the assistant's own mailbox and the one address it talks
// to. AuthServ is the authserv-id of the receiving server whose
// Authentication-Results header is trusted (mx.google.com for Gmail).
type EmailConfig struct {
	IMAP     string
	SMTP     string
	User     string
	Password string
	Owner    string
	AuthServ string
}

// EmailChannel is a mailbox the owner writes to and the assistant answers from. It
// reads the inbox over IMAP (read-only: messages stay unread and in place)
// and sends over SMTP. A message counts as the owner's only when From is the
// owner's address and the topmost Authentication-Results header, written by
// the configured receiving server, shows DKIM or DMARC passing for the
// owner's domain; From alone is easy to forge. IMAP uses TLS and SMTP uses
// TLS or STARTTLS, except to a loopback host such as a local mail bridge.
type EmailChannel struct {
	config EmailConfig
	domain string
	dial   func(ctx context.Context, network, address string) (net.Conn, error)
}

func NewEmailChannel(config EmailConfig) (*EmailChannel, error) {
	owner, err := mail.ParseAddress(config.Owner)
	if err != nil {
		return nil, fmt.Errorf("email: -email-owner %q is not an address", config.Owner)
	}
	config.Owner = strings.ToLower(owner.Address)
	user, err := mail.ParseAddress(config.User)
	if err != nil {
		return nil, fmt.Errorf("email: -email-user %q is not an address", config.User)
	}
	config.User = user.Address
	if config.Password == "" {
		return nil, errors.New("email needs the mailbox password (MINILOOP_EMAIL_PASSWORD)")
	}
	for flag, address := range map[string]string{"-email-imap": config.IMAP, "-email-smtp": config.SMTP} {
		if _, _, err := net.SplitHostPort(address); err != nil {
			return nil, fmt.Errorf("email: %s %q must be host:port", flag, address)
		}
	}
	if strings.TrimSpace(config.AuthServ) == "" {
		return nil, errors.New("email needs -email-authserv, the receiving server whose Authentication-Results header is trusted (mx.google.com for Gmail)")
	}
	if strings.EqualFold(config.Owner, config.User) {
		return nil, errors.New("email: -email-owner must be your own address, not the assistant's mailbox")
	}
	return &EmailChannel{config: config, domain: domainOf(config.Owner), dial: (&net.Dialer{Timeout: emailTimeout}).DialContext}, nil
}

func (e *EmailChannel) Name() string            { return RemoteEmail }
func (e *EmailChannel) Label() string           { return "邮件" }
func (e *EmailChannel) Account() string         { return e.config.Owner + " ↔ " + e.config.User }
func (e *EmailChannel) Interval() time.Duration { return emailPoll }

// connect opens a connection with a deadline: TLS unless the host is
// loopback.
func (e *EmailChannel) connect(ctx context.Context, address string, implicitTLS bool) (net.Conn, string, error) {
	host, _, _ := net.SplitHostPort(address)
	conn, err := e.dial(ctx, "tcp", address)
	if err != nil {
		return nil, host, err
	}
	if err := conn.SetDeadline(time.Now().Add(emailTimeout)); err != nil {
		return nil, host, errors.Join(err, conn.Close())
	}
	if implicitTLS && !loopbackHost(host) {
		secure := tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
		if err := secure.HandshakeContext(ctx); err != nil {
			return nil, host, errors.Join(err, conn.Close())
		}
		return secure, host, nil
	}
	return conn, host, nil
}

// Receive reads the inbox after cursor ("UIDVALIDITY:last UID"). A first
// start, or a mailbox whose UIDVALIDITY changed, starts at the newest
// message instead of replaying the mailbox.
func (e *EmailChannel) Receive(ctx context.Context, cursor string) (RemoteBatch, error) {
	conn, _, err := e.connect(ctx, e.config.IMAP, true)
	if err != nil {
		return RemoteBatch{}, fmt.Errorf("IMAP %s: %w", e.config.IMAP, err)
	}
	defer conn.Close()
	client, err := newIMAP(conn)
	if err != nil {
		return RemoteBatch{}, err
	}
	defer client.logout()
	if err := client.login(e.config.User, e.config.Password); err != nil {
		return RemoteBatch{}, err
	}
	validity, next, err := client.examine()
	if err != nil {
		return RemoteBatch{}, err
	}
	head := fmt.Sprintf("%d:%d", validity, next-1)
	stored, last, ok := strings.Cut(cursor, ":")
	lastUID, parseErr := strconv.ParseUint(last, 10, 64)
	if !ok || parseErr != nil || stored != strconv.FormatUint(validity, 10) {
		return RemoteBatch{Cursor: head}, nil
	}
	uids, err := client.uidsAfter(lastUID)
	if err != nil {
		return RemoteBatch{}, err
	}
	if len(uids) > emailPage {
		uids = uids[:emailPage]
	}
	batch := RemoteBatch{Cursor: cursor}
	if len(uids) == 0 {
		return batch, nil
	}
	sizes, err := client.sizes(uids)
	if err != nil {
		return RemoteBatch{}, err
	}
	for _, uid := range uids {
		if size := sizes[uid]; size > emailMaxBytes {
			batch.Messages = append(batch.Messages, RemoteInbound{Sender: fmt.Sprintf("UID %d", uid), Refused: fmt.Sprintf("邮件太大（%d KB）", size>>10)})
		} else {
			raw, err := client.message(uid)
			if err != nil {
				return RemoteBatch{}, err
			}
			inbound := e.parse(raw)
			if inbound.ID == "" {
				inbound.ID = fmt.Sprintf("uid-%d-%d", validity, uid)
			}
			batch.Messages = append(batch.Messages, inbound)
		}
		batch.Cursor = fmt.Sprintf("%d:%d", validity, uid)
	}
	return batch, nil
}

// parse turns one raw message into an inbound line, or a refusal.
func (e *EmailChannel) parse(raw []byte) RemoteInbound {
	message, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return RemoteInbound{Sender: "未知发件人", Refused: "无法解析的邮件"}
	}
	header := message.Header
	inbound := RemoteInbound{ID: messageID(header.Get("Message-Id"))}
	from, err := mail.ParseAddressList(header.Get("From"))
	if err != nil || len(from) != 1 {
		inbound.Sender, inbound.Refused = "未知发件人", "发件人无法解析"
		return inbound
	}
	inbound.Sender = strings.ToLower(from[0].Address)
	if inbound.Sender != e.config.Owner {
		inbound.Refused = "不是你的地址"
		return inbound
	}
	if !authenticated(header["Authentication-Results"], e.config.AuthServ, e.domain) {
		inbound.Refused = "自称是你但发件人认证没通过（" + e.config.AuthServ + " 没有记下你域名的 DKIM 或 DMARC 通过）"
		return inbound
	}
	inbound.Sender = "你"
	inbound.ReplyTo = messageID(header.Get("In-Reply-To"))
	text, err := plainText(textproto.MIMEHeader(header), message.Body, 0)
	if err != nil {
		inbound.Refused = err.Error()
		return inbound
	}
	inbound.Text = stripQuoted(text)
	if inbound.Text == "" {
		subject, _ := new(mime.WordDecoder).DecodeHeader(header.Get("Subject"))
		inbound.Text = strings.TrimSpace(replyPrefix.ReplaceAllString(subject, ""))
	}
	if inbound.Text == "" {
		inbound.Refused = "邮件是空的"
	}
	return inbound
}

// messageID takes the first <id> of a Message-ID or In-Reply-To header.
func messageID(value string) string {
	value = strings.TrimSpace(value)
	if start := strings.Index(value, "<"); start >= 0 {
		if end := strings.Index(value[start:], ">"); end > 0 {
			return value[start+1 : start+end]
		}
	}
	if fields := strings.Fields(value); len(fields) > 0 {
		return strings.Trim(fields[0], "<>")
	}
	return ""
}

// authenticated reads the topmost Authentication-Results header, the one the
// receiving server prepends. Headers below it may come from the sender. It
// must name authserv and show dmarc=pass for the owner's domain, or
// dkim=pass signed by that domain.
func authenticated(values []string, authserv, domain string) bool {
	if len(values) == 0 {
		return false
	}
	parts := strings.Split(stripComments(values[0]), ";")
	id := strings.Fields(parts[0])
	if len(id) == 0 || !strings.EqualFold(id[0], authserv) {
		return false
	}
	for _, result := range parts[1:] {
		fields := strings.Fields(result)
		if len(fields) == 0 {
			continue
		}
		method, verdict, _ := strings.Cut(fields[0], "=")
		if !strings.EqualFold(verdict, "pass") {
			continue
		}
		properties := map[string]string{}
		for _, field := range fields[1:] {
			if key, value, ok := strings.Cut(field, "="); ok {
				properties[strings.ToLower(key)] = strings.ToLower(strings.Trim(value, `"`))
			}
		}
		switch strings.ToLower(method) {
		case "dmarc":
			if properties["header.from"] == domain {
				return true
			}
		case "dkim":
			if properties["header.d"] == domain || domainOf(properties["header.i"]) == domain {
				return true
			}
		}
	}
	return false
}

// stripComments drops parenthesized comments, which may nest.
func stripComments(value string) string {
	var b strings.Builder
	depth := 0
	for _, r := range value {
		switch {
		case r == '(':
			depth++
		case r == ')' && depth > 0:
			depth--
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// domainOf is the lower-cased part after the last "@".
func domainOf(address string) string {
	if at := strings.LastIndex(address, "@"); at >= 0 {
		return strings.ToLower(address[at+1:])
	}
	return strings.ToLower(address)
}

// plainText finds the message's text: the first text/plain part, else the
// first text/html part with its tags removed.
func plainText(header textproto.MIMEHeader, body io.Reader, depth int) (string, error) {
	mediaType, params, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil {
		mediaType, params = "text/plain", map[string]string{}
	}
	switch {
	case strings.HasPrefix(mediaType, "multipart/"):
		if depth >= emailPartDepth || params["boundary"] == "" {
			return "", errors.New("邮件结构太复杂")
		}
		reader := multipart.NewReader(body, params["boundary"])
		fallback := ""
		for {
			part, err := reader.NextRawPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return "", errors.New("无法解析的邮件")
			}
			partType, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
			if strings.HasPrefix(part.Header.Get("Content-Disposition"), "attachment") {
				continue
			}
			text, err := plainText(part.Header, part, depth+1)
			if err != nil {
				return "", err
			}
			if text == "" {
				continue
			}
			if partType == "text/html" {
				if fallback == "" {
					fallback = text
				}
				continue
			}
			return text, nil
		}
		return fallback, nil
	case mediaType == "text/plain" || mediaType == "text/html":
		data, err := io.ReadAll(io.LimitReader(transferDecoder(header.Get("Content-Transfer-Encoding"), body), emailMaxBytes))
		if err != nil {
			return "", errors.New("邮件正文无法解码")
		}
		if !utf8.Valid(data) {
			charset := firstNonEmpty(params["charset"], "未知")
			return "", fmt.Errorf("不支持的字符集 %s，请用 UTF-8 发送", charset)
		}
		text := string(data)
		if mediaType == "text/html" {
			text = htmlText(text)
		}
		return text, nil
	}
	return "", nil
}

func transferDecoder(encoding string, body io.Reader) io.Reader {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, body)
	case "quoted-printable":
		return quotedprintable.NewReader(body)
	}
	return body
}

var (
	htmlBreak = regexp.MustCompile(`(?i)<\s*(br|/p|/div|/li|/tr)[^>]*>`)
	htmlBlock = regexp.MustCompile(`(?is)<(style|script|head)[^>]*>.*?</(style|script|head)\s*>|<blockquote.*`)
	htmlTag   = regexp.MustCompile(`(?s)<[^>]*>`)
	// quoteIntro is the line a mail client writes above the quoted original.
	quoteIntro  = regexp.MustCompile(`(?i)^(on\s.+\swrote:|.+写道[:：]|-{2,}\s*(original message|原始邮件|forwarded message)\s*-{2,})$`)
	replyPrefix = regexp.MustCompile(`(?i)^\s*((re|fwd?|回复|答复|转发)\s*[:：]\s*)+`)
)

// htmlText keeps an HTML part's text: line breaks for block ends, nothing
// from style, script or quoted blocks.
func htmlText(text string) string {
	text = htmlBlock.ReplaceAllString(text, "")
	text = htmlBreak.ReplaceAllString(text, "\n")
	return html.UnescapeString(htmlTag.ReplaceAllString(text, ""))
}

// stripQuoted keeps what the owner wrote above the quoted original and the
// signature.
func stripQuoted(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	kept := []string{}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		joined := trimmed
		if i+1 < len(lines) {
			joined += " " + strings.TrimSpace(lines[i+1])
		}
		if line == "-- " || quoteIntro.MatchString(trimmed) || (strings.HasPrefix(strings.ToLower(trimmed), "on ") && quoteIntro.MatchString(joined)) {
			break
		}
		if strings.HasPrefix(trimmed, ">") {
			continue
		}
		kept = append(kept, strings.TrimRight(line, " \t"))
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// Send mails one line to the owner. Port 465 is TLS from the start; other
// ports must offer STARTTLS. A loopback host is used as is.
func (e *EmailChannel) Send(ctx context.Context, message RemoteOutbound) ([]string, error) {
	_, port, _ := net.SplitHostPort(e.config.SMTP)
	conn, host, err := e.connect(ctx, e.config.SMTP, port == "465")
	if err != nil {
		return nil, fmt.Errorf("SMTP %s: %w", e.config.SMTP, err)
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("SMTP %s: %w", e.config.SMTP, err), conn.Close())
	}
	defer client.Close()
	if port != "465" && !loopbackHost(host) {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return nil, fmt.Errorf("SMTP %s offers no STARTTLS; refusing to send the password in the clear", e.config.SMTP)
		}
		if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return nil, fmt.Errorf("SMTP STARTTLS: %w", err)
		}
	}
	if ok, _ := client.Extension("AUTH"); ok {
		if err := client.Auth(smtp.PlainAuth("", e.config.User, e.config.Password, host)); err != nil {
			return nil, fmt.Errorf("SMTP AUTH: %w", err)
		}
	}
	id, err := newMessageID(e.config.User, message.MessageID)
	if err != nil {
		return nil, err
	}
	if err := client.Mail(e.config.User); err != nil {
		return nil, fmt.Errorf("SMTP MAIL: %w", err)
	}
	if err := client.Rcpt(e.config.Owner); err != nil {
		return nil, fmt.Errorf("SMTP RCPT: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return nil, fmt.Errorf("SMTP DATA: %w", err)
	}
	if _, err := writer.Write(composeMail(e.config.User, e.config.Owner, message.Subject, id, message.Text, time.Now())); err != nil {
		return nil, fmt.Errorf("SMTP DATA: %w", err)
	}
	if err := writer.Close(); err != nil {
		var refused *textproto.Error
		if errors.As(err, &refused) && refused.Code >= 500 {
			return nil, &RemoteRejectedError{fmt.Errorf("SMTP refused the message: %w", err)}
		}
		return nil, fmt.Errorf("SMTP DATA: %w", err)
	}
	_ = client.Quit()
	return []string{id}, nil
}

// newMessageID makes a unique Message-ID in the sending mailbox's domain;
// the owner's reply quotes it in In-Reply-To.
func newMessageID(sender string, line int64) (string, error) {
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return fmt.Sprintf("mini-loop.%d.%s@%s", line, hex.EncodeToString(random), domainOf(sender)), nil
}

// composeMail writes a UTF-8 plain-text message with a base64 body.
func composeMail(from, to, subject, id, text string, now time.Time) []byte {
	var b bytes.Buffer
	header := func(key, value string) { fmt.Fprintf(&b, "%s: %s\r\n", key, value) }
	header("From", (&mail.Address{Name: "mini-loop 助理", Address: from}).String())
	header("To", to)
	header("Subject", mime.QEncoding.Encode("utf-8", subject))
	header("Date", now.Format(time.RFC1123Z))
	header("Message-ID", "<"+id+">")
	header("MIME-Version", "1.0")
	header("Content-Type", `text/plain; charset="utf-8"`)
	header("Content-Transfer-Encoding", "base64")
	b.WriteString("\r\n")
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	for len(encoded) > 76 {
		b.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	b.WriteString(encoded + "\r\n")
	return b.Bytes()
}
