package assistant

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	// imapLineLimit and imapLiteralLimit bound what one response may hold.
	imapLineLimit    = 64 << 10
	imapLiteralLimit = 4 << 20
)

// imapClient is the small part of IMAP4rev1 the email channel needs: log in,
// open the inbox read-only, find UIDs after a cursor, read sizes and whole
// messages without setting \Seen. One client serves one receive and then
// logs out.
type imapClient struct {
	conn net.Conn
	r    *bufio.Reader
	tag  int
}

var (
	imapLiteral     = regexp.MustCompile(`\{(\d+)\+?\}$`)
	imapUIDValidity = regexp.MustCompile(`\[UIDVALIDITY (\d+)\]`)
	imapUIDNext     = regexp.MustCompile(`\[UIDNEXT (\d+)\]`)
	imapFetchUID    = regexp.MustCompile(`\bUID (\d+)`)
	imapFetchSize   = regexp.MustCompile(`\bRFC822\.SIZE (\d+)`)
)

// newIMAP reads the server greeting on an open connection.
func newIMAP(conn net.Conn) (*imapClient, error) {
	c := &imapClient{conn: conn, r: bufio.NewReaderSize(conn, 8<<10)}
	greeting, _, err := c.response()
	if err != nil {
		return nil, fmt.Errorf("IMAP greeting: %w", err)
	}
	if !strings.HasPrefix(greeting, "* OK") && !strings.HasPrefix(greeting, "* PREAUTH") {
		return nil, fmt.Errorf("IMAP greeting: %s", truncateRunes(greeting, 120))
	}
	return c, nil
}

// line reads one CRLF-terminated line within imapLineLimit.
func (c *imapClient) line() (string, error) {
	var b strings.Builder
	for {
		chunk, err := c.r.ReadSlice('\n')
		b.Write(chunk)
		if b.Len() > imapLineLimit {
			return "", errors.New("IMAP line too long")
		}
		if err == nil {
			return strings.TrimRight(b.String(), "\r\n"), nil
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return "", err
		}
	}
}

// response reads one response: a line, and when it ends in a literal {n},
// the n bytes and the rest of the response after them.
func (c *imapClient) response() (string, [][]byte, error) {
	var text strings.Builder
	var literals [][]byte
	for {
		line, err := c.line()
		if err != nil {
			return "", nil, err
		}
		text.WriteString(line)
		match := imapLiteral.FindStringSubmatch(line)
		if match == nil {
			return text.String(), literals, nil
		}
		size, err := strconv.Atoi(match[1])
		if err != nil || size > imapLiteralLimit {
			return "", nil, fmt.Errorf("IMAP literal of %s bytes is too large", match[1])
		}
		literal := make([]byte, size)
		if _, err := io.ReadFull(c.r, literal); err != nil {
			return "", nil, err
		}
		literals = append(literals, literal)
	}
}

// command sends one tagged command and collects the untagged responses and
// their literals until the tagged status. Errors quote the command's verb
// and the server's reply, never its arguments, so a password never appears.
func (c *imapClient) command(verb, arguments string) ([]string, [][]byte, error) {
	c.tag++
	tag := "m" + strconv.Itoa(c.tag)
	line := tag + " " + verb
	if arguments != "" {
		line += " " + arguments
	}
	if _, err := io.WriteString(c.conn, line+"\r\n"); err != nil {
		return nil, nil, fmt.Errorf("IMAP %s: %w", verb, err)
	}
	untagged := []string{}
	literals := [][]byte{}
	for {
		text, found, err := c.response()
		if err != nil {
			return nil, nil, fmt.Errorf("IMAP %s: %w", verb, err)
		}
		if status, ok := strings.CutPrefix(text, tag+" "); ok {
			if !strings.HasPrefix(status, "OK") {
				return nil, nil, fmt.Errorf("IMAP %s: %s", verb, truncateRunes(status, 160))
			}
			return untagged, literals, nil
		}
		if strings.HasPrefix(text, "+") {
			return nil, nil, fmt.Errorf("IMAP %s: unexpected continuation", verb)
		}
		untagged = append(untagged, text)
		literals = append(literals, found...)
	}
}

// imapQuote quotes a LOGIN argument. IMAP quoted strings are 7-bit and
// cannot hold line breaks.
func imapQuote(value string) (string, error) {
	for _, r := range value {
		if r > 0x7e || r == '\r' || r == '\n' {
			return "", errors.New("IMAP user and password must be printable ASCII")
		}
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`, nil
}

func (c *imapClient) login(user, password string) error {
	quotedUser, err := imapQuote(user)
	if err != nil {
		return err
	}
	quotedPassword, err := imapQuote(password)
	if err != nil {
		return err
	}
	_, _, err = c.command("LOGIN", quotedUser+" "+quotedPassword)
	return err
}

// examine opens the inbox read-only and returns its UIDVALIDITY and UIDNEXT.
func (c *imapClient) examine() (uint64, uint64, error) {
	lines, _, err := c.command("EXAMINE", "INBOX")
	if err != nil {
		return 0, 0, err
	}
	var validity, next uint64
	for _, line := range lines {
		if match := imapUIDValidity.FindStringSubmatch(line); match != nil {
			validity, _ = strconv.ParseUint(match[1], 10, 64)
		}
		if match := imapUIDNext.FindStringSubmatch(line); match != nil {
			next, _ = strconv.ParseUint(match[1], 10, 64)
		}
	}
	if validity == 0 || next == 0 {
		return 0, 0, errors.New("IMAP EXAMINE: no UIDVALIDITY or UIDNEXT")
	}
	return validity, next, nil
}

// uidsAfter lists the UIDs greater than last, ascending. "n:*" also matches
// the newest message when nothing is newer than n, so it filters.
func (c *imapClient) uidsAfter(last uint64) ([]uint64, error) {
	lines, _, err := c.command("UID SEARCH", fmt.Sprintf("UID %d:*", last+1))
	if err != nil {
		return nil, err
	}
	uids := []uint64{}
	for _, line := range lines {
		rest, ok := strings.CutPrefix(line, "* SEARCH")
		if !ok {
			continue
		}
		for _, field := range strings.Fields(rest) {
			if uid, err := strconv.ParseUint(field, 10, 64); err == nil && uid > last {
				uids = append(uids, uid)
			}
		}
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	return uids, nil
}

// sizes reads each UID's message size.
func (c *imapClient) sizes(uids []uint64) (map[uint64]int, error) {
	set := make([]string, len(uids))
	for i, uid := range uids {
		set[i] = strconv.FormatUint(uid, 10)
	}
	lines, _, err := c.command("UID FETCH", strings.Join(set, ",")+" (UID RFC822.SIZE)")
	if err != nil {
		return nil, err
	}
	sizes := map[uint64]int{}
	for _, line := range lines {
		uid, size := imapFetchUID.FindStringSubmatch(line), imapFetchSize.FindStringSubmatch(line)
		if uid == nil || size == nil {
			continue
		}
		id, _ := strconv.ParseUint(uid[1], 10, 64)
		sizes[id], _ = strconv.Atoi(size[1])
	}
	return sizes, nil
}

// message reads one whole message without marking it seen.
func (c *imapClient) message(uid uint64) ([]byte, error) {
	_, literals, err := c.command("UID FETCH", strconv.FormatUint(uid, 10)+" (BODY.PEEK[])")
	if err != nil {
		return nil, err
	}
	if len(literals) == 0 {
		return nil, fmt.Errorf("IMAP UID FETCH %d: no message body", uid)
	}
	return literals[0], nil
}

func (c *imapClient) logout() {
	_, _, _ = c.command("LOGOUT", "")
}
