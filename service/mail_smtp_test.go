package service

import (
	"bufio"
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/mail"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lejianwen/rustdesk-api/v2/model"
)

func TestSMTPTransportAndMIME(t *testing.T) {
	s, _ := mailFixture(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	s.cfg.Host = "127.0.0.1"
	s.cfg.Port, _ = strconv.Atoi(port)
	s.cfg.TLS = "none"
	s.cfg.FromName = "测试账号中心"
	received := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		reader := bufio.NewReader(conn)
		io.WriteString(conn, "220 localhost test SMTP\r\n")
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO"):
				io.WriteString(conn, "250-localhost\r\n250 8BITMIME\r\n")
			case strings.HasPrefix(line, "DATA"):
				io.WriteString(conn, "354 Send data\r\n")
				var data strings.Builder
				for {
					v, e := reader.ReadString('\n')
					if e != nil {
						return
					}
					if v == ".\r\n" {
						break
					}
					data.WriteString(v)
				}
				received <- data.String()
				io.WriteString(conn, "250 queued\r\n")
			case strings.HasPrefix(line, "QUIT"):
				io.WriteString(conn, "221 bye\r\n")
				return
			default:
				io.WriteString(conn, "250 OK\r\n")
			}
		}
	}()
	err = s.sendSMTP(context.Background(), model.MailMessage{ID: 1, To: "recipient@example.test", Subject: "邮箱验证码", Body: "验证码：12345678\n中文内容", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case raw := <-received:
		msg, err := mail.ReadMessage(strings.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, msg.Body))
		if err != nil || !strings.Contains(string(body), "验证码：12345678") {
			t.Fatal("SMTP MIME body corrupted")
		}
		if msg.Header.Get("To") != "recipient@example.test" {
			t.Fatal("wrong envelope content")
		}
	case <-time.After(time.Second):
		t.Fatal("SMTP server received no message")
	}
}
