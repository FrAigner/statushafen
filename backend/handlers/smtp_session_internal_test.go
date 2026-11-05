package handlers

import (
	"bufio"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptedSMTP ist ein SMTP-Test-Double, dessen Antworten pro Befehl vorgegeben
// werden. So lässt sich jeder Fehlerpfad der Sitzung (EHLO, STARTTLS, AUTH,
// MAIL, RCPT, DATA) einzeln auslösen. Schlüssel von replies sind
// Befehlsanfänge in Großbuchstaben (EHLO, STARTTLS, AUTH, MAIL, RCPT, DATA,
// END für das Ende der Nachricht, QUIT); fehlt ein Eintrag, antwortet der Server
// mit 250.
type scriptedSMTP struct {
	greeting string
	replies  map[string]string
	// closeAfterData beendet die Verbindung direkt nach "354", sodass das
	// Schreiben der Nachricht fehlschlägt.
	closeAfterData bool

	mu   sync.Mutex
	seen []string
}

func (s *scriptedSMTP) commands() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.seen...)
}

func (s *scriptedSMTP) start(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := ln.Accept()
		if err != nil {
			return
		}
		s.serve(c)
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
	})
	return ln.Addr().String()
}

func (s *scriptedSMTP) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	say := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }
	reply := func(key, def string) {
		resp, ok := s.replies[key]
		if !ok {
			resp = def
		}
		say(resp)
	}
	greeting := s.greeting
	if greeting == "" {
		greeting = "220 scripted.smtp ready"
	}
	say(greeting)
	inData := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.ToUpper(strings.TrimRight(line, "\r\n"))
		s.mu.Lock()
		s.seen = append(s.seen, line)
		s.mu.Unlock()
		if inData {
			if line == "." {
				inData = false
				reply("END", "250 queued")
			}
			continue
		}
		key, _, _ := strings.Cut(line, " ")
		switch key {
		case "DATA":
			reply("DATA", "354 go ahead")
			if s.closeAfterData {
				return
			}
			if resp := s.replies["DATA"]; !strings.HasPrefix(resp, "4") && !strings.HasPrefix(resp, "5") {
				inData = true
			}
		case "QUIT":
			say("221 bye")
			return
		default:
			reply(key, "250 OK")
		}
	}
}

func runScripted(t *testing.T, s *scriptedSMTP, auth *testAuth, msg []byte) error {
	t.Helper()
	addr := s.start(t)
	host, _, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	if auth == nil {
		return sendMailWithTimeout(addr, host, nil, "from@example.com", "to@example.org", msg)
	}
	return sendMailWithTimeout(addr, host, auth.auth(host), "from@example.com", "to@example.org", msg)
}

// testAuth bündelt Zugangsdaten für PlainAuth gegen 127.0.0.1 (PlainAuth
// erlaubt unverschlüsselte Verbindungen nur zu Loopback-Adressen).
type testAuth struct{ user, password string }

func (a *testAuth) auth(host string) smtp.Auth {
	return smtpConfig{Host: host, User: a.user, Password: a.password}.auth()
}

func TestSendMailWithTimeout_Success(t *testing.T) {
	s := &scriptedSMTP{replies: map[string]string{"EHLO": "250-scripted\r\n250 AUTH PLAIN", "AUTH": "235 ok"}}
	require.NoError(t, runScripted(t, s, &testAuth{"user@example.com", "pw-example"}, []byte("Subject: x\r\n\r\nbody")))
	got := s.commands()
	assert.Contains(t, got, "MAIL FROM:<FROM@EXAMPLE.COM>")
	assert.Contains(t, got, "RCPT TO:<TO@EXAMPLE.ORG>")
	assert.Contains(t, got, "QUIT")
}

func TestSendMailWithTimeout_ErrorPaths(t *testing.T) {
	big := []byte(strings.Repeat("x", 8<<20))
	authExt := "250-scripted\r\n250 AUTH PLAIN"
	cases := []struct {
		name    string
		s       *scriptedSMTP
		auth    *testAuth
		msg     []byte
		wantErr string
		lastCmd string
	}{
		{"Begrüßung abgelehnt", &scriptedSMTP{greeting: "554 no service"}, nil, nil, "554", ""},
		{"EHLO und HELO abgelehnt", &scriptedSMTP{replies: map[string]string{"EHLO": "502 no", "HELO": "502 no"}}, nil, nil, "502", ""},
		{"STARTTLS abgelehnt", &scriptedSMTP{replies: map[string]string{"EHLO": "250-scripted\r\n250 STARTTLS", "STARTTLS": "454 tls unavailable"}}, nil, nil, "454", "STARTTLS"},
		{"AUTH nicht angeboten", &scriptedSMTP{}, &testAuth{"user@example.com", "pw-example"}, nil, "doesn't support AUTH", "EHLO LOCALHOST"},
		{"AUTH abgelehnt", &scriptedSMTP{replies: map[string]string{"EHLO": authExt, "AUTH": "535 bad credentials"}}, &testAuth{"user@example.com", "pw-example"}, nil, "535", ""},
		{"MAIL abgelehnt", &scriptedSMTP{replies: map[string]string{"MAIL": "550 sender refused"}}, nil, nil, "550", ""},
		{"RCPT abgelehnt", &scriptedSMTP{replies: map[string]string{"RCPT": "550 recipient refused"}}, nil, nil, "550", ""},
		{"DATA abgelehnt", &scriptedSMTP{replies: map[string]string{"DATA": "451 try later"}}, nil, nil, "451", ""},
		{"Ende der Nachricht abgelehnt", &scriptedSMTP{replies: map[string]string{"END": "554 rejected"}}, nil, []byte("body"), "554", ""},
		// Der Server schließt nach "354"; der Client schreibt mehr, als in die
		// Socket-Puffer passt, und muss einen Fehler melden.
		{"Verbindung bricht beim Schreiben ab", &scriptedSMTP{closeAfterData: true}, nil, big, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := tc.msg
			if msg == nil {
				msg = []byte("body")
			}
			err := runScripted(t, tc.s, tc.auth, msg)
			require.Error(t, err)
			if tc.wantErr != "" {
				assert.Contains(t, err.Error(), tc.wantErr)
			}
			if tc.lastCmd != "" {
				assert.Contains(t, tc.s.commands(), tc.lastCmd)
			}
		})
	}
}

func TestSendMailSession_ClosedConnectionFailsOnDeadline(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()
	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	err = sendMailSession(conn, "127.0.0.1", nil, "from@example.com", "to@example.org", []byte("x"))
	require.Error(t, err)
}
